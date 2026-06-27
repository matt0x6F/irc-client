package script

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/matt0x6f/irc-client/cascade"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/extension"
)

// eventMessageReceived mirrors irc.EventMessageReceived without importing the irc
// package (avoids an import cycle; the string is the contract).
const eventMessageReceived = "message.received"

// cascadeSDKVersion is the cascade SDK module version the scaffolded scripts
// go.mod requires. Bump this when the app ships against a newer cascade tag.
const cascadeSDKVersion = "v1.0.0"

// Sender sends a message to target on the given network (e.g. App.SendMessage).
type Sender func(networkID int64, target, message string) error

// loaded is a loaded script plus a mutex serializing its dispatch (Yaegi
// interpreters are not safe for concurrent Call).
type loaded struct {
	script *Script
	mu     sync.Mutex
}

// Manager discovers, loads, and dispatches scripts. It is the extension.Host
// for the script runtime.
type Manager struct {
	dir    string
	sender Sender
	reg    *extension.Registry
	router *extension.Router

	mu      sync.RWMutex
	scripts map[extension.ID]*loaded
	watcher *fsnotify.Watcher
}

// NewManager builds a script manager and attaches its router to the bus.
func NewManager(bus *events.EventBus, dir string, sender Sender) *Manager {
	reg := extension.NewRegistry()
	m := &Manager{
		dir:     dir,
		sender:  sender,
		reg:     reg,
		router:  extension.NewRouter(reg),
		scripts: make(map[extension.ID]*loaded),
	}
	m.router.Attach(bus)
	return m
}

// scaffoldModule writes a go.mod at the scripts-dir root if one does not exist,
// so an external editor's gopls resolves `import ".../cascade"`. It never
// overwrites a user-edited go.mod. (Resolution requires the cascade/<ver> tag
// to be published; pre-release, gopls in the scripts dir cannot fetch it.)
func (m *Manager) scaffoldModule() error {
	path := filepath.Join(m.dir, "go.mod")
	if _, err := os.Stat(path); err == nil {
		return nil // exists; respect user edits
	}
	content := "module cascade-scripts\n\ngo 1.21\n\nrequire github.com/matt0x6f/irc-client/cascade " + cascadeSDKVersion + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// LoadAll ensures the scripts dir exists, then loads each immediate subdirectory
// that contains .go files. Per-script load failures are recorded as errored
// extensions, never aborting the whole scan.
func (m *Manager) LoadAll() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("scripts dir: %w", err)
	}
	if err := m.scaffoldModule(); err != nil {
		return fmt.Errorf("scaffold scripts module: %w", err)
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read scripts dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(m.dir, e.Name())
		if goFiles, _ := filepath.Glob(filepath.Join(sub, "*.go")); len(goFiles) == 0 {
			continue
		}
		m.loadDir(sub)
	}
	return nil
}

func (m *Manager) loadDir(dir string) {
	id := extension.ID(filepath.Base(dir))
	man := parseManifest(dir)
	name := man.Name
	if name == "" {
		name = filepath.Base(dir)
	}
	ext := &extension.Extension{
		ID:          id,
		Name:        name,
		Description: man.Description,
		Kind:        extension.KindScript,
		Enabled:     true,
		Status:      extension.StatusLoaded,
		Perms:       man.Permissions,
	}

	s, err := LoadPackage(dir)
	if err != nil {
		ext.Status = extension.StatusError
		ext.Err = err.Error()
		m.mu.Lock()
		delete(m.scripts, id) // a prior (working) version must not keep running
		m.mu.Unlock()
		m.reg.Register(ext, m, nil) // registered (visible) but no subscriptions
		return
	}

	var evs []string
	if s.Has("OnText") {
		evs = append(evs, eventMessageReceived)
	}

	m.mu.Lock()
	m.scripts[id] = &loaded{script: s}
	m.mu.Unlock()
	m.reg.Register(ext, m, evs)
}

// Kind implements extension.Host.
func (m *Manager) Kind() extension.Kind { return extension.KindScript }

// Deliver implements extension.Host: translate the event and dispatch to the
// script, serialized per script and with panic recovery so a buggy script can
// never crash the host.
func (m *Manager) Deliver(id extension.ID, ev events.Event) (err error) {
	m.mu.RLock()
	l, ok := m.scripts[id]
	m.mu.RUnlock()
	if !ok {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("script %s panicked: %v", id, r)
		}
	}()

	switch ev.Type {
	case eventMessageReceived:
		te, ok := m.buildTextEvent(ev)
		if !ok {
			return nil
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		l.script.DispatchText(te)
	}
	return nil
}

// buildTextEvent maps a message.received event into a cascade.TextEvent with a
// Reply closure routed to the right network + target.
func (m *Manager) buildTextEvent(ev events.Event) (cascade.TextEvent, bool) {
	nick, _ := ev.Data["user"].(string)
	channel, _ := ev.Data["channel"].(string)
	message, _ := ev.Data["message"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	// "*" is the server/status pseudo-user (not a real sender); never deliver it to scripts.
	if nick == "" || nick == "*" {
		return cascade.TextEvent{}, false
	}

	target := channel
	if !isChannel(channel) {
		target = nick // DM: reply to the sender
	}
	reply := func(msg string) { _ = m.sender(networkID, target, msg) }
	return cascade.NewTextEvent(nick, channel, message, reply), true
}

func isChannel(s string) bool {
	return len(s) > 0 && (s[0] == '#' || s[0] == '&')
}

// reload re-loads the script directory (used for hot reload + manual reload).
// loadDir already replaces the registry entry and scripts map slot, so reload
// is just a re-load by directory.
func (m *Manager) reload(dir string) { m.loadDir(dir) }

// unload removes a script entirely (used when its directory is deleted).
func (m *Manager) unload(id extension.ID) {
	m.mu.Lock()
	delete(m.scripts, id)
	m.mu.Unlock()
	m.reg.Remove(id)
}

// List returns a snapshot of all registered scripts.
func (m *Manager) List() []extension.Extension { return m.reg.List() }

// registry exposes the registry for tests.
func (m *Manager) registry() *extension.Registry { return m.reg }

// Watch begins watching the scripts dir (and each script subdir) and hot-reloads
// on change. Safe to call once after LoadAll. Errors starting the watcher are
// returned; per-event errors are logged-and-continued by the run loop.
func (m *Manager) Watch() error {
	if m.watcher != nil {
		return nil // already watching
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	m.watcher = w
	// Watch the root (for new/removed script dirs) and each existing script dir.
	_ = w.Add(m.dir)
	if entries, err := os.ReadDir(m.dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				_ = w.Add(filepath.Join(m.dir, e.Name()))
			}
		}
	}
	go m.runWatch()
	return nil
}

// Close stops the watcher.
func (m *Manager) Close() error {
	if m.watcher != nil {
		return m.watcher.Close()
	}
	return nil
}

func (m *Manager) runWatch() {
	for {
		select {
		case ev, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			m.handleFSEvent(ev)
		case _, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// scriptDirFor maps a changed path to the script directory it belongs to (an
// immediate child of m.dir), or "" if the path is m.dir itself / unrelated.
func (m *Manager) scriptDirFor(path string) string {
	rel, err := filepath.Rel(m.dir, path)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
	if parts[0] == "" || parts[0] == "." || parts[0] == ".." {
		return ""
	}
	return filepath.Join(m.dir, parts[0])
}

func (m *Manager) handleFSEvent(ev fsnotify.Event) {
	// New directory under root: start watching it and load it.
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() && filepath.Dir(ev.Name) == m.dir {
			_ = m.watcher.Add(ev.Name)
			m.loadDir(ev.Name)
			return
		}
	}
	sub := m.scriptDirFor(ev.Name)
	if sub == "" {
		return
	}
	id := extension.ID(filepath.Base(sub))
	if _, err := os.Stat(sub); os.IsNotExist(err) {
		m.unload(id)
		return
	}
	if goFiles, _ := filepath.Glob(filepath.Join(sub, "*.go")); len(goFiles) > 0 {
		m.reload(sub)
	}
}

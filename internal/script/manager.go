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
	"github.com/matt0x6f/irc-client/internal/logger"
)

// eventMessageReceived mirrors irc.EventMessageReceived without importing the irc
// package (avoids an import cycle; the string is the contract).
const eventMessageReceived = "message.received"

const (
	eventUserJoined = "user.joined"
	eventUserParted = "user.parted"
)

// cascadeSDKVersion is the cascade SDK module version the scaffolded scripts
// go.mod requires. Bump this when the app ships against a newer cascade tag.
const cascadeSDKVersion = "v1.0.0"

// Sender sends a message to target on the given network (e.g. App.SendMessage).
type Sender func(networkID int64, target, message string) error

// Host bundles the capabilities the Manager needs from the application.
type Host struct {
	Send           Sender                          // App.SendMessage
	SelfNick       func(networkID int64) string    // App.GetCurrentNick wrapper
	ResolveNetwork func(name string) (int64, bool) // name → networkID (via App.GetNetworks)
	// LoadDisabled returns the set of script IDs that should start disabled.
	// Nil-safe: a nil func is treated as returning an empty map.
	LoadDisabled func() map[string]bool
	// PersistEnabled persists an enable/disable toggle. Nil-safe.
	PersistEnabled func(id string, enabled bool)
}

// loaded is a loaded script plus a mutex serializing its dispatch (Yaegi
// interpreters are not safe for concurrent Call).
type loaded struct {
	script *Script
	mu     sync.Mutex
}

// Manager discovers, loads, and dispatches scripts. It is the extension.Host
// for the script runtime.
type Manager struct {
	dir      string
	host     Host
	reg      *extension.Registry
	router   *extension.Router
	watchdog *watchdog
	sched    *scheduler

	mu      sync.RWMutex
	scripts map[extension.ID]*loaded
	watcher *fsnotify.Watcher
}

// NewManager builds a script manager and attaches its router to the bus.
// host.SelfNick returns the bot's current nick on the given network; it is
// used to suppress delivery of the bot's own (possibly server-echoed) messages.
func NewManager(bus *events.EventBus, dir string, host Host) *Manager {
	reg := extension.NewRegistry()
	m := &Manager{
		dir:     dir,
		host:    host,
		reg:     reg,
		router:  extension.NewRouter(reg),
		scripts: make(map[extension.ID]*loaded),
		sched:   newScheduler(),
	}
	m.watchdog = newWatchdog(defaultDispatchDeadline, defaultMaxStrikes, m.disableScript)
	m.router.Attach(bus)
	return m
}

// makeClient builds a per-script *cascade.Client whose Network().Say resolves
// the network name via host.ResolveNetwork and calls host.Send. If the network
// is unknown the call is logged and dropped. The scheduler closures are wired
// to the per-script timer registry so ticks run through the watchdog +
// per-script mutex and are cancelled on reload/unload.
func (m *Manager) makeClient(id extension.ID) *cascade.Client {
	say := func(networkName, target, message string) {
		netID, ok := m.host.ResolveNetwork(networkName)
		if !ok {
			logger.Log.Warn().Str("script", string(id)).Str("network", networkName).Msg("script Say: unknown network")
			return
		}
		_ = m.host.Send(netID, target, message)
	}
	return cascade.NewClient(say, m.scheduleEvery(id), m.scheduleAfter(id))
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
	// Load the persisted-disabled set once; nil-safe.
	disabled := map[string]bool{}
	if m.host.LoadDisabled != nil {
		if d := m.host.LoadDisabled(); d != nil {
			disabled = d
		}
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
		// Apply persisted-disabled state: scripts start inert if they were
		// disabled in a previous session.
		id := extension.ID(e.Name())
		if disabled[string(id)] {
			m.Disable(id)
		}
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

	// Run Setup if the script exports it. A failure marks the script errored and
	// skips event subscription (a Setup-panicking script must not handle events).
	if s.HasSetup() {
		if err := s.RunSetup(m.makeClient(id)); err != nil {
			ext.Status = extension.StatusError
			ext.Err = "Setup: " + err.Error()
			m.mu.Lock()
			delete(m.scripts, id)
			m.mu.Unlock()
			m.reg.Register(ext, m, nil) // registered+errored but no subscriptions
			return
		}
	}

	var evs []string
	if s.Has("OnText") || s.Has("OnNotice") {
		evs = append(evs, eventMessageReceived)
	}
	if s.Has("OnJoin") {
		evs = append(evs, eventUserJoined)
	}
	if s.Has("OnPart") {
		evs = append(evs, eventUserParted)
	}

	m.mu.Lock()
	m.scripts[id] = &loaded{script: s}
	m.mu.Unlock()
	m.reg.Register(ext, m, evs)
}

// Kind implements extension.Host.
func (m *Manager) Kind() extension.Kind { return extension.KindScript }

// Deliver implements extension.Host. The watchdog bounds each dispatch and
// auto-disables a script that hangs or repeatedly fails.
func (m *Manager) Deliver(id extension.ID, ev events.Event) error {
	err := m.watchdog.run(id, func() error { return m.dispatch(id, ev) })
	// If the script was just disabled (StatusRunaway), suppress the error so the
	// Router does not overwrite the runaway status with a transient StatusError.
	if err != nil {
		if ext, ok := m.reg.Get(id); ok && ext.Status == extension.StatusRunaway {
			return nil
		}
	}
	return err
}

// dispatch translates the event and runs the script's handler, serialized per
// script. A handler panic is recovered (so the per-script mutex unlocks) and
// returned as an error for the watchdog to count.
func (m *Manager) dispatch(id extension.ID, ev events.Event) (err error) {
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
		mt, _ := ev.Data["messageType"].(string)
		if mt == "notice" {
			if ne, ok := m.buildNoticeEvent(ev); ok {
				l.mu.Lock()
				defer l.mu.Unlock()
				l.script.DispatchNotice(ne)
			}
			return nil
		}
		if te, ok := m.buildTextEvent(ev); ok {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.script.DispatchText(te)
		}
	case eventUserJoined:
		if je, ok := m.buildJoinEvent(ev); ok {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.script.DispatchJoin(je)
		}
	case eventUserParted:
		if pe, ok := m.buildPartEvent(ev); ok {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.script.DispatchPart(pe)
		}
	}
	return nil
}

// disableScript is the watchdog's disable callback: stop delivery and record why.
func (m *Manager) disableScript(id extension.ID, reason string) {
	m.reg.DisableAsRunaway(id, reason)
	logger.Log.Warn().Str("script", string(id)).Str("reason", reason).Msg("script auto-disabled by watchdog")
}

// msgFields extracts the common sender fields from a message.received event and
// applies the self/pseudo-user guards. Returns ok=false if delivery should be suppressed.
func (m *Manager) msgFields(ev events.Event) (nick, channel, message string, networkID int64, ok bool) {
	nick, _ = ev.Data["user"].(string)
	channel, _ = ev.Data["channel"].(string)
	message, _ = ev.Data["message"].(string)
	networkID, _ = ev.Data["networkId"].(int64)
	// "*" is the server/status pseudo-user (not a real sender); never deliver it.
	if nick == "" || nick == "*" {
		return "", "", "", 0, false
	}
	if m.host.SelfNick != nil && nick == m.host.SelfNick(networkID) {
		return "", "", "", 0, false
	}
	return nick, channel, message, networkID, true
}

// replyTo returns a Reply closure that routes to the channel (for channel messages)
// or to the sender nick (for DMs).
func (m *Manager) replyTo(networkID int64, channel, nick string) func(string) {
	target := channel
	if !isChannel(channel) {
		target = nick
	}
	return func(msg string) { _ = m.host.Send(networkID, target, msg) }
}

// buildTextEvent maps a message.received event into a cascade.TextEvent with a
// Reply closure routed to the right network + target.
func (m *Manager) buildTextEvent(ev events.Event) (cascade.TextEvent, bool) {
	nick, channel, message, networkID, ok := m.msgFields(ev)
	if !ok {
		return cascade.TextEvent{}, false
	}
	return cascade.NewTextEvent(nick, channel, message, m.replyTo(networkID, channel, nick)), true
}

// buildNoticeEvent maps a message.received/notice event into a cascade.NoticeEvent.
func (m *Manager) buildNoticeEvent(ev events.Event) (cascade.NoticeEvent, bool) {
	nick, channel, message, networkID, ok := m.msgFields(ev)
	if !ok {
		return cascade.NoticeEvent{}, false
	}
	return cascade.NewNoticeEvent(nick, channel, message, m.replyTo(networkID, channel, nick)), true
}

// buildJoinEvent maps a user.joined event into a cascade.JoinEvent.
func (m *Manager) buildJoinEvent(ev events.Event) (cascade.JoinEvent, bool) {
	nick, _ := ev.Data["user"].(string)
	channel, _ := ev.Data["channel"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if nick == "" || nick == "*" || (m.host.SelfNick != nil && nick == m.host.SelfNick(networkID)) {
		return cascade.JoinEvent{}, false
	}
	return cascade.NewJoinEvent(nick, channel, m.replyTo(networkID, channel, nick)), true
}

// buildPartEvent maps a user.parted event into a cascade.PartEvent.
func (m *Manager) buildPartEvent(ev events.Event) (cascade.PartEvent, bool) {
	nick, _ := ev.Data["user"].(string)
	channel, _ := ev.Data["channel"].(string)
	reason, _ := ev.Data["reason"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if nick == "" || nick == "*" || (m.host.SelfNick != nil && nick == m.host.SelfNick(networkID)) {
		return cascade.PartEvent{}, false
	}
	return cascade.NewPartEvent(nick, channel, reason, m.replyTo(networkID, channel, nick)), true
}

func isChannel(s string) bool {
	return len(s) > 0 && (s[0] == '#' || s[0] == '&')
}

// reload re-loads the script directory (used for hot reload + manual reload).
// Old timers are stopped BEFORE re-running Setup so that (a) there are no
// duplicate ticks from the previous version and (b) the new Setup can register
// fresh timers cleanly.
func (m *Manager) reload(dir string) {
	id := extension.ID(filepath.Base(dir))
	m.sched.stopTimers(id)
	m.loadDir(dir)
}

// unload removes a script entirely (used when its directory is deleted).
// Timers are stopped first so in-flight ticks see an empty scripts map and
// return early from runScriptFn.
func (m *Manager) unload(id extension.ID) {
	m.sched.stopTimers(id)
	m.mu.Lock()
	delete(m.scripts, id)
	m.mu.Unlock()
	m.reg.Remove(id)
}

// List returns a snapshot of all registered scripts.
func (m *Manager) List() []extension.Extension { return m.reg.List() }

// Snapshot returns a snapshot of all registered scripts (alias for List,
// intended for the Wails-bound App.ListScripts method).
func (m *Manager) Snapshot() []extension.Extension { return m.reg.List() }

// Disable stops a script's timers, marks it disabled in the registry, and
// optionally persists the toggle via host.PersistEnabled.
func (m *Manager) Disable(id extension.ID) {
	m.sched.stopTimers(id)
	m.reg.SetEnabled(id, false)
	if m.host.PersistEnabled != nil {
		m.host.PersistEnabled(string(id), false)
	}
}

// Enable re-enables a previously disabled or runaway script, clears its strike
// count, re-runs Setup (to restore timers), and optionally persists the toggle.
func (m *Manager) Enable(id extension.ID) {
	m.reg.SetEnabled(id, true)
	m.reg.SetStatus(id, extension.StatusLoaded, "") // clears runaway/error
	m.watchdog.clearStrikes(id)

	// Re-run Setup to restore timers that were stopped by Disable. Old timers
	// are already gone (stopped by Disable), so this cannot duplicate them.
	m.mu.RLock()
	l, ok := m.scripts[id]
	m.mu.RUnlock()
	if ok && l.script.HasSetup() {
		if err := l.script.RunSetup(m.makeClient(id)); err != nil {
			logger.Log.Warn().Str("script", string(id)).Err(err).Msg("Enable: Setup re-run failed")
		}
	}

	if m.host.PersistEnabled != nil {
		m.host.PersistEnabled(string(id), true)
	}
}

// ReloadByID reloads a script by its ID (directory name under m.dir).
func (m *Manager) ReloadByID(id extension.ID) {
	m.reload(filepath.Join(m.dir, string(id)))
}

// NewScript creates a new script directory and starter file at <dir>/<name>/<name>.go.
// name must be a safe single directory segment (no separators, no "..").
// Returns the path to the created file. Errors if the directory already exists.
func (m *Manager) NewScript(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("script name must not be empty")
	}
	// Reject any path separator or ".." to enforce a single safe dir segment.
	if strings.ContainsAny(name, `/\`) || name == ".." || strings.Contains(name, "..") {
		return "", fmt.Errorf("script name %q is not a safe directory name", name)
	}
	scriptDir := filepath.Join(m.dir, name)
	if _, err := os.Stat(scriptDir); err == nil {
		return "", fmt.Errorf("script %q already exists", name)
	}
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		return "", fmt.Errorf("create script dir: %w", err)
	}
	filePath := filepath.Join(scriptDir, name+".go")
	content := "package main\n\n" +
		"// cascade:name " + name + "\n\n" +
		"import \"github.com/matt0x6f/irc-client/cascade\"\n\n" +
		"func OnText(e cascade.TextEvent) {\n" +
		"\t// TODO: implement\n" +
		"}\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write script file: %w", err)
	}
	return filePath, nil
}

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

// Close stops the watcher and all script timers.
func (m *Manager) Close() error {
	m.sched.stopAll()
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

package script

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/matt0x6f/irc-client/cascade"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/extension"
)

// eventMessageReceived mirrors irc.EventMessageReceived without importing the irc
// package (avoids an import cycle; the string is the contract).
const eventMessageReceived = "message.received"

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

// LoadAll ensures the scripts dir exists, then loads each immediate subdirectory
// that contains .go files. Per-script load failures are recorded as errored
// extensions, never aborting the whole scan.
func (m *Manager) LoadAll() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("scripts dir: %w", err)
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
	ext := &extension.Extension{
		ID:      id,
		Name:    filepath.Base(dir),
		Kind:    extension.KindScript,
		Enabled: true,
		Status:  extension.StatusLoaded,
	}

	s, err := LoadPackage(dir)
	if err != nil {
		ext.Status = extension.StatusError
		ext.Err = err.Error()
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

// List returns a snapshot of all registered scripts.
func (m *Manager) List() []extension.Extension { return m.reg.List() }

// registry exposes the registry for tests.
func (m *Manager) registry() *extension.Registry { return m.reg }

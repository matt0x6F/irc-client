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
	"golang.org/x/mod/modfile"
)

// eventMessageReceived mirrors irc.EventMessageReceived without importing the irc
// package (avoids an import cycle; the string is the contract).
const eventMessageReceived = "message.received"

const (
	eventUserJoined = "user.joined"
	eventUserParted = "user.parted"
	eventUserQuit   = "user.quit"
	eventUserKicked = "user.kicked"
	eventUserNick   = "user.nick"
	eventUserMeta   = "user.meta"
	eventSelfStatus = "self.status"
)

// cascadeSDKVersion is the cascade SDK module version the scaffolded scripts
// go.mod requires. Bump this when the app ships against a newer cascade tag.
const cascadeSDKVersion = "v1.1.0"

// cascadeModulePath is the import path of the cascade SDK module.
const cascadeModulePath = "github.com/matt0x6f/irc-client/cascade"

// Sender sends a message to target on the given network (e.g. App.SendMessage).
type Sender func(networkID int64, target, message string) error

// Host bundles the capabilities the Manager needs from the application.
type Host struct {
	Send           Sender                          // App.SendMessage
	SelfNick       func(networkID int64) string    // App.GetCurrentNick wrapper
	ResolveNetwork func(name string) (int64, bool) // name → networkID (via App.GetNetworks)
	Connected      func(networkID int64) bool
	IsMe           func(networkID int64, nick string) bool
	UserStatus     func(networkID int64, nick string) cascade.UserStatus
	Notice         func(networkID int64, target, message string) error
	Action         func(networkID int64, target, message string) error
	Join           func(networkID int64, channel, key string) error
	Part           func(networkID int64, channel, reason string) error
	ChangeNick     func(networkID int64, nick string) error
	SetAway        func(networkID int64, message string) error
	IsChannel      func(networkID int64, target string) bool
	// LoadDisabled returns the set of script IDs that should start disabled.
	// Nil-safe: a nil func is treated as returning an empty map.
	LoadDisabled func() map[string]bool
	// PersistEnabled persists an enable/disable toggle. Nil-safe.
	PersistEnabled func(id string, enabled bool)
	// Notify is called after any change to the loaded-script set or a script's
	// status (enable/disable/reload/runaway), so the frontend can refetch the
	// inventory. Nil-safe.
	Notify func()
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
	resolve := func(networkName string) (int64, bool) {
		if m.host.ResolveNetwork == nil {
			return 0, false
		}
		return m.host.ResolveNetwork(networkName)
	}
	say := func(networkName, target, message string) {
		netID, ok := resolve(networkName)
		if !ok {
			logger.Log.Warn().Str("script", string(id)).Str("network", networkName).Msg("script Say: unknown network")
			return
		}
		_ = m.host.Send(netID, target, message)
	}
	connected := func(networkName string) bool {
		netID, ok := resolve(networkName)
		return ok && m.host.Connected != nil && m.host.Connected(netID)
	}
	nick := func(networkName string) string {
		netID, ok := resolve(networkName)
		if !ok || m.host.SelfNick == nil {
			return ""
		}
		return m.host.SelfNick(netID)
	}
	isMe := func(networkName, candidate string) bool {
		netID, ok := resolve(networkName)
		return ok && m.host.IsMe != nil && m.host.IsMe(netID, candidate)
	}
	userStatus := func(networkName, candidate string) cascade.UserStatus {
		netID, ok := resolve(networkName)
		if !ok || m.host.UserStatus == nil {
			return cascade.UserStatus{}
		}
		return m.host.UserStatus(netID, candidate)
	}
	withTargetAction := func(name string, fn func(int64, string, string) error) func(string, string, string) {
		return func(networkName, target, value string) {
			netID, ok := resolve(networkName)
			if !ok || fn == nil {
				if !ok {
					logger.Log.Warn().Str("script", string(id)).Str("network", networkName).Str("action", name).Msg("script action: unknown network")
				}
				return
			}
			_ = fn(netID, target, value)
		}
	}
	withNetworkAction := func(name string, fn func(int64, string) error) func(string, string) {
		return func(networkName, value string) {
			netID, ok := resolve(networkName)
			if !ok || fn == nil {
				if !ok {
					logger.Log.Warn().Str("script", string(id)).Str("network", networkName).Str("action", name).Msg("script action: unknown network")
				}
				return
			}
			_ = fn(netID, value)
		}
	}
	return cascade.NewClient(
		say,
		m.scheduleEvery(id),
		m.scheduleAfter(id),
		cascade.WithNetworkQueries(connected, nick, isMe, userStatus),
		cascade.WithIRCActions(
			withTargetAction("notice", m.host.Notice),
			withTargetAction("action", m.host.Action),
			withTargetAction("join", m.host.Join),
			withTargetAction("part", m.host.Part),
			withNetworkAction("nick", m.host.ChangeNick),
			withNetworkAction("away", m.host.SetAway),
		),
	)
}

// reconcileModule keeps the scripts-dir go.mod's cascade SDK pin in lockstep with
// the app's own cascadeSDKVersion, so an external editor's gopls always resolves
// `import ".../cascade"` to the same SDK the runtime actually injects. The app is
// the source of truth: at runtime Yaegi binds the import to the compiled SDK
// regardless of go.mod, so the pin is purely for the editor and must mirror the
// app. When the client auto-updates to a newer SDK, the next launch rewrites the
// pin here.
//
// If go.mod is absent it is scaffolded. If present, only the cascade require line
// is rewritten (any user edits — a replace directive, extra requires — survive).
// When the pin already matches, the file is left byte-for-byte untouched so a
// steady-state launch does not churn its mtime.
func (m *Manager) reconcileModule() error {
	path := filepath.Join(m.dir, "go.mod")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		content := "module cascade-scripts\n\ngo 1.21\n\nrequire " + cascadeModulePath + " " + cascadeSDKVersion + "\n"
		return os.WriteFile(path, []byte(content), 0o644)
	}
	if err != nil {
		return err
	}

	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return fmt.Errorf("parse go.mod: %w", err)
	}
	for _, r := range f.Require {
		if r.Mod.Path == cascadeModulePath && r.Mod.Version == cascadeSDKVersion {
			return nil // already pinned to the app's SDK; leave the file untouched
		}
	}
	if err := f.AddRequire(cascadeModulePath, cascadeSDKVersion); err != nil {
		return fmt.Errorf("pin cascade require: %w", err)
	}
	out, err := f.Format()
	if err != nil {
		return fmt.Errorf("format go.mod: %w", err)
	}
	return os.WriteFile(path, out, 0o644)
}

// LoadAll ensures the scripts dir exists, then loads each immediate subdirectory
// that contains .go files. Per-script load failures are recorded as errored
// extensions, never aborting the whole scan.
func (m *Manager) LoadAll() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("scripts dir: %w", err)
	}
	if err := m.reconcileModule(); err != nil {
		return fmt.Errorf("reconcile scripts module: %w", err)
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
	if s.Has("OnQuit") {
		evs = append(evs, eventUserQuit)
	}
	if s.Has("OnKick") {
		evs = append(evs, eventUserKicked)
	}
	if s.Has("OnNick") {
		evs = append(evs, eventUserNick)
	}
	if s.Has("OnUserStatus") {
		evs = append(evs, eventUserMeta, eventSelfStatus)
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
	case eventUserQuit:
		if qe, ok := m.buildQuitEvent(ev); ok {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.script.DispatchQuit(qe)
		}
	case eventUserKicked:
		if ke, ok := m.buildKickEvent(ev); ok {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.script.DispatchKick(ke)
		}
	case eventUserNick:
		if ne, ok := m.buildNickEvent(ev); ok {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.script.DispatchNick(ne)
		}
	case eventUserMeta, eventSelfStatus:
		if se, ok := m.buildUserStatusEvent(ev); ok {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.script.DispatchUserStatus(se)
		}
	}
	return nil
}

// disableScript is the watchdog's disable callback: stop delivery and record why.
func (m *Manager) disableScript(id extension.ID, reason string) {
	m.reg.DisableAsRunaway(id, reason)
	logger.Log.Warn().Str("script", string(id)).Str("reason", reason).Msg("script auto-disabled by watchdog")
	m.notify()
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
	if !m.isChannel(networkID, channel) {
		target = nick
	}
	return func(msg string) { _ = m.host.Send(networkID, target, msg) }
}

func (m *Manager) isChannel(networkID int64, target string) bool {
	if m.host.IsChannel != nil {
		return m.host.IsChannel(networkID, target)
	}
	return isChannel(target)
}

// buildTextEvent maps a message.received event into a cascade.TextEvent with a
// Reply closure routed to the right network + target.
func (m *Manager) buildTextEvent(ev events.Event) (cascade.TextEvent, bool) {
	nick, channel, message, networkID, ok := m.msgFields(ev)
	if !ok {
		return cascade.TextEvent{}, false
	}
	e := cascade.NewTextEventWithDirect(nick, channel, message, !m.isChannel(networkID, channel), m.replyTo(networkID, channel, nick))
	m.applyContext(&e.Self, &e.Account, &e.Network, &e.MsgID, &e.Time, ev, networkID)
	e.Action, _ = ev.Data["isAction"].(bool)
	return e, true
}

// buildNoticeEvent maps a message.received/notice event into a cascade.NoticeEvent.
func (m *Manager) buildNoticeEvent(ev events.Event) (cascade.NoticeEvent, bool) {
	nick, channel, message, networkID, ok := m.msgFields(ev)
	if !ok {
		return cascade.NoticeEvent{}, false
	}
	e := cascade.NewNoticeEventWithDirect(nick, channel, message, !m.isChannel(networkID, channel), m.replyTo(networkID, channel, nick))
	m.applyContext(&e.Self, &e.Account, &e.Network, &e.MsgID, &e.Time, ev, networkID)
	return e, true
}

// applyContext fills the common context fields from a message.received event.
func (m *Manager) applyContext(self, account, network, msgID *string, ts *cascade.Time, ev events.Event, networkID int64) {
	if m.host.SelfNick != nil {
		*self = m.host.SelfNick(networkID)
	}
	*account, _ = ev.Data["account"].(string)
	*network, _ = ev.Data["networkName"].(string)
	*msgID, _ = ev.Data["msgid"].(string)
	if u, ok := ev.Data["messageUnix"].(int64); ok {
		*ts = cascade.NewTime(u)
	}
}

// buildJoinEvent maps a user.joined event into a cascade.JoinEvent.
func (m *Manager) buildJoinEvent(ev events.Event) (cascade.JoinEvent, bool) {
	nick, _ := ev.Data["user"].(string)
	channel, _ := ev.Data["channel"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if nick == "" || nick == "*" || (m.host.SelfNick != nil && nick == m.host.SelfNick(networkID)) {
		return cascade.JoinEvent{}, false
	}
	e := cascade.NewJoinEvent(nick, channel, m.replyTo(networkID, channel, nick))
	m.applyMembershipContext(&e.Self, &e.Account, &e.Network, &e.Host, &e.Realname, &e.Time, ev, networkID, nick)
	return e, true
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
	e := cascade.NewPartEvent(nick, channel, reason, m.replyTo(networkID, channel, nick))
	m.applyMembershipContext(&e.Self, &e.Account, &e.Network, &e.Host, &e.Realname, &e.Time, ev, networkID, nick)
	return e, true
}

func (m *Manager) applyMembershipContext(self, account, network, host, realname *string, ts *cascade.Time, ev events.Event, networkID int64, nick string) {
	if m.host.SelfNick != nil {
		*self = m.host.SelfNick(networkID)
	}
	*network, _ = ev.Data["networkName"].(string)
	if *network == "" {
		*network, _ = ev.Data["network"].(string)
	}
	if m.host.UserStatus != nil {
		status := m.host.UserStatus(networkID, nick)
		*account, *host, *realname = status.Account, status.Host, status.Realname
	}
	if !ev.Timestamp.IsZero() {
		*ts = cascade.NewTime(ev.Timestamp.Unix())
	}
}

func (m *Manager) buildQuitEvent(ev events.Event) (cascade.QuitEvent, bool) {
	nick, _ := ev.Data["user"].(string)
	reason, _ := ev.Data["reason"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if nick == "" || nick == "*" || (m.host.IsMe != nil && m.host.IsMe(networkID, nick)) {
		return cascade.QuitEvent{}, false
	}
	e := cascade.NewQuitEvent(nick, reason)
	m.applyMembershipContext(&e.Self, &e.Account, &e.Network, &e.Host, &e.Realname, &e.Time, ev, networkID, nick)
	return e, true
}

func (m *Manager) buildKickEvent(ev events.Event) (cascade.KickEvent, bool) {
	nick, _ := ev.Data["user"].(string)
	by, _ := ev.Data["kicker"].(string)
	channel, _ := ev.Data["channel"].(string)
	reason, _ := ev.Data["reason"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if nick == "" || channel == "" {
		return cascade.KickEvent{}, false
	}
	e := cascade.NewKickEvent(nick, by, channel, reason, m.replyTo(networkID, channel, by))
	var host, realname string
	m.applyMembershipContext(&e.Self, &e.Account, &e.Network, &host, &realname, &e.Time, ev, networkID, by)
	return e, true
}

func (m *Manager) buildNickEvent(ev events.Event) (cascade.NickEvent, bool) {
	oldNick, _ := ev.Data["oldNick"].(string)
	newNick, _ := ev.Data["newNick"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if oldNick == "" || newNick == "" {
		return cascade.NickEvent{}, false
	}
	e := cascade.NewNickEvent(oldNick, newNick)
	var host, realname string
	m.applyMembershipContext(&e.Self, &e.Account, &e.Network, &host, &realname, &e.Time, ev, networkID, newNick)
	return e, true
}

func (m *Manager) buildUserStatusEvent(ev events.Event) (cascade.UserStatusEvent, bool) {
	nick, _ := ev.Data["nickname"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if nick == "" {
		return cascade.UserStatusEvent{}, false
	}
	network, _ := ev.Data["networkName"].(string)
	if network == "" {
		network, _ = ev.Data["network"].(string)
	}
	self := ""
	if m.host.SelfNick != nil {
		self = m.host.SelfNick(networkID)
	}
	isSelf := m.host.IsMe != nil && m.host.IsMe(networkID, nick)
	status := cascade.UserStatus{Known: true}
	status.Away, _ = ev.Data["away"].(bool)
	status.AwayMessage, _ = ev.Data["away_message"].(string)
	status.Account, _ = ev.Data["account"].(string)
	status.Host, _ = ev.Data["host"].(string)
	status.Realname, _ = ev.Data["realname"].(string)
	at := cascade.Time{}
	if !ev.Timestamp.IsZero() {
		at = cascade.NewTime(ev.Timestamp.Unix())
	}
	return cascade.NewUserStatusEvent(network, self, nick, status, at, isSelf), true
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
	m.notify()
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
	m.notify()
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
	m.notify()
}

// Enable re-enables a previously disabled or runaway script, clears its strike
// count, re-runs Setup (to restore timers), and optionally persists the toggle.
func (m *Manager) Enable(id extension.ID) {
	m.reg.SetEnabled(id, true)
	m.reg.SetStatus(id, extension.StatusLoaded, "") // clears runaway/error
	m.watchdog.clearStrikes(id)

	// Stop any existing timers before re-running Setup so that calling Enable
	// on an already-running script does not register a second set of timers
	// (which would double tick frequency). This mirrors how reload() clears
	// timers before re-running Setup.
	m.sched.stopTimers(id)

	// Re-run Setup to restore timers.
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
	m.notify()
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
	// Reject any path separator, ".", or ".." to enforce a single safe dir segment.
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." || strings.Contains(name, "..") {
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

// notify signals the host that the script inventory or a script's status changed.
func (m *Manager) notify() {
	if m.host.Notify != nil {
		m.host.Notify()
	}
}

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
			m.notify()
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

# Native Desktop Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `beeep` desktop-notification path with the first-party Wails v3 notifications service, adding click-to-jump, inline reply, mark-as-read, and reconnect actions, all gated by granular user settings.

**Architecture:** `internal/notification` stays Wails-free — it defines a `Delivery` interface, plain notification structs, and prefs-driven policy. A `wailsDelivery` adapter in the `main` package wraps `*notifications.NotificationService` and translates plain structs to the framework types. `main.go` registers the service and injects the adapter. Notification responses route via a pure `routeNotificationResponse` function: server-state actions (reply, reconnect) run in Go; UI-state actions (navigate, mark-read) emit Wails events the frontend handles.

**Tech Stack:** Go (Wails v3 `v3.0.0-alpha2.103`), `github.com/wailsapp/wails/v3/pkg/services/notifications`; React 19 + TypeScript + Zustand; SQLite settings table via `App.GetSetting`/`SetSetting`.

## Global Constraints

- Notifications only render in a **bundled + signed** `.app`. The Cocoa service's
  startup fails without a valid bundle identifier; adapter calls return errors and
  must degrade to a logged no-op (never panic, never block). Nothing fires in
  `task dev` — this is accepted.
- `beeep` (`github.com/gen2brain/beeep`) is **removed entirely** by the end of the plan.
- All notification `Data`/`UserInfo` values are **strings** — the map round-trips
  through the platform/JSON boundary, so `networkId` is stored as a decimal string
  and parsed back with `strconv.ParseInt` (an `int64` would arrive as `float64`).
- `internal/notification` must **not** import `github.com/wailsapp/wails/v3` — keep it unit-testable.
- Settings keys (stable; never rename): `notifications.enabled` (default `false`),
  `notifications.privateMessages` (default `true`), `notifications.mentions`
  (default `true`), `notifications.connectionLost` (default `true`),
  `notifications.onlyWhenUnfocused` (default `true`).
- Backend tests run with: `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1`
  (and `./...` for the `main` package). Frontend: `cd frontend && npx vitest run`.
- Category IDs / action IDs (shared backend constants): category `"message"`
  (actions `REPLY`, `MARK_READ`, reply field) and `"connection"` (action
  `RECONNECT`). Default body click uses `notifications.DefaultActionIdentifier`
  (`"DEFAULT_ACTION"`).

---

### Task 1: Notification policy layer (`internal/notification`)

Rewrite the package: add the `Delivery` interface + plain structs + `Prefs`, make
`Notifier` carry a delivery and a prefs snapshot, and rewrite `ShouldNotify` to be
prefs-driven. Remove the `beeep` call.

**Files:**
- Modify: `internal/notification/notification.go`
- Test: `internal/notification/notification_test.go` (create)

**Interfaces:**
- Produces (consumed by Task 2):
  - `type Prefs struct { Enabled, PrivateMessages, Mentions, ConnectionLost, OnlyWhenUnfocused bool }`
  - `type Action struct { ID, Title string; Destructive bool }`
  - `type Category struct { ID string; Actions []Action; HasReplyField bool; ReplyPlaceholder, ReplyButtonTitle string }`
  - `type Notification struct { ID, Title, Subtitle, Body, CategoryID string; Data map[string]any }`
  - `type Delivery interface { Send(Notification) error; SendWithActions(Notification) error; RegisterCategory(Category) error; RequestAuthorization() (bool, error); CheckAuthorization() (bool, error) }`
  - `func (n *Notifier) SetDelivery(Delivery)`, `func (n *Notifier) SetPrefs(Prefs)`, `func (n *Notifier) Prefs() Prefs`
  - `func (n *Notifier) RequestAuthorization() (bool, error)`
  - `func (n *Notifier) Send(Notification)` (async-safe; logs+no-ops on nil delivery or error)
  - Unchanged: `NewNotifier`, `SetFocused`, `IsFocused`, `ShouldNotify`, `IsPrivateMessage`, `IsMention`

- [ ] **Step 1: Write the failing tests**

Create `internal/notification/notification_test.go`:

```go
package notification

import "testing"

type fakeDelivery struct {
	withActions []Notification
	authResult  bool
	authErr     error
}

func (f *fakeDelivery) Send(n Notification) error            { return nil }
func (f *fakeDelivery) SendWithActions(n Notification) error { f.withActions = append(f.withActions, n); return nil }
func (f *fakeDelivery) RegisterCategory(c Category) error    { return nil }
func (f *fakeDelivery) RequestAuthorization() (bool, error)  { return f.authResult, f.authErr }
func (f *fakeDelivery) CheckAuthorization() (bool, error)    { return f.authResult, f.authErr }

func TestShouldNotify_prefsMatrix(t *testing.T) {
	cases := []struct {
		name      string
		prefs     Prefs
		focused   bool
		eventType string
		want      bool
	}{
		{"disabled blocks all", Prefs{Enabled: false, PrivateMessages: true}, false, "message.received", false},
		{"unfocused-only suppresses when focused", Prefs{Enabled: true, PrivateMessages: true, OnlyWhenUnfocused: true}, true, "message.received", false},
		{"unfocused-only allows when blurred", Prefs{Enabled: true, PrivateMessages: true, OnlyWhenUnfocused: true}, false, "message.received", true},
		{"focused allowed when toggle off", Prefs{Enabled: true, Mentions: true, OnlyWhenUnfocused: false}, true, "message.received", true},
		{"message needs a message category enabled", Prefs{Enabled: true, ConnectionLost: true}, false, "message.received", false},
		{"connection gated by its toggle", Prefs{Enabled: true, ConnectionLost: false}, false, "connection.lost", false},
		{"connection allowed", Prefs{Enabled: true, ConnectionLost: true}, false, "connection.lost", true},
		{"unknown event never notifies", Prefs{Enabled: true, PrivateMessages: true}, false, "user.joined", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := NewNotifier()
			n.SetPrefs(tc.prefs)
			n.SetFocused(tc.focused)
			if got := n.ShouldNotify(tc.eventType, nil); got != tc.want {
				t.Fatalf("ShouldNotify(%q) = %v, want %v", tc.eventType, got, tc.want)
			}
		})
	}
}

func TestNotifier_Send_usesDelivery(t *testing.T) {
	n := NewNotifier()
	fd := &fakeDelivery{}
	n.SetDelivery(fd)
	n.Send(Notification{ID: "1", Title: "PM from bob", CategoryID: "message"})
	if len(fd.withActions) != 1 || fd.withActions[0].Title != "PM from bob" {
		t.Fatalf("expected SendWithActions to receive the notification, got %+v", fd.withActions)
	}
}

func TestNotifier_Send_nilDeliveryIsNoop(t *testing.T) {
	NewNotifier().Send(Notification{ID: "1", Title: "x"}) // must not panic
}

func TestNotifier_RequestAuthorization_nilDelivery(t *testing.T) {
	ok, err := NewNotifier().RequestAuthorization()
	if ok || err != nil {
		t.Fatalf("nil delivery should report (false, nil), got (%v, %v)", ok, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/notification/ -count=1`
Expected: FAIL (compile errors — `SetPrefs`, `SetDelivery`, `Send`, `Prefs`, `Delivery` undefined).

- [ ] **Step 3: Rewrite `internal/notification/notification.go`**

```go
// Package notification decides when to raise a desktop notification and delegates
// the actual delivery to a platform-specific Delivery (wired in by the main
// package). It deliberately imports no Wails packages so the policy stays
// unit-testable.
package notification

import (
	"strings"
	"sync"

	"github.com/matt0x6f/irc-client/internal/logger"
)

// Prefs is a snapshot of the user's notification preferences. The main package
// pushes it in via SetPrefs on startup and whenever a notifications.* setting changes.
type Prefs struct {
	Enabled           bool
	PrivateMessages   bool
	Mentions          bool
	ConnectionLost    bool
	OnlyWhenUnfocused bool
}

// Action is a button on a notification.
type Action struct {
	ID          string
	Title       string
	Destructive bool // macOS-only styling
}

// Category groups actions (and an optional reply field) for reuse across notifications.
type Category struct {
	ID               string
	Actions          []Action
	HasReplyField    bool
	ReplyPlaceholder string
	ReplyButtonTitle string
}

// Notification is a platform-agnostic notification request. Data values must be
// strings — the map round-trips through the OS/JSON boundary on the way back.
type Notification struct {
	ID         string
	Title      string
	Subtitle   string
	Body       string
	CategoryID string
	Data       map[string]any
}

// Delivery is the platform delivery backend, implemented in the main package by
// an adapter over the Wails notifications service.
type Delivery interface {
	Send(Notification) error
	SendWithActions(Notification) error
	RegisterCategory(Category) error
	RequestAuthorization() (bool, error)
	CheckAuthorization() (bool, error)
}

// Notifier decides whether to notify and forwards to the Delivery.
type Notifier struct {
	focused  bool
	prefs    Prefs
	delivery Delivery
	mu       sync.RWMutex
}

// NewNotifier creates a new Notifier.
func NewNotifier() *Notifier {
	return &Notifier{}
}

// SetFocused updates whether the app window is currently focused.
func (n *Notifier) SetFocused(focused bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.focused = focused
}

// IsFocused returns whether the app window is currently focused.
func (n *Notifier) IsFocused() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.focused
}

// SetDelivery installs the platform delivery backend.
func (n *Notifier) SetDelivery(d Delivery) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.delivery = d
}

// SetPrefs replaces the current preferences snapshot.
func (n *Notifier) SetPrefs(p Prefs) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.prefs = p
}

// Prefs returns the current preferences snapshot.
func (n *Notifier) Prefs() Prefs {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.prefs
}

// RequestAuthorization asks the platform for notification permission. A nil
// delivery (e.g. unbundled/dev) reports (false, nil).
func (n *Notifier) RequestAuthorization() (bool, error) {
	n.mu.RLock()
	d := n.delivery
	n.mu.RUnlock()
	if d == nil {
		return false, nil
	}
	return d.RequestAuthorization()
}

// Send delivers an interactive notification. Safe to call from a goroutine; a nil
// delivery or a delivery error is logged at debug and otherwise ignored.
func (n *Notifier) Send(notif Notification) {
	n.mu.RLock()
	d := n.delivery
	n.mu.RUnlock()
	if d == nil {
		return
	}
	if err := d.SendWithActions(notif); err != nil {
		logger.Log.Debug().Err(err).Str("title", notif.Title).Msg("Desktop notification failed")
	}
}

// ShouldNotify is the coarse gate: notifications enabled, focus rule satisfied,
// and at least one relevant category enabled for the event. The caller refines
// the private-message vs mention decision using Prefs().
func (n *Notifier) ShouldNotify(eventType string, _ map[string]interface{}) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if !n.prefs.Enabled {
		return false
	}
	if n.prefs.OnlyWhenUnfocused && n.focused {
		return false
	}
	switch eventType {
	case "message.received":
		return n.prefs.PrivateMessages || n.prefs.Mentions
	case "connection.lost":
		return n.prefs.ConnectionLost
	default:
		return false
	}
}

// IsPrivateMessage checks if the event data represents a private message
// (target is not a channel, i.e. does not start with # or &).
func IsPrivateMessage(data map[string]interface{}) bool {
	channel, ok := data["channel"].(string)
	if !ok || channel == "" {
		return false
	}
	return channel[0] != '#' && channel[0] != '&'
}

// IsMention checks if the message text contains the given nickname (case-insensitive).
func IsMention(message, nickname string) bool {
	if nickname == "" {
		return false
	}
	return strings.Contains(strings.ToLower(message), strings.ToLower(nickname))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/notification/ -count=1`
Expected: PASS (all `TestShouldNotify_prefsMatrix` subtests + `TestNotifier_*`).

- [ ] **Step 5: Commit**

```bash
git add internal/notification/notification.go internal/notification/notification_test.go
git commit -m "feat(notifications): prefs-driven policy + Delivery interface"
```

---

### Task 2: Wails delivery adapter, response routing, and wiring (`main` package)

Add `app_notifications.go` (adapter + categories + response routing + bound
methods + prefs refresh + reconnect helper), wire it in `main.go`, update
`app_events.go` to build interactive notifications, add the `notifyWindow` field,
and refresh prefs on `notifications.*` setting writes. Then regenerate bindings.

**Files:**
- Create: `app_notifications.go`
- Create: `app_notifications_test.go`
- Modify: `app.go` (add `notifyWindow` field; refresh prefs in `SetSetting`)
- Modify: `app_events.go:280-342` (build interactive notifications)
- Modify: `main.go` (register service + window capture + AttachNotifications)

**Interfaces:**
- Consumes (from Task 1): `notification.{Prefs,Action,Category,Notification,Delivery}`, `Notifier.{SetDelivery,SetPrefs,Prefs,RequestAuthorization,Send}`.
- Consumes (existing): `App.SendMessage(networkID int64, target, message string) error`; `App.connectNetwork(NetworkConfig, bool) error`; `App.buildReconnectConfig(int64, *storage.Network) (NetworkConfig, error)`; `App.storage.GetNetwork(int64) (*storage.Network, error)`; `App.resolveNetworkID(map[string]interface{}) (int64, bool)`; `App.emit(string, ...any)`; `notifications.{New,NotificationService,NotificationOptions,NotificationCategory,NotificationAction,NotificationResult,NotificationResponse,DefaultActionIdentifier}`.
- Produces: bound `App.RequestNotificationPermission() (bool, error)`, bound `App.FocusMainWindow()`, `App.AttachNotifications(*notifications.NotificationService, *application.WebviewWindow)`, `App.refreshNotificationPrefs()`.

- [ ] **Step 1: Write the failing routing test**

Create `app_notifications_test.go`:

```go
package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

func TestRouteNotificationResponse(t *testing.T) {
	type emitted struct {
		name string
		data []any
	}
	newDeps := func(e *[]emitted, sent *[]string, recon *[]int64) notifyResponseDeps {
		return notifyResponseDeps{
			emit:        func(name string, data ...any) { *e = append(*e, emitted{name, data}) },
			sendMessage: func(id int64, target, msg string) error { *sent = append(*sent, target+"|"+msg); return nil },
			reconnect:   func(id int64) error { *recon = append(*recon, id); return nil },
		}
	}

	t.Run("default action navigates", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifications.DefaultActionIdentifier,
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "pm:alice"},
		}, newDeps(&e, &sent, &recon))
		if len(e) != 1 || e[0].name != "notification:navigate" {
			t.Fatalf("want one notification:navigate emit, got %+v", e)
		}
	})

	t.Run("mark read emits", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionMarkRead,
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "#go"},
		}, newDeps(&e, &sent, &recon))
		if len(e) != 1 || e[0].name != "notification:markRead" {
			t.Fatalf("want notification:markRead, got %+v", e)
		}
	})

	t.Run("reply strips pm prefix and sends to nick", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionReply,
			UserText:         "hi back",
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "pm:alice"},
		}, newDeps(&e, &sent, &recon))
		if len(sent) != 1 || sent[0] != "alice|hi back" {
			t.Fatalf("want reply to alice, got %+v", sent)
		}
	})

	t.Run("reply to channel mention keeps channel target", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionReply,
			UserText:         "yo",
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "#go"},
		}, newDeps(&e, &sent, &recon))
		if len(sent) != 1 || sent[0] != "#go|yo" {
			t.Fatalf("want reply to #go, got %+v", sent)
		}
	})

	t.Run("empty reply text is ignored", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionReply,
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "pm:alice"},
		}, newDeps(&e, &sent, &recon))
		if len(sent) != 0 {
			t.Fatalf("empty reply must not send, got %+v", sent)
		}
	})

	t.Run("reconnect parses network id", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionReconnect,
			UserInfo:         map[string]interface{}{"networkId": "42", "target": ""},
		}, newDeps(&e, &sent, &recon))
		if len(recon) != 1 || recon[0] != 42 {
			t.Fatalf("want reconnect(42), got %+v", recon)
		}
	})
}

func TestToOptionsAndCategory(t *testing.T) {
	d := &wailsDelivery{}
	opts := d.toOptions(notificationFixture())
	if opts.ID != "id1" || opts.CategoryID != "message" || opts.Data["target"] != "pm:bob" {
		t.Fatalf("toOptions mismatch: %+v", opts)
	}
	cat := d.toCategory(messageCategory())
	if cat.ID != "message" || !cat.HasReplyField || len(cat.Actions) != 2 || cat.Actions[0].ID != notifyActionReply {
		t.Fatalf("toCategory mismatch: %+v", cat)
	}
}
```

(Helpers `notificationFixture`/`messageCategory` are defined in Step 3.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 . -run 'TestRouteNotificationResponse|TestToOptionsAndCategory' -count=1`
Expected: FAIL (compile errors — `routeNotificationResponse`, `wailsDelivery`, `notifyAction*` undefined).

- [ ] **Step 3: Create `app_notifications.go`**

```go
package main

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/notification"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Notification category and action identifiers, shared with the response router.
const (
	notifyCategoryMessage    = "message"
	notifyCategoryConnection = "connection"

	notifyActionReply     = "REPLY"
	notifyActionMarkRead  = "MARK_READ"
	notifyActionReconnect = "RECONNECT"
)

// wailsDelivery adapts the Wails notifications service to notification.Delivery,
// translating our Wails-free structs into the framework's option types.
type wailsDelivery struct {
	svc *notifications.NotificationService
}

func newWailsDelivery(svc *notifications.NotificationService) *wailsDelivery {
	return &wailsDelivery{svc: svc}
}

func (d *wailsDelivery) toOptions(n notification.Notification) notifications.NotificationOptions {
	return notifications.NotificationOptions{
		ID:         n.ID,
		Title:      n.Title,
		Subtitle:   n.Subtitle,
		Body:       n.Body,
		CategoryID: n.CategoryID,
		Data:       n.Data,
	}
}

func (d *wailsDelivery) toCategory(c notification.Category) notifications.NotificationCategory {
	actions := make([]notifications.NotificationAction, len(c.Actions))
	for i, a := range c.Actions {
		actions[i] = notifications.NotificationAction{ID: a.ID, Title: a.Title, Destructive: a.Destructive}
	}
	return notifications.NotificationCategory{
		ID:               c.ID,
		Actions:          actions,
		HasReplyField:    c.HasReplyField,
		ReplyPlaceholder: c.ReplyPlaceholder,
		ReplyButtonTitle: c.ReplyButtonTitle,
	}
}

func (d *wailsDelivery) Send(n notification.Notification) error {
	return d.svc.SendNotification(d.toOptions(n))
}
func (d *wailsDelivery) SendWithActions(n notification.Notification) error {
	return d.svc.SendNotificationWithActions(d.toOptions(n))
}
func (d *wailsDelivery) RegisterCategory(c notification.Category) error {
	return d.svc.RegisterNotificationCategory(d.toCategory(c))
}
func (d *wailsDelivery) RequestAuthorization() (bool, error) {
	return d.svc.RequestNotificationAuthorization()
}
func (d *wailsDelivery) CheckAuthorization() (bool, error) {
	return d.svc.CheckNotificationAuthorization()
}

// AttachNotifications wires the Wails notification service into the app: it
// installs the delivery backend, registers the categories, hooks the response
// callback, captures the main window for focus, and seeds the prefs snapshot.
func (a *App) AttachNotifications(svc *notifications.NotificationService, win *application.WebviewWindow) {
	a.notifyWindow = win
	d := newWailsDelivery(svc)
	a.notifier.SetDelivery(d)

	if err := d.RegisterCategory(messageCategory()); err != nil {
		logger.Log.Debug().Err(err).Msg("register message notification category")
	}
	if err := d.RegisterCategory(connectionCategory()); err != nil {
		logger.Log.Debug().Err(err).Msg("register connection notification category")
	}

	svc.OnNotificationResponse(a.handleNotificationResponse)
	a.refreshNotificationPrefs()
}

func messageCategory() notification.Category {
	return notification.Category{
		ID: notifyCategoryMessage,
		Actions: []notification.Action{
			{ID: notifyActionReply, Title: "Reply"},
			{ID: notifyActionMarkRead, Title: "Mark as read"},
		},
		HasReplyField:    true,
		ReplyPlaceholder: "Message…",
		ReplyButtonTitle: "Send",
	}
}

func connectionCategory() notification.Category {
	return notification.Category{
		ID:      notifyCategoryConnection,
		Actions: []notification.Action{{ID: notifyActionReconnect, Title: "Reconnect"}},
	}
}

// RequestNotificationPermission asks the OS for notification permission. Bound to
// the frontend; called when the user enables notifications in settings.
func (a *App) RequestNotificationPermission() (bool, error) {
	return a.notifier.RequestAuthorization()
}

// FocusMainWindow raises the main window. Bound to the frontend and called from
// the notification:navigate handler so window focus runs on the framework's
// webview-request path rather than the off-main notification callback.
func (a *App) FocusMainWindow() {
	if a.notifyWindow != nil {
		a.notifyWindow.Focus()
	}
}

// refreshNotificationPrefs reloads the notifications.* settings into the notifier.
func (a *App) refreshNotificationPrefs() {
	a.notifier.SetPrefs(notification.Prefs{
		Enabled:           a.boolSetting("notifications.enabled", false),
		PrivateMessages:   a.boolSetting("notifications.privateMessages", true),
		Mentions:          a.boolSetting("notifications.mentions", true),
		ConnectionLost:    a.boolSetting("notifications.connectionLost", true),
		OnlyWhenUnfocused: a.boolSetting("notifications.onlyWhenUnfocused", true),
	})
}

func (a *App) boolSetting(key string, def bool) bool {
	v, err := a.storage.GetSetting(key)
	if err != nil || v == "" {
		return def
	}
	return v == "true"
}

// reconnectNetworkByID rebuilds the saved network config and reconnects it,
// flagging the client to rejoin every channel it was in.
func (a *App) reconnectNetworkByID(networkID int64) error {
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return err
	}
	cfg, err := a.buildReconnectConfig(networkID, network)
	if err != nil {
		return err
	}
	return a.connectNetwork(cfg, true)
}

// notifyResponseDeps are the seams routeNotificationResponse needs, so the
// routing logic is unit-testable without a live app.
type notifyResponseDeps struct {
	emit        func(name string, data ...any)
	sendMessage func(networkID int64, target, message string) error
	reconnect   func(networkID int64) error
}

// handleNotificationResponse is the live callback Wails invokes off the main
// thread. It builds the deps from the app and delegates to the pure router.
func (a *App) handleNotificationResponse(result notifications.NotificationResult) {
	if result.Error != nil {
		logger.Log.Debug().Err(result.Error).Msg("notification response error")
		return
	}
	routeNotificationResponse(result.Response, notifyResponseDeps{
		emit:        a.emit,
		sendMessage: a.SendMessage,
		reconnect:   a.reconnectNetworkByID,
	})
}

// routeNotificationResponse dispatches a notification response. UI-state actions
// (navigate, mark-read) emit events to the frontend; server-state actions (reply,
// reconnect) run in Go.
func routeNotificationResponse(resp notifications.NotificationResponse, deps notifyResponseDeps) {
	networkID, _ := parseNetworkID(resp.UserInfo["networkId"])
	target, _ := resp.UserInfo["target"].(string)

	switch resp.ActionIdentifier {
	case notifications.DefaultActionIdentifier:
		deps.emit("notification:navigate", map[string]any{"networkId": networkID, "target": target})
	case notifyActionMarkRead:
		deps.emit("notification:markRead", map[string]any{"networkId": networkID, "target": target})
	case notifyActionReply:
		if resp.UserText == "" {
			return
		}
		sendTarget := target
		if strings.HasPrefix(target, "pm:") {
			sendTarget = strings.TrimPrefix(target, "pm:")
		}
		if err := deps.sendMessage(networkID, sendTarget, resp.UserText); err != nil {
			logger.Log.Debug().Err(err).Msg("notification reply send failed")
		}
	case notifyActionReconnect:
		if err := deps.reconnect(networkID); err != nil {
			logger.Log.Debug().Err(err).Msg("notification reconnect failed")
		}
	}
}

// parseNetworkID extracts an int64 network id from a UserInfo value that may have
// crossed the JSON boundary (string is the canonical form; float64/int64 handled
// defensively).
func parseNetworkID(v any) (int64, bool) {
	switch t := v.(type) {
	case string:
		n, err := strconv.ParseInt(t, 10, 64)
		return n, err == nil
	case float64:
		return int64(t), true
	case int64:
		return t, true
	}
	return 0, false
}

// newNotificationID returns a random hex id for a notification.
func newNotificationID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// notificationFixture / test helper-facing constructors live with the tests.
func notificationFixture() notification.Notification {
	return notification.Notification{
		ID:         "id1",
		Title:      "PM from bob",
		Body:       "hi",
		CategoryID: notifyCategoryMessage,
		Data:       map[string]any{"networkId": "7", "target": "pm:bob", "kind": "pm"},
	}
}
```

- [ ] **Step 4: Add the `notifyWindow` field to the `App` struct**

In `app.go`, find the `App` struct field block near `notifier  *notification.Notifier`
(line ~32) and add below it:

```go
	notifier              *notification.Notifier
	notifyWindow          *application.WebviewWindow
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `CGO_ENABLED=1 go test -tags fts5 . -run 'TestRouteNotificationResponse|TestToOptionsAndCategory' -count=1`
Expected: PASS.

- [ ] **Step 6: Build interactive notifications in `app_events.go`**

Replace `handleDesktopNotification` body (lines ~289-333) message and connection
branches, and replace the `sendNotification` helper (lines ~335-342). Add imports
`"strconv"` and ensure `"github.com/matt0x6f/irc-client/internal/notification"` is
present (it already is).

Message branch (replace the `if notification.IsPrivateMessage(...) { ... } else if ... { ... }` block):

```go
		prefs := a.notifier.Prefs()
		if notification.IsPrivateMessage(event.Data) {
			if !prefs.PrivateMessages {
				return
			}
			a.sendNotification(notification.Notification{
				ID:         newNotificationID(),
				Title:      fmt.Sprintf("PM from %s", user),
				Body:       message,
				CategoryID: notifyCategoryMessage,
				Data: map[string]any{
					"networkId": strconv.FormatInt(networkID, 10),
					"target":    "pm:" + user,
					"kind":      "pm",
				},
			})
		} else if notification.IsMention(message, myNick) {
			if !prefs.Mentions {
				return
			}
			a.sendNotification(notification.Notification{
				ID:         newNotificationID(),
				Title:      fmt.Sprintf("Mention in %s", channel),
				Body:       fmt.Sprintf("%s: %s", user, message),
				CategoryID: notifyCategoryMessage,
				Data: map[string]any{
					"networkId": strconv.FormatInt(networkID, 10),
					"target":    channel,
					"kind":      "mention",
				},
			})
		}
```

Connection branch (replace the `case irc.EventConnectionLost:` body):

```go
	case irc.EventConnectionLost:
		networkAddress, _ := event.Data["network"].(string)
		networkID, _ := event.Data["networkId"].(int64)
		a.sendNotification(notification.Notification{
			ID:         newNotificationID(),
			Title:      fmt.Sprintf("Connection Lost: %s", networkAddress),
			Body:       "Cascade Chat lost connection to the server.",
			CategoryID: notifyCategoryConnection,
			Data: map[string]any{
				"networkId": strconv.FormatInt(networkID, 10),
				"target":    "",
				"kind":      "connection",
			},
		})
	}
```

`sendNotification` helper (replace lines ~335-342):

```go
// sendNotification dispatches an interactive desktop notification off the hot path.
func (a *App) sendNotification(n notification.Notification) {
	go a.notifier.Send(n)
}
```

- [ ] **Step 7: Refresh prefs when a notifications.* setting changes**

In `app.go`, in `SetSetting` (after the successful `a.emit("setting:changed", ...)`),
add before `return nil`:

```go
	if strings.HasPrefix(key, "notifications.") {
		a.refreshNotificationPrefs()
	}
```

Ensure `app.go` imports `"strings"` (add if missing).

- [ ] **Step 8: Wire the service in `main.go`**

Add the import:

```go
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
```

Create the service before `application.New` (after `ircApp, err := NewApp()` block):

```go
	ns := notifications.New()
```

Add it to the `Services` slice:

```go
		Services: []application.Service{
			application.NewService(ircApp),
			application.NewService(ns),
		},
```

Capture the window and attach (replace the bare `app.Window.NewWithOptions(...)` call
at line ~83 so its return is captured, then attach after it):

```go
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Cascade Chat",
		Width:            1280,
		Height:           800,
		MinWidth:         940,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(27, 38, 54),
	})

	// Wire native notifications now that the service and window exist.
	ircApp.AttachNotifications(ns, mainWindow)
```

- [ ] **Step 9: Build and run the full backend test suite**

Run: `CGO_ENABLED=1 go test -tags fts5 ./... -count=1`
Expected: PASS (notification package + main package routing/translation tests; existing suite green).

- [ ] **Step 10: Regenerate Wails bindings**

Run: `wails3 generate bindings -clean=true`
Expected: regenerates `frontend/wailsjs/go/main/App.*` with `RequestNotificationPermission` and `FocusMainWindow`, plus a `NotificationService` binding. (Model `$$createType` renumbering is benign.)

- [ ] **Step 11: Commit**

```bash
git add app_notifications.go app_notifications_test.go app.go app_events.go main.go frontend/wailsjs
git commit -m "feat(notifications): Wails delivery adapter, response routing, and wiring"
```

---

### Task 3: Remove the `beeep` dependency

**Files:**
- Modify: `go.mod`, `go.sum` (via `go mod tidy`)

- [ ] **Step 1: Confirm no remaining references**

Run: `grep -rn "beeep" --include="*.go" .`
Expected: no output (Task 1 removed the only import).

- [ ] **Step 2: Tidy modules**

Run: `go mod tidy`
Expected: `github.com/gen2brain/beeep` (and now-unused transitive deps like
`github.com/jackmordaunt/icns`) removed from `go.mod`/`go.sum`.

- [ ] **Step 3: Verify the build**

Run: `CGO_ENABLED=1 go build -tags fts5 ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(notifications): drop beeep dependency"
```

---

### Task 4: Notification settings store (frontend)

Add the five `notifications.*` keys to the settings store with defaults, hydration,
and cross-window reconciliation.

**Files:**
- Modify: `frontend/src/stores/settings.ts`
- Test: `frontend/src/stores/settings.notifications.test.ts` (create)

**Interfaces:**
- Produces (consumed by Tasks 5): store slices `notificationsEnabled`, `notifyPrivateMessages`, `notifyMentions`, `notifyConnectionLost`, `notifyOnlyWhenUnfocused` (all `boolean`) and setters `setNotificationsEnabled`, `setNotifyPrivateMessages`, `setNotifyMentions`, `setNotifyConnectionLost`, `setNotifyOnlyWhenUnfocused` (each `(v: boolean) => void`).

- [ ] **Step 1: Write the failing test**

Create `frontend/src/stores/settings.notifications.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

const setSetting = vi.fn(() => Promise.resolve());
const settingValues: Record<string, string> = {};
vi.mock('../../wailsjs/go/main/App', () => ({
  GetSetting: vi.fn((key: string) => Promise.resolve(settingValues[key] ?? '')),
  SetSetting: (...a: unknown[]) => setSetting(...a),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn() }));

import { useSettingsStore } from './settings';

describe('notification settings', () => {
  beforeEach(() => {
    setSetting.mockClear();
    useSettingsStore.setState({
      notificationsEnabled: false,
      notifyPrivateMessages: true,
    });
  });

  it('persists the master toggle under notifications.enabled', () => {
    useSettingsStore.getState().setNotificationsEnabled(true);
    expect(useSettingsStore.getState().notificationsEnabled).toBe(true);
    expect(setSetting).toHaveBeenCalledWith('notifications.enabled', 'true');
  });

  it('persists per-event toggles', () => {
    useSettingsStore.getState().setNotifyPrivateMessages(false);
    expect(setSetting).toHaveBeenCalledWith('notifications.privateMessages', 'false');
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/stores/settings.notifications.test.ts`
Expected: FAIL (`setNotificationsEnabled` is not a function).

- [ ] **Step 3: Extend the store**

In `frontend/src/stores/settings.ts`:

Add keys near the other key constants:

```ts
const NOTIFY_ENABLED_KEY = 'notifications.enabled';
const NOTIFY_PM_KEY = 'notifications.privateMessages';
const NOTIFY_MENTIONS_KEY = 'notifications.mentions';
const NOTIFY_CONNECTION_KEY = 'notifications.connectionLost';
const NOTIFY_UNFOCUSED_KEY = 'notifications.onlyWhenUnfocused';
```

Add to the `SettingsState` interface:

```ts
  notificationsEnabled: boolean;
  setNotificationsEnabled: (value: boolean) => void;
  notifyPrivateMessages: boolean;
  setNotifyPrivateMessages: (value: boolean) => void;
  notifyMentions: boolean;
  setNotifyMentions: (value: boolean) => void;
  notifyConnectionLost: boolean;
  setNotifyConnectionLost: (value: boolean) => void;
  notifyOnlyWhenUnfocused: boolean;
  setNotifyOnlyWhenUnfocused: (value: boolean) => void;
```

Add to the `create<SettingsState>((set) => ({ ... }))` body. Use a local helper to
keep it DRY:

```ts
  notificationsEnabled: false,
  setNotificationsEnabled: (value) => {
    set({ notificationsEnabled: value });
    SetSetting(NOTIFY_ENABLED_KEY, value ? 'true' : 'false').catch((e) =>
      console.error('Failed to persist notifications.enabled:', e),
    );
  },
  notifyPrivateMessages: true,
  setNotifyPrivateMessages: (value) => {
    set({ notifyPrivateMessages: value });
    SetSetting(NOTIFY_PM_KEY, value ? 'true' : 'false').catch((e) =>
      console.error('Failed to persist notifications.privateMessages:', e),
    );
  },
  notifyMentions: true,
  setNotifyMentions: (value) => {
    set({ notifyMentions: value });
    SetSetting(NOTIFY_MENTIONS_KEY, value ? 'true' : 'false').catch((e) =>
      console.error('Failed to persist notifications.mentions:', e),
    );
  },
  notifyConnectionLost: true,
  setNotifyConnectionLost: (value) => {
    set({ notifyConnectionLost: value });
    SetSetting(NOTIFY_CONNECTION_KEY, value ? 'true' : 'false').catch((e) =>
      console.error('Failed to persist notifications.connectionLost:', e),
    );
  },
  notifyOnlyWhenUnfocused: true,
  setNotifyOnlyWhenUnfocused: (value) => {
    set({ notifyOnlyWhenUnfocused: value });
    SetSetting(NOTIFY_UNFOCUSED_KEY, value ? 'true' : 'false').catch((e) =>
      console.error('Failed to persist notifications.onlyWhenUnfocused:', e),
    );
  },
```

In `initSettings`, extend the `Promise.all` and `setState`:

```ts
    const [consolidate, prefixMode, channel, nEnabled, nPm, nMentions, nConn, nUnfocused] = await Promise.all([
      GetSetting(CONSOLIDATE_JOIN_QUIT_KEY),
      GetSetting(PREFIX_DISPLAY_MODE_KEY),
      GetSetting(UPDATE_CHANNEL_KEY),
      GetSetting(NOTIFY_ENABLED_KEY),
      GetSetting(NOTIFY_PM_KEY),
      GetSetting(NOTIFY_MENTIONS_KEY),
      GetSetting(NOTIFY_CONNECTION_KEY),
      GetSetting(NOTIFY_UNFOCUSED_KEY),
    ]);
    useSettingsStore.setState({
      consolidateJoinQuit: consolidate === 'true',
      ...(prefixMode === 'text' || prefixMode === 'icon'
        ? { prefixDisplayMode: prefixMode }
        : {}),
      ...(isUpdateChannel(channel) ? { updateChannel: channel } : {}),
      notificationsEnabled: nEnabled === 'true',
      // Per-event toggles default to ON: treat only an explicit 'false' as off.
      notifyPrivateMessages: nPm !== 'false',
      notifyMentions: nMentions !== 'false',
      notifyConnectionLost: nConn !== 'false',
      notifyOnlyWhenUnfocused: nUnfocused !== 'false',
    });
```

In the `EventsOn('setting:changed', ...)` handler, add reconciliation branches:

```ts
    } else if (payload.key === NOTIFY_ENABLED_KEY) {
      useSettingsStore.setState({ notificationsEnabled: payload.value === 'true' });
    } else if (payload.key === NOTIFY_PM_KEY) {
      useSettingsStore.setState({ notifyPrivateMessages: payload.value !== 'false' });
    } else if (payload.key === NOTIFY_MENTIONS_KEY) {
      useSettingsStore.setState({ notifyMentions: payload.value !== 'false' });
    } else if (payload.key === NOTIFY_CONNECTION_KEY) {
      useSettingsStore.setState({ notifyConnectionLost: payload.value !== 'false' });
    } else if (payload.key === NOTIFY_UNFOCUSED_KEY) {
      useSettingsStore.setState({ notifyOnlyWhenUnfocused: payload.value !== 'false' });
    }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd frontend && npx vitest run src/stores/settings.notifications.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/settings.ts frontend/src/stores/settings.notifications.test.ts
git commit -m "feat(notifications): settings store keys for notification prefs"
```

---

### Task 5: Notifications settings section (frontend UI)

Add a "Notifications" pane to the settings panel: master toggle (requests OS
permission on enable) + four sub-toggles (disabled while master is off).

**Files:**
- Modify: `frontend/src/components/settings-panel.tsx`

**Interfaces:**
- Consumes (Task 4): the new store slices/setters; bound `RequestNotificationPermission` (Task 2).

- [ ] **Step 1: Extend the section union and nav**

In `frontend/src/components/settings-panel.tsx`:

Update the type and guard (line ~17-20):

```ts
export type SettingsSection = 'networks' | 'plugins' | 'display' | 'notifications' | 'advanced' | 'about';

export const isSettingsSection = (v: string): v is SettingsSection =>
  v === 'networks' || v === 'plugins' || v === 'display' || v === 'notifications' || v === 'advanced' || v === 'about';
```

Add to `navItems` (line ~1393), after the `display` entry:

```ts
    { id: 'notifications', label: 'Notifications' },
```

- [ ] **Step 2: Import the bound method and store slices**

Add `RequestNotificationPermission` to the `App` import (line ~3):

```ts
import { GetNetworks, SaveNetwork, ConnectNetwork, DeleteNetwork, DisconnectNetwork, GetConnectionStatus, GetServers, ListPlugins, EnablePlugin, DisablePlugin, ReloadPlugin, GetBuildInfo, CheckForUpdates, GetLogConfig, SetLogConfig, GetDefaultLogPath, GetSTSPolicies, ClearSTSPolicy, RequestNotificationPermission } from '../../wailsjs/go/main/App';
```

Inside the `SettingsPanel` component, near the other `useSettingsStore` slices (line ~87):

```ts
  const notificationsEnabled = useSettingsStore((s) => s.notificationsEnabled);
  const setNotificationsEnabled = useSettingsStore((s) => s.setNotificationsEnabled);
  const notifyPrivateMessages = useSettingsStore((s) => s.notifyPrivateMessages);
  const setNotifyPrivateMessages = useSettingsStore((s) => s.setNotifyPrivateMessages);
  const notifyMentions = useSettingsStore((s) => s.notifyMentions);
  const setNotifyMentions = useSettingsStore((s) => s.setNotifyMentions);
  const notifyConnectionLost = useSettingsStore((s) => s.notifyConnectionLost);
  const setNotifyConnectionLost = useSettingsStore((s) => s.setNotifyConnectionLost);
  const notifyOnlyWhenUnfocused = useSettingsStore((s) => s.notifyOnlyWhenUnfocused);
  const setNotifyOnlyWhenUnfocused = useSettingsStore((s) => s.setNotifyOnlyWhenUnfocused);
  const [notifyNotice, setNotifyNotice] = useState<string | null>(null);
```

- [ ] **Step 3: Add the enable handler and the `notifications` case**

Inside the component, near the other handlers (e.g. after `handleDisconnect`, ~line 504):

```ts
  const handleToggleNotifications = async (v: boolean) => {
    if (v) {
      try {
        const granted = await RequestNotificationPermission();
        setNotifyNotice(
          granted
            ? null
            : 'Notifications are blocked. Enable Cascade Chat in System Settings → Notifications, then toggle this again.',
        );
      } catch (e) {
        setNotifyNotice(String(e));
      }
    } else {
      setNotifyNotice(null);
    }
    setNotificationsEnabled(v);
  };
```

In `renderContent`'s `switch`, add a case before `case 'advanced':`:

```tsx
      case 'notifications':
        return (
          <div className="mb-6">
            <h3 className="text-md font-semibold mb-4">Notifications</h3>
            <div className="space-y-4">
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)]">
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Enable desktop notifications</span>
                  <Toggle checked={notificationsEnabled} onChange={(v) => void handleToggleNotifications(v)} />
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  Show native desktop notifications for messages and connection events. Requires granting permission the first time. Notifications only appear in installed builds.
                </p>
                {notifyNotice && (
                  <p className="text-xs text-amber-500 mt-2" data-testid="notify-permission-notice">
                    {notifyNotice}
                  </p>
                )}
              </div>

              <div className={`border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)] space-y-3 ${notificationsEnabled ? '' : 'opacity-50 pointer-events-none'}`}>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Private messages</span>
                  <Toggle checked={notifyPrivateMessages} onChange={setNotifyPrivateMessages} />
                </div>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Mentions</span>
                  <Toggle checked={notifyMentions} onChange={setNotifyMentions} />
                </div>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Connection lost</span>
                  <Toggle checked={notifyConnectionLost} onChange={setNotifyConnectionLost} />
                </div>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Only when window is unfocused</span>
                  <Toggle checked={notifyOnlyWhenUnfocused} onChange={setNotifyOnlyWhenUnfocused} />
                </div>
              </div>
            </div>
          </div>
        );
```

- [ ] **Step 4: Type-check the frontend**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors. (If `RequestNotificationPermission` is missing, Task 2 Step 10's binding regeneration was skipped — run it.)

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/settings-panel.tsx
git commit -m "feat(notifications): settings UI for notification preferences"
```

---

### Task 6: Notification response routing (frontend)

Listen for `notification:navigate` and `notification:markRead` and act on the
network store; focus the window via the bound method.

**Files:**
- Create: `frontend/src/hooks/useNotificationRouting.ts`
- Modify: `frontend/src/App.tsx`
- Test: `frontend/src/hooks/useNotificationRouting.test.ts` (create)

**Interfaces:**
- Consumes: `useNetworkStore.getState().selectPane(networkId, target)`, `.clearActivity('${networkId}:${target}')`; bound `FocusMainWindow` (Task 2); `EventsOn('notification:navigate' | 'notification:markRead')`.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/hooks/useNotificationRouting.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

const handlers: Record<string, (p: unknown) => void> = {};
const selectPane = vi.fn();
const clearActivity = vi.fn();
const focusMainWindow = vi.fn();

vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (p: unknown) => void) => {
    handlers[name] = cb;
    return () => delete handlers[name];
  },
}));
vi.mock('../../wailsjs/go/main/App', () => ({ FocusMainWindow: (...a: unknown[]) => focusMainWindow(...a) }));
vi.mock('../stores/network', () => ({
  useNetworkStore: { getState: () => ({ selectPane, clearActivity }) },
}));

import { registerNotificationRouting } from './useNotificationRouting';

describe('notification routing', () => {
  beforeEach(() => {
    selectPane.mockClear();
    clearActivity.mockClear();
    focusMainWindow.mockClear();
  });

  it('navigate selects the pane and focuses the window', () => {
    registerNotificationRouting();
    handlers['notification:navigate']({ networkId: 7, target: 'pm:alice' });
    expect(selectPane).toHaveBeenCalledWith(7, 'pm:alice');
    expect(focusMainWindow).toHaveBeenCalled();
  });

  it('mark read clears the activity key', () => {
    registerNotificationRouting();
    handlers['notification:markRead']({ networkId: 7, target: '#go' });
    expect(clearActivity).toHaveBeenCalledWith('7:#go');
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/hooks/useNotificationRouting.test.ts`
Expected: FAIL (`registerNotificationRouting` not found).

- [ ] **Step 3: Create the hook**

Create `frontend/src/hooks/useNotificationRouting.ts`:

```ts
import { useEffect } from 'react';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { FocusMainWindow } from '../../wailsjs/go/main/App';
import { useNetworkStore } from '../stores/network';

interface NavPayload {
  networkId: number;
  target: string;
}

/**
 * registerNotificationRouting subscribes to backend notification-response events
 * and routes them into the network store. Returns an unsubscribe function.
 * Extracted from the hook so it can be unit-tested without a React renderer.
 */
export function registerNotificationRouting(): () => void {
  const offNav = EventsOn('notification:navigate', (p: NavPayload) => {
    if (p?.target) {
      void useNetworkStore.getState().selectPane(p.networkId, p.target);
    }
    void FocusMainWindow();
  });
  const offRead = EventsOn('notification:markRead', (p: NavPayload) => {
    if (p?.target) {
      useNetworkStore.getState().clearActivity(`${p.networkId}:${p.target}`);
    }
  });
  return () => {
    offNav();
    offRead();
  };
}

/** useNotificationRouting wires notification-response routing for the app lifetime. */
export function useNotificationRouting(): void {
  useEffect(() => registerNotificationRouting(), []);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd frontend && npx vitest run src/hooks/useNotificationRouting.test.ts`
Expected: PASS.

- [ ] **Step 5: Call the hook from `App.tsx`**

In `frontend/src/App.tsx`, add the import after the other hook/store imports (line ~6):

```ts
import { useNotificationRouting } from './hooks/useNotificationRouting';
```

Inside the `App()` component body, after the store-slice declarations (after line ~54, `const restoreLastPane = ...`), add:

```ts
  useNotificationRouting();
```

- [ ] **Step 6: Type-check and run the full frontend suite**

Run: `cd frontend && npx tsc --noEmit && npx vitest run`
Expected: no type errors; all tests pass (the vitest job is known-flaky — rerun once if an unrelated test fails).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/hooks/useNotificationRouting.ts frontend/src/hooks/useNotificationRouting.test.ts frontend/src/App.tsx
git commit -m "feat(notifications): route notification responses into the app"
```

---

### Task 7: Full verification and manual smoke test

**Files:** none (verification only)

- [ ] **Step 1: Run all checks**

Run: `task check`
Expected: fmt, lint, type-check, and tests all pass. (If gofmt flags files, run
`gofmt -w` on them and re-run.)

- [ ] **Step 2: Build a packaged app**

Run: `task build`
Expected: a signed `.app` under `bin/` (notifications need the bundle).

- [ ] **Step 3: Manual smoke checklist (packaged app only)**

Launch the built `.app` and verify:

- [ ] Settings → Notifications → enable master → macOS permission prompt appears; granting it leaves the toggle on.
- [ ] With another user, receive a PM while the window is unfocused → a "PM from <nick>" notification appears with **Reply** and **Mark as read**.
- [ ] Type into **Reply** and send → the message is delivered as a PRIVMSG to that user (visible in the DM).
- [ ] Click a mention notification body → the window focuses and jumps to the channel; the unread badge clears.
- [ ] **Mark as read** on a notification → the originating pane's unread badge clears without focusing.
- [ ] Toggle "Mentions" off → a channel mention produces no notification; a PM still does.
- [ ] Toggle "Only when window is unfocused" off → a PM notifies even while the app is focused.
- [ ] Kill a network's connection → a "Connection Lost" notification with **Reconnect**; clicking it reconnects.

- [ ] **Step 4: Commit any formatting fixes**

```bash
git add -A
git commit -m "chore(notifications): formatting and verification fixes" || echo "nothing to commit"
```

---

## Self-Review Notes

- **Spec coverage:** Delivery interface + plain structs (Task 1) ✓; Wails adapter + categories + response routing + bound methods + wiring (Task 2) ✓; beeep removal (Task 3) ✓; settings model 5 keys + hydrate + reconcile (Task 4) ✓; settings UI with master gating + auth request (Task 5) ✓; click-to-jump / mark-read / reply / reconnect routing (Tasks 2+6) ✓; bindings regen + Info.plist note (Task 2 / Constraints) ✓; tests at every layer ✓; manual packaged smoke (Task 7) ✓.
- **Type consistency:** `notifyCategoryMessage`/`notifyCategoryConnection` and `notifyActionReply`/`notifyActionMarkRead`/`notifyActionReconnect` are defined once in Task 2 and reused in `app_events.go` and the router. `networkId` is a decimal string in `Data`/`UserInfo`, parsed via `parseNetworkID`, and emitted to the frontend as a number. `target` is `'pm:<nick>'` or `'#channel'`, matching `selectPane`'s channel arg and the `${networkId}:${target}` activity key.
- **Info.plist:** verify the build config's `Info.plist` carries a bundle identifier (Cocoa service refuses to start otherwise) — checked during Task 7 Step 2 packaging.

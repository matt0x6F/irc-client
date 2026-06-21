# Native desktop notifications via the Wails v3 notification service

**Branch:** `matt/loving-euclid-ba4add`

## Problem

Desktop notifications currently go through the third-party `beeep` library
([internal/notification/notification.go](../../internal/notification/notification.go)).
On macOS `beeep` shells out to `terminal-notifier`/`osascript`, which means:

- The notification is attributed to the helper, not Cascade (wrong icon/identity).
- There are **no action buttons, no inline reply, and no click callback** — the
  subprocess exits the moment the toast is posted, so a user can never click a
  ping to jump back into the conversation or reply from the banner.

Wails v3 ships a first-party `notifications` service
(`github.com/wailsapp/wails/v3/pkg/services/notifications`, already present as a
transitive dependency of `wails/v3 v3.0.0-alpha2.103`) that delivers true native
notifications with categories, action buttons, inline reply, and a response
callback. We adopt it and remove `beeep`.

## Decisions (from product owner)

- **Full interactive.** Body-click focuses the window and jumps to the
  originating channel/DM; PM/mention notifications carry an inline **Reply** field
  and a **Mark as read** button; connection-lost notifications carry a
  **Reconnect** button. No invite/"Join" notification (none exists today — out of
  scope).
- **All-in on the Wails service.** `beeep` is removed entirely. The Cocoa service
  hard-fails without a signed `.app` bundle, so notifications only fire in a real
  `wails3 build`; nothing fires in `task dev`. This is accepted.
- **Settings-driven and granular.** The end user chooses what they are notified
  about via a new "Notifications" settings section. The master toggle also drives
  the macOS authorization request.
- **Interactive buttons are always-on** for their notification type — not
  individually toggleable. The per-event toggles already control whether a
  notification appears at all.

## Architecture

Approach: keep `internal/notification` Wails-free and unit-tested; put all
framework glue in the `main` package alongside the other bound methods
(per CLAUDE.md: "all Wails-bound methods on the `App` struct, split by file").

```
main.go ── notifications.New() ──┐ (added to application.Options.Services)
           ircApp.AttachNotifications(ns, app)
                                 ▼
app_notifications.go (new)
  • wailsDelivery   → implements notification.Delivery, wraps
                      *notifications.NotificationService, translates our plain
                      structs → notifications.NotificationOptions
  • registers categories (pm/mention, connection) at attach time
  • handleNotificationResponse(result) → routes by ActionIdentifier
  • bound method: RequestNotificationPermission() (bool, error)
                                 │ injects Delivery
                                 ▼
internal/notification (stays Wails-free)
  • Delivery interface: Send, SendWithActions, RegisterCategory,
    RequestAuthorization, CheckAuthorization
  • plain structs: Notification, Action, Category (no wails/v3 import)
  • Notifier: focus tracking + prefs snapshot + policy
  • ShouldNotify(eventType, data) consults prefs
```

The plain-struct ↔ `NotificationOptions` translation in `wailsDelivery` is the
seam that keeps the leaf package free of the cgo-backed framework types.

## Data flow

**Outbound.** IRC event → `handleDesktopNotification`
([app_events.go:280](../../app_events.go)) → `Notifier.ShouldNotify` (now
prefs-driven) → `Notifier` builds a `Notification` with a UUID `ID`, title/body,
`CategoryID`, and `Data` routing map `{networkID, target, kind}` →
`Delivery.SendWithActions` → Wails Cocoa service.

**Inbound.** User interaction → Wails invokes `handleNotificationResponse(result)`.
The `Data` map returns as `result.Response.UserInfo`. Routing by
`result.Response.ActionIdentifier`:

| Action            | Handled where | Effect                                                        |
| ----------------- | ------------- | ------------------------------------------------------------- |
| Default (body)    | emit event    | `notification:navigate {networkID,target}` → focus + select   |
| `MARK_READ`       | emit event    | `notification:markRead {networkID,target}` → clear unread     |
| `REPLY` + UserText| backend       | PRIVMSG `UserText` to `target` via existing send path         |
| `RECONNECT`       | backend       | connect `networkID` via existing connection path              |

Split rule: actions that change **server state** (reply, reconnect) run in Go;
actions that change **UI state** (navigate, mark-read) emit a Wails event the
frontend already knows how to act on.

## Backend (Go)

1. **`internal/notification/notification.go`**
   - Add `Delivery` interface and plain `Notification`/`Action`/`Category` structs
     (no `wails/v3` import).
   - `Notifier` gains a `delivery Delivery` field and an atomic `prefs` snapshot
     (master, privateMessages, mentions, connectionLost, onlyWhenUnfocused).
   - `SetDelivery(Delivery)` and `SetPrefs(Prefs)` setters.
   - Rewrite `ShouldNotify` to consult `prefs`: master gate, per-event gates,
     and `onlyWhenUnfocused` (replacing the hardcoded `IsFocused()` gate).
   - `Notify` is replaced by a method that builds a `Notification` (UUID ID,
     category, routing `Data`) and calls `Delivery.SendWithActions`; logs at debug
     and no-ops on error (matching today's `beeep` failure path).

2. **`app_notifications.go`** (new)
   - `wailsDelivery` implementing `notification.Delivery`, wrapping
     `*notifications.NotificationService`; translates plain structs →
     `notifications.NotificationOptions` / `NotificationCategory`.
   - `AttachNotifications(ns *notifications.NotificationService, app *application.App)`:
     `notifier.SetDelivery(...)`, registers the `pm/mention` category
     (`HasReplyField: true`, actions `REPLY`, `MARK_READ`) and `connection`
     category (action `RECONNECT`), and sets `ns.OnNotificationResponse(a.handleNotificationResponse)`.
   - `handleNotificationResponse(result)`: route per the table above. Reply →
     existing message-send path; reconnect → existing connect path; navigate /
     mark-read → `app.Event.Emit(...)`.
   - Bound `RequestNotificationPermission() (bool, error)` → `Delivery.RequestAuthorization`.
   - Prefs plumbing: hydrate `notifier` prefs on startup; re-push on
     `notifications.*` `SetSetting` writes.

3. **`main.go`**
   - Create `ns := notifications.New()`, add `application.NewService(ns)` to the
     `Services` list ([main.go:31](../../main.go)), and call
     `ircApp.AttachNotifications(ns, app)` after `application.New(...)`.

4. **`app_events.go`**
   - `handleDesktopNotification` ([app_events.go:280](../../app_events.go)) sets
     `CategoryID`/`kind` and the routing `Data` (networkID, target) when building
     notifications; otherwise unchanged.

5. **Threading.** `OnNotificationResponse` fires off the main thread (see the
   `wails-appevent-listeners-off-main-thread` note). Reply/reconnect/`Event.Emit`
   are goroutine-safe; native window **focus** is routed through the frontend
   `notification:navigate` event rather than calling AppKit from the callback,
   sidestepping the main-queue requirement.

## Frontend (TypeScript/React)

1. **`frontend/src/stores/settings.ts`** — add five keyed settings under
   `notifications.*` (`enabled`, `privateMessages`, `mentions`, `connectionLost`,
   `onlyWhenUnfocused`) with defaults (master off; the rest on); hydrate in the
   existing `Promise.all` block.

2. **`frontend/src/components/settings-panel.tsx`** — a "Notifications" section
   with the five toggles; sub-toggles disabled while master is off. Turning master
   **on** calls `RequestNotificationPermission()`; if denied, show a hint to enable
   in System Settings and reflect the real state in the toggle.

3. **Response routing** — a listener (App-level) for `notification:navigate`
   (focus + select network/target via the network store) and
   `notification:markRead` (clear that target's unread badge the same way clicking
   into the channel does).

## Testing

- **Unit (`internal/notification`):** `ShouldNotify` across the prefs matrix
  (each toggle × focused/unfocused) with a fake `Delivery`; retain
  `IsPrivateMessage`/`IsMention` tests. No Wails import.
- **Unit (`main`):** `wailsDelivery` struct translation (plain → `NotificationOptions`);
  `handleNotificationResponse` routing via injected seams (emit func + send func)
  asserting the correct branch per `ActionIdentifier`.
- **Frontend:** settings store hydrate/persist for the new keys (mirroring
  [settings.update-channel.test.ts](../../frontend/src/stores/settings.update-channel.test.ts));
  event-routing handler test for `notification:navigate`/`markRead`.
- **Manual:** packaged `.app` smoke checklist (the only place real notifications
  render): authorize via settings; PM → reply from banner lands as PRIVMSG; mention
  → body-click focuses + jumps; mark-as-read clears badge; kill a connection →
  reconnect button restores it.

## Housekeeping

- Remove `github.com/gen2brain/beeep` from `go.mod`; `go mod tidy`.
- Regenerate bindings: `wails3 generate bindings -clean=true` (new bound method +
  the `NotificationService` frontend binding). See `wails-bindings-regenerate` note.
- Verify the build's `Info.plist` carries a bundle identifier (the Cocoa service
  refuses to start without one).

## Platform notes

- **macOS:** full feature set; requires bundled + signed `.app`.
- **Windows:** full feature set (actions + reply); no bundle/signing gate.
- **Linux:** D-Bus backend ignores `HasReplyField` — inline reply silently absent,
  everything else works. No app-side special-casing; the service degrades for us.

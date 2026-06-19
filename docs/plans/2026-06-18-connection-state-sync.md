# Connection State Sync Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the UI's connection state (header badge, sidebar dots) and the channel nick list stay faithful to the real IRC socket state, including after silent socket death (sleep/wake, NAT drop).

**Architecture:** Defense-in-depth liveness detection in the Go IRC client (tighter library ping + a guarded staleness watchdog + a sleep/wake probe), all funneling through the existing `DisconnectCallback` → `EventConnectionLost` → auto-reconnect path so there is still a single teardown path. On the frontend, make the 2s poll authoritative for *all* networks and make event application order-safe via the timestamp the backend already sends, and refresh the nick list when a (re)connect lands.

**Tech Stack:** Go (`ergochat/irc-go` v0.4.0, Wails v3), React 19 + Zustand + TypeScript, Vitest.

---

## ⚠️ Correction (post-implementation review)

The original plan below proposed a **guarded staleness watchdog** (Tasks 2–4) keyed on
`lastMessageTime`, on the stated assumption that the read loop's catch-all callback
(`conn.AddCallback("", …)`) bumps `lastMessageTime` on every inbound message. **That
assumption is false.** `ergochat/irc-go@v0.4.0` `addCallback` discards `command == ""`
(`ircevent/irc_callback.go:36-38`), so the catch-all registers nothing and
`lastMessageTime` is only ever set once, at connect. A watchdog keyed on it would mark
*every* connection stale ~90s after connect and tear down healthy links — re-introducing
the exact #13 failure. (This is also the likely undiagnosed root cause of #13 itself: the
old passive staleness check read the same frozen field.)

**Resolution (implemented):** liveness detection is owned entirely by the library's
PING/PONG ping loop, simply tuned faster (Task 1: `Timeout 30s / KeepAlive 60s`, dead-socket
detection within ~90s for idle and busy links alike). The `isStale` helper, the watchdog
goroutine, `lastMessageTime`, and the dead catch-all were **removed**. The sleep/wake probe
(Task 4) was reworked to `ForceReconnect`/`reconnectAllOnWake`: on `SystemDidWake`, every
*auto-connect* network is force-reconnected via `conn.Quit()` → the normal
`DisconnectCallback` → auto-reconnect path (no `lastMessageTime` involved). All Phase 2/3
frontend work stands unchanged. Tasks 2–4 below are retained for historical context but do
**not** reflect the merged code.

---

## Background: the root cause

`c.connected` (the one bool behind `IsConnected()` → `GetConnectionStatus`) is flipped to `false` by exactly one path: the library `DisconnectCallback`, driven by the ping loop tuned to `Timeout 1m / KeepAlive 3m` ([client.go:440](../../internal/irc/client.go)). Commit `bf33a50` (#13) deliberately removed the active staleness check (`lastMessageTime` is now "informational only", [client.go:42](../../internal/irc/client.go)) to fix the inverse "stuck disconnected" bug. Result: a silently-dead socket reads "Connected" for minutes, and the 2s poll can't help because it reads the same stale bool.

Two amplifiers: the event bus dispatches `go sub.OnEvent` with no ordering ([bus.go:91](../../internal/events/bus.go)), and the interval poll only refreshes `selectedNetwork` ([App.tsx:248](../../frontend/src/App.tsx)), so non-selected sidebar dots ride on unordered events alone. The nick list ([channel-info.tsx](../../frontend/src/components/channel-info.tsx)) only refreshes on `message-event`, never on reconnect.

**~~The #13 guard (critical invariant)~~ — FALSE, see the Correction above:** the original plan assumed `lastMessageTime` is updated by the read loop's catch-all callback on *every* inbound message. It is not — `ergochat` discards the `""` catch-all, so the field never advances after connect. The merged implementation therefore relies on the library's PING/PONG loop for liveness instead of any `lastMessageTime`-based check.

## Test commands

- Backend: `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1`
- Frontend: `cd frontend && npx vitest run`

---

## Phase 1 — Backend liveness detection

### Task 1: Tighten the library ping cadence

**Files:**
- Modify: `internal/constants/constants.go`
- Modify: `internal/irc/client.go:440-441`
- Test: `internal/irc/client_test.go`

- [ ] **Step 1: Add liveness constants**

In `internal/constants/constants.go`, inside the existing `const (...)` block, add:

```go
	// ConnectionReadTimeout is how long a socket read/write may stall before the
	// library treats the connection as dead (enforced via socket deadlines).
	ConnectionReadTimeout = 30 * time.Second
	// ConnectionKeepAlive is how often the library sends a keepalive PING. The
	// library requires KeepAlive >= Timeout.
	ConnectionKeepAlive = 60 * time.Second
	// ConnectionStaleThreshold is how long the client may go with NO inbound
	// traffic at all before the watchdog forces a teardown. Must be > KeepAlive
	// so a healthy idle link (which still gets PING/PONG every KeepAlive) is
	// never flagged.
	ConnectionStaleThreshold = 90 * time.Second
	// ConnectionWatchdogInterval is how often the watchdog re-checks staleness.
	ConnectionWatchdogInterval = 15 * time.Second
```

- [ ] **Step 2: Write the failing test for the KeepAlive >= Timeout invariant**

Add to `internal/irc/client_test.go`:

```go
import "github.com/matt0x6f/irc-client/internal/constants"

// The library requires KeepAlive >= Timeout; the watchdog must only fire after
// a healthy idle link would have already PINGed, so StaleThreshold > KeepAlive.
func TestLivenessConstantOrdering(t *testing.T) {
	if constants.ConnectionKeepAlive < constants.ConnectionReadTimeout {
		t.Fatalf("KeepAlive (%v) must be >= Timeout (%v)", constants.ConnectionKeepAlive, constants.ConnectionReadTimeout)
	}
	if constants.ConnectionStaleThreshold <= constants.ConnectionKeepAlive {
		t.Fatalf("StaleThreshold (%v) must be > KeepAlive (%v)", constants.ConnectionStaleThreshold, constants.ConnectionKeepAlive)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run TestLivenessConstantOrdering -count=1`
Expected: FAIL — `constants.ConnectionKeepAlive` undefined (until Step 1 compiles) or, if Step 1 is in, PASS. If it fails to compile because constants aren't referenced elsewhere yet, proceed to Step 4 first, then re-run.

- [ ] **Step 4: Wire the constants into the connection**

In `internal/irc/client.go`, replace lines 440-441:

```go
		Timeout:   1 * time.Minute,
		KeepAlive: 3 * time.Minute,
```

with:

```go
		Timeout:   constants.ConnectionReadTimeout,
		KeepAlive: constants.ConnectionKeepAlive,
```

Confirm `internal/constants` is already imported in `client.go` (it is, via `constants.ConnectionCleanupDelay`).

- [ ] **Step 5: Run the test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run TestLivenessConstantOrdering -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/constants/constants.go internal/irc/client.go internal/irc/client_test.go
git commit -m "fix(irc): tighten keepalive/timeout for faster dead-socket detection"
```

---

### Task 2: Add a pure `isStale` check on the client

**Files:**
- Modify: `internal/irc/client.go`
- Test: `internal/irc/client_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/irc/client_test.go`:

```go
// isStale must NEVER report a connection dead while the read loop is delivering
// traffic (the #13 regression). It only reports stale when connected AND no
// inbound message has arrived within the threshold.
func TestIsStale(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	threshold := 90 * time.Second

	cases := []struct {
		name      string
		connected bool
		lastMsg   time.Time
		want      bool
	}{
		{"fresh traffic", true, now.Add(-1 * time.Second), false},
		{"idle but within threshold", true, now.Add(-30 * time.Second), false},
		{"silent past threshold", true, now.Add(-120 * time.Second), true},
		{"not connected is never stale", false, now.Add(-999 * time.Second), false},
		{"zero lastMessageTime while connected is not stale", true, time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &IRCClient{connected: tc.connected, lastMessageTime: tc.lastMsg}
			if got := c.isStale(now, threshold); got != tc.want {
				t.Fatalf("isStale=%v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run TestIsStale -count=1`
Expected: FAIL — `c.isStale undefined`

- [ ] **Step 3: Implement `isStale`**

In `internal/irc/client.go`, add immediately after `IsConnectedDirect` (around line 212):

```go
// isStale reports whether the connection is alive in name (connected==true) but
// has received NO inbound traffic for at least threshold. Because lastMessageTime
// is bumped by the read loop on every inbound message (including PING), a true
// result means the read loop has genuinely gone quiet — this is what makes the
// watchdog safe and is the inverse of the #13 race. A zero lastMessageTime
// (never received anything yet) is treated as not-stale to avoid a spurious
// teardown immediately after connect, before the first message.
func (c *IRCClient) isStale(now time.Time, threshold time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.connected || c.lastMessageTime.IsZero() {
		return false
	}
	return now.Sub(c.lastMessageTime) >= threshold
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run TestIsStale -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/irc/client.go internal/irc/client_test.go
git commit -m "feat(irc): add guarded isStale liveness check keyed on lastMessageTime"
```

---

### Task 3: Run a watchdog goroutine that forces teardown when stale

**Files:**
- Modify: `internal/irc/client.go`

This is integration wiring around the unit-tested `isStale`; there is no pure unit to add here. Verification is by build + the manual repro in Phase 4.

- [ ] **Step 1: Add a stop channel field to IRCClient**

In the `IRCClient` struct (near `reconnecting bool`, ~line 77) add:

```go
	watchdogStop          chan struct{}         // closed to stop the liveness watchdog (guarded by mu)
```

- [ ] **Step 2: Add the watchdog method**

In `internal/irc/client.go`, add after `isStale`:

```go
// startWatchdog launches a goroutine that periodically checks for a silently
// dead socket (connected==true but no inbound traffic past the stale threshold)
// and forces a teardown via conn.Quit(). Quit() issues a socket write that, on a
// dead link, trips the read/write deadline (ConnectionReadTimeout) and makes the
// library run its DisconnectCallback — the single teardown path that emits
// EventConnectionLost and drives auto-reconnect. The lastMessageTime guard in
// isStale guarantees this never fires while traffic is flowing (#13 safe).
func (c *IRCClient) startWatchdog() {
	stop := make(chan struct{})
	c.mu.Lock()
	c.watchdogStop = stop
	c.mu.Unlock()

	go func() {
		ticker := time.NewTicker(constants.ConnectionWatchdogInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if c.isStale(time.Now(), constants.ConnectionStaleThreshold) {
					logger.Log.Warn().Int64("network_id", c.networkID).
						Msg("Watchdog: connection silent past threshold, forcing teardown")
					c.conn.Quit()
				}
			}
		}
	}()
}

// stopWatchdog signals the watchdog goroutine to exit. Safe to call multiple times.
func (c *IRCClient) stopWatchdog() {
	c.mu.Lock()
	if c.watchdogStop != nil {
		close(c.watchdogStop)
		c.watchdogStop = nil
	}
	c.mu.Unlock()
}
```

- [ ] **Step 3: Start the watchdog on connect, stop it on disconnect**

In the `AddConnectCallback` body ([client.go:849](../../internal/irc/client.go)), after `c.connected = true ... c.mu.Unlock()` (line 853), add:

```go
		c.startWatchdog()
```

In the `AddDisconnectCallback` body ([client.go:896](../../internal/irc/client.go)), as the first statements after `c.connected = false ... c.mu.Unlock()` (line 902), add:

```go
		c.stopWatchdog()
```

- [ ] **Step 4: Build to verify it compiles**

Run: `CGO_ENABLED=1 go build -tags fts5 ./...`
Expected: builds clean

- [ ] **Step 5: Run the full irc package tests**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/irc/client.go
git commit -m "feat(irc): watchdog forces teardown of silently dead sockets"
```

---

### Task 4: Probe all connections on system wake

**Files:**
- Modify: `app_connection.go`
- Modify: `app.go` (ServiceStartup, ~line 154-165)
- Modify: `internal/irc/client.go` (expose a force-check helper)

After sleep/wake the socket is usually dead but the watchdog tick may be up to 15s away and the library ping up to ~90s away. A wake hook makes detection immediate.

- [ ] **Step 1: Add a `CheckLivenessNow` helper on the client**

In `internal/irc/client.go`, add after `stopWatchdog`:

```go
// CheckLivenessNow forces an immediate staleness evaluation (used by the
// system-wake hook). If stale, it tears the connection down via the normal path.
// Returns true if a teardown was triggered.
func (c *IRCClient) CheckLivenessNow() bool {
	if c.isStale(time.Now(), constants.ConnectionStaleThreshold) {
		logger.Log.Warn().Int64("network_id", c.networkID).
			Msg("Wake probe: connection silent past threshold, forcing teardown")
		c.conn.Quit()
		return true
	}
	return false
}
```

- [ ] **Step 2: Add an App method that probes every client**

In `app_connection.go`, add near `GetConnectionStatus`:

```go
// probeAllConnections forces an immediate liveness check on every active client.
// Called on system wake, where sockets are commonly dead but undetected. The
// teardown (if any) flows through the normal DisconnectCallback → auto-reconnect
// path, so this method only nudges; it does not reconnect directly.
func (a *App) probeAllConnections() {
	a.mu.RLock()
	clients := make([]*irc.IRCClient, 0, len(a.ircClients))
	for _, c := range a.ircClients {
		clients = append(clients, c)
	}
	a.mu.RUnlock()

	for _, c := range clients {
		c.CheckLivenessNow()
	}
}
```

- [ ] **Step 3: Register the wake hook in ServiceStartup**

In `app.go`, inside `ServiceStartup` after the existing `a.app.Event.On(...)` registrations (~line 165), add:

```go
	// On system wake, sockets are usually dead but undetected; probe immediately
	// so the UI reflects reality without waiting for the watchdog/library ping.
	// This callback runs off the main thread (Wails app-event semantics); that is
	// fine here — probeAllConnections touches no AppKit/UI APIs.
	a.app.Event.OnApplicationEvent(events.Common.SystemDidWake, func(*application.ApplicationEvent) {
		logger.Log.Info().Msg("System woke; probing all IRC connections for liveness")
		a.probeAllConnections()
	})
```

Ensure `app.go` imports `"github.com/wailsapp/wails/v3/pkg/events"` (add it if absent; `application` and `logger` are already imported).

- [ ] **Step 4: Build to verify it compiles**

Run: `CGO_ENABLED=1 go build -tags fts5 ./...`
Expected: builds clean

- [ ] **Step 5: Commit**

```bash
git add internal/irc/client.go app_connection.go app.go
git commit -m "feat(app): probe connection liveness on system wake"
```

---

## Phase 2 — Frontend connection-status correctness

### Task 5: Make `setConnectionStatus` reject out-of-order events

**Files:**
- Modify: `frontend/src/stores/network.ts`
- Modify: `frontend/src/App.tsx`
- Test: `frontend/src/stores/__tests__/network.test.ts`

The backend already stamps each `connection-status` event with a `timestamp` ([app_events.go:29](../../app_events.go)). The store currently ignores it, so unordered goroutine delivery ([bus.go:91](../../internal/events/bus.go)) can apply an older event last. Track the last-applied time per network and drop stale event updates. The poll (Task 6) calls without a timestamp and is always authoritative.

- [ ] **Step 1: Write the failing test**

Add to `frontend/src/stores/__tests__/network.test.ts`:

```ts
describe('network store: connectionStatus ordering', () => {
  beforeEach(() => {
    useNetworkStore.setState({ connectionStatus: {}, connectionStatusAt: {} });
  });

  it('applies a newer event and ignores an older one (out-of-order delivery)', () => {
    const { setConnectionStatus } = useNetworkStore.getState();
    setConnectionStatus(1, true, 2000);   // newer: connected
    setConnectionStatus(1, false, 1000);  // older: must be ignored
    expect(useNetworkStore.getState().connectionStatus[1]).toBe(true);
  });

  it('an untimestamped update (poll) always wins', () => {
    const { setConnectionStatus } = useNetworkStore.getState();
    setConnectionStatus(1, true, 5000);
    setConnectionStatus(1, false);        // poll, no timestamp -> authoritative
    expect(useNetworkStore.getState().connectionStatus[1]).toBe(false);
  });

  it('is per-network', () => {
    const { setConnectionStatus } = useNetworkStore.getState();
    setConnectionStatus(1, true, 2000);
    setConnectionStatus(2, false, 1000);
    expect(useNetworkStore.getState().connectionStatus).toEqual({ 1: true, 2: false });
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/stores/__tests__/network.test.ts -t "connectionStatus ordering"`
Expected: FAIL — `connectionStatusAt` not in state / signature mismatch

- [ ] **Step 3: Add `connectionStatusAt` to state and update the type**

In `frontend/src/stores/network.ts`, in the state interface near `connectionStatus: Record<number, boolean>;` (line 122) add:

```ts
  connectionStatusAt: Record<number, number>; // last-applied event time (ms) per network
```

Change the action signature (line 194) from:

```ts
  setConnectionStatus: (networkId: number, connected: boolean) => void;
```

to:

```ts
  setConnectionStatus: (networkId: number, connected: boolean, at?: number) => void;
```

In the initial state near `connectionStatus: {},` (line 220) add:

```ts
  connectionStatusAt: {},
```

- [ ] **Step 4: Implement the ordered reducer**

Replace the `setConnectionStatus` implementation (lines 882-885):

```ts
  setConnectionStatus: (networkId, connected) =>
    set((state) => ({
      connectionStatus: { ...state.connectionStatus, [networkId]: connected },
    })),
```

with:

```ts
  setConnectionStatus: (networkId, connected, at) =>
    set((state) => {
      // Timestamped updates (events) may arrive out of order because the Go event
      // bus dispatches subscribers on unordered goroutines. Drop an event older
      // than the last one we applied for this network. Untimestamped updates (the
      // poll) are authoritative and always win.
      if (at !== undefined) {
        const lastAt = state.connectionStatusAt[networkId];
        if (lastAt !== undefined && at < lastAt) {
          return state;
        }
        return {
          connectionStatus: { ...state.connectionStatus, [networkId]: connected },
          connectionStatusAt: { ...state.connectionStatusAt, [networkId]: at },
        };
      }
      return {
        connectionStatus: { ...state.connectionStatus, [networkId]: connected },
      };
    }),
```

- [ ] **Step 5: Pass the event timestamp from App.tsx**

In `frontend/src/App.tsx`, in the `connection-status` handler (lines 258-264), replace:

```tsx
    const unsubscribe = EventsOn('connection-status', (data: any) => {
      const networkId = data?.networkId;
      const connected = data?.connected;
      if (networkId !== undefined && typeof connected === 'boolean') {
        setConnectionStatus(networkId, connected);
      }
    });
```

with:

```tsx
    const unsubscribe = EventsOn('connection-status', (data: any) => {
      const networkId = data?.networkId;
      const connected = data?.connected;
      const at = data?.timestamp ? Date.parse(data.timestamp) : undefined;
      if (networkId !== undefined && typeof connected === 'boolean') {
        setConnectionStatus(networkId, connected, Number.isNaN(at) ? undefined : at);
      }
    });
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd frontend && npx vitest run src/stores/__tests__/network.test.ts -t "connectionStatus ordering"`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add frontend/src/stores/network.ts frontend/src/App.tsx frontend/src/stores/__tests__/network.test.ts
git commit -m "fix(ui): apply connection-status events in timestamp order"
```

---

### Task 6: Poll connection status for ALL networks, not just the selected one

**Files:**
- Modify: `frontend/src/stores/network.ts`
- Modify: `frontend/src/App.tsx`
- Test: `frontend/src/stores/__tests__/network.test.ts`

The interval poll ([App.tsx:248](../../frontend/src/App.tsx)) only refreshes `selectedNetwork`, so non-selected sidebar dots ride on events alone. Add a store action that refreshes every known network and call it on the interval.

- [ ] **Step 1: Write the failing test**

Add to `frontend/src/stores/__tests__/network.test.ts`:

```ts
import { vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', async (orig) => {
  const actual = await (orig() as Promise<Record<string, unknown>>);
  return { ...actual, GetConnectionStatus: vi.fn() };
});

import { GetConnectionStatus } from '../../../wailsjs/go/main/App';

describe('network store: refreshAllConnectionStatus', () => {
  beforeEach(() => {
    useNetworkStore.setState({
      networks: [
        { id: 1, name: 'A' } as any,
        { id: 2, name: 'B' } as any,
      ],
      connectionStatus: {},
      connectionStatusAt: {},
    });
    (GetConnectionStatus as any).mockReset();
  });

  it('writes a status for every known network', async () => {
    (GetConnectionStatus as any).mockImplementation((id: number) =>
      Promise.resolve(id === 1),
    );
    await useNetworkStore.getState().refreshAllConnectionStatus();
    expect(useNetworkStore.getState().connectionStatus).toEqual({ 1: true, 2: false });
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/stores/__tests__/network.test.ts -t "refreshAllConnectionStatus"`
Expected: FAIL — `refreshAllConnectionStatus` not a function

- [ ] **Step 3: Declare the action in the interface**

In `frontend/src/stores/network.ts`, near `loadConnectionStatus` (line 163) add:

```ts
  refreshAllConnectionStatus: () => Promise<void>;
```

- [ ] **Step 4: Implement the action**

In `frontend/src/stores/network.ts`, add right after the `loadConnectionStatus` implementation (after line 523):

```ts
  refreshAllConnectionStatus: async () => {
    const { networks } = get();
    const results = await Promise.all(
      networks.map(async (n) => {
        try {
          return { id: n.id, connected: await GetConnectionStatus(n.id) };
        } catch {
          return { id: n.id, connected: false };
        }
      }),
    );
    set((state) => {
      const next = { ...state.connectionStatus };
      results.forEach(({ id, connected }) => {
        next[id] = connected;
      });
      return { connectionStatus: next };
    });
  },
```

- [ ] **Step 5: Call it on the interval**

In `frontend/src/App.tsx`, subscribe to the action with the other store selectors (near line 32):

```tsx
  const refreshAllConnectionStatus = useNetworkStore((s) => s.refreshAllConnectionStatus);
```

In the interval effect (lines 246-251), replace `loadConnectionStatus();` *inside the interval* with `refreshAllConnectionStatus();` so the poll covers every network:

```tsx
      const interval = setInterval(() => {
        loadMessages();
        refreshAllConnectionStatus();
        loadCurrentNick();
        loadChannelInfo();
      }, 2000);
```

(Leave the one-time `loadConnectionStatus();` at line 240 as-is for the immediate selected-network read on selection change.)

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd frontend && npx vitest run src/stores/__tests__/network.test.ts -t "refreshAllConnectionStatus"`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add frontend/src/stores/network.ts frontend/src/App.tsx frontend/src/stores/__tests__/network.test.ts
git commit -m "fix(ui): poll connection status for all networks so sidebar dots stay accurate"
```

---

## Phase 3 — Nick list refresh on reconnect

### Task 7: Repopulate the roster after reconnect (backend)

**Files:**
- Read first: `internal/irc/client.go` rejoin path (`rejoinChannels`/`channelsToJoin`, ~line 3534-3740) and the `NAMES`/`353`/`366` handlers.
- Modify: as determined by the read.
- Test: `internal/irc/client_test.go` if a pure seam exists.

The screenshot shows `USERS (0)` in an active channel after reconnect. On disconnect the roster is cleared ([client.go:963-974](../../internal/irc/client.go)); on reconnect we rejoin every open channel ([client.go:channelsToJoin reconnect=true](../../internal/irc/client.go)), which should make the server resend `NAMES`. Verify that the `353` handler writes channel users to storage on the reconnected client and that nothing short-circuits it when the channel row already exists.

- [ ] **Step 1: Trace and confirm the failure mode**

Read the `353` (`RPL_NAMREPLY`) and `366` (`RPL_ENDOFNAMES`) callbacks in `client.go`. Confirm whether, after `ClearChannelUsers` + rejoin, the names reply repopulates `channel_users`. Note findings in the commit message. If repopulation already works at the storage layer, the bug is purely the frontend not reloading (Task 8) — record that and skip to Task 8.

- [ ] **Step 2: If a backend gap is found, add a focused failing test**

If the trace shows a real backend gap (e.g. names reply ignored when not freshly joined), write a unit test against the smallest pure seam you can extract (e.g. a parser/host-extraction helper) following the `TestNoticePMTarget` table style in `internal/irc/client_test.go`. Run it, watch it fail, fix minimally, watch it pass.

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -count=1`

- [ ] **Step 3: Commit**

```bash
git add internal/irc/client.go internal/irc/client_test.go
git commit -m "fix(irc): repopulate channel roster on reconnect"
```

(If Step 1 found no backend gap, skip this task's commit and proceed to Task 8.)

---

### Task 8: Refresh the nick list when a (re)connect lands (frontend)

**Files:**
- Modify: `frontend/src/components/channel-info.tsx`

The panel only reloads on `message-event` ([channel-info.tsx:107](../../frontend/src/components/channel-info.tsx)). Add a `connection-status` subscription so a reconnect for the visible network reloads the roster.

- [ ] **Step 1: Add the subscription effect**

In `frontend/src/components/channel-info.tsx`, add a new `useEffect` alongside the existing `message-event` effect (after the block ending at line 212), using the same `networkId`/`channelName`/`loadChannelInfo` deps:

```tsx
  // A (re)connect clears and repopulates the server-side roster; reload so the
  // user list doesn't sit empty/stale after the socket comes back.
  useEffect(() => {
    const unsubscribe = EventsOn('connection-status', (data: any) => {
      if (data?.networkId === networkId && data?.connected === true) {
        loadChannelInfo();
      }
    });
    return () => unsubscribe();
  }, [networkId, channelName, loadChannelInfo]);
```

- [ ] **Step 2: Run the frontend tests + type check**

Run: `cd frontend && npx vitest run && npx tsc --noEmit`
Expected: PASS / no type errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/channel-info.tsx
git commit -m "fix(ui): reload channel roster when a network (re)connects"
```

---

## Phase 4 — Full verification

- [ ] **Step 1: Run all backend tests**

Run: `CGO_ENABLED=1 go test -tags fts5 ./... -count=1`
Expected: PASS

- [ ] **Step 2: Run all frontend tests + type check + lint**

Run: `cd frontend && npx vitest run && npx tsc --noEmit`
Then from repo root: `task check`
Expected: PASS

- [ ] **Step 3: Manual repro of the original symptom**

1. `task dev`, connect to a network, join a channel with users.
2. Simulate silent death: put the machine to sleep for ~1 min (or block the IRC port with a firewall rule / pull network) so no clean FIN is sent.
3. Wake / restore network.
4. Confirm within ~a few seconds: the header badge flips to Disconnected then back to Connected on auto-reconnect (not stuck green), sidebar dots for *all* networks track reality, and the nick list repopulates.

- [ ] **Step 4: Confirm #13 did not regress**

With an idle-but-healthy connection (no chatter for several minutes), confirm the badge stays Connected and the watchdog never tears it down (PING/PONG keeps `lastMessageTime` fresh, so `isStale` stays false).

---

## Self-review notes

- **Spec coverage:** stuck-green → Tasks 1-4; sidebar dots → Tasks 5-6; nick list → Tasks 7-8; #13 guard → `isStale` lastMessageTime keying + Phase 4 Step 4.
- **Type consistency:** `setConnectionStatus(networkId, connected, at?)`, `connectionStatusAt`, `refreshAllConnectionStatus`, `isStale(now, threshold)`, `startWatchdog`/`stopWatchdog`/`CheckLivenessNow`, `probeAllConnections` are used consistently across tasks.
- **Risk:** Task 7 is investigation-gated — it may collapse to "frontend-only" (Task 8) if the backend already repopulates. That is intentional; do the trace before writing code.

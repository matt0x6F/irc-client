# Bot Mode `+B` (User-Mode Half) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the persistent `+B` user-mode half of IRCv3 [bot mode](https://ircv3.net/specs/extensions/bot-mode) — parse the `BOT=` ISUPPORT token, recognize `+B` bots at rest via WHO flags, and let a connection announce itself as a bot via a durable per-network setting — flipping the ratified cap from ◐ Partial to ✅.

**Architecture:** Recognition reuses the existing `markBot` path (badge/event/store already built for the detection half). The advertised bot letter is parsed from `BOT=<letter>` into `ServerCapabilities.BotModeChar` and gates everything — recognition and self-announce both use the *server-advertised* letter, never a hardcoded `B`. Self-announce is a durable `networks.identify_as_bot` column applied at connect time (same hook as auto-join), folding the server's `+B` echo back through `markBot` rather than a separate self-mode tracker.

**Tech Stack:** Go (`internal/irc`, `internal/storage`, SQLite + sqlc), React/TypeScript + Wails bindings (`frontend/`).

## Global Constraints

- Go tests run with: `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1` (fts5 build tag required).
- After any schema/query change, regenerate sqlc: `task sqlc-generate`.
- After changing any Wails-bound method or model, regenerate bindings: `wails3 generate bindings -clean=true` (commit the regenerated `frontend/wailsjs/` / bindings; a benign `$$createType` renumber is expected).
- Use the **server-advertised** bot mode letter everywhere; never hardcode `'B'`. The conventional value is `B` but servers may advertise `b`.
- The `bot` message tag is valueless — key on presence, ignore any value (already honored by `maybeMarkBotFromTag`; do not regress).
- `ServerCapabilities` mutations are done under `c.mu.Lock()` per the existing pattern in `applyISUPPORTToken`.

---

### Task 1: Parse the `BOT=<letter>` ISUPPORT token

**Files:**
- Modify: `internal/irc/client.go` (struct `ServerCapabilities` ~line 88; `applyISUPPORTToken` ~line 4239; add `BotMode()` accessor near `ExtbanInfo` ~line 4299)
- Test: `internal/irc/isupport_test.go`

**Interfaces:**
- Produces: `ServerCapabilities.BotModeChar rune` (0 when unadvertised); `func (c *IRCClient) BotMode() string` returning the advertised letter as a string (`""` when unadvertised).

- [ ] **Step 1: Write the failing test**

Add to `internal/irc/isupport_test.go`:

```go
func TestApplyISUPPORTToken_BotMode(t *testing.T) {
	c := &IRCClient{serverCapabilities: &ServerCapabilities{}, network: &storage.Network{Name: "test"}}

	c.applyISUPPORTToken("BOT=B")
	if got := c.BotMode(); got != "B" {
		t.Fatalf("BotMode() = %q, want %q", got, "B")
	}
	if c.serverCapabilities.BotModeChar != 'B' {
		t.Fatalf("BotModeChar = %q, want 'B'", c.serverCapabilities.BotModeChar)
	}
}

func TestApplyISUPPORTToken_BotMode_LowercaseLetter(t *testing.T) {
	c := &IRCClient{serverCapabilities: &ServerCapabilities{}, network: &storage.Network{Name: "test"}}
	c.applyISUPPORTToken("BOT=b")
	if got := c.BotMode(); got != "b" {
		t.Fatalf("BotMode() = %q, want %q", got, "b")
	}
}

func TestBotMode_Unadvertised(t *testing.T) {
	c := &IRCClient{serverCapabilities: &ServerCapabilities{}, network: &storage.Network{Name: "test"}}
	if got := c.BotMode(); got != "" {
		t.Fatalf("BotMode() = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run TestApplyISUPPORTToken_BotMode -count=1`
Expected: FAIL — `c.BotMode undefined` and `BotModeChar` field missing.

- [ ] **Step 3: Add the struct field**

In `internal/irc/client.go`, add to `ServerCapabilities` (after `ExtbanTypes`, before `mu`):

```go
	BotModeChar  rune          // BOT=<letter> ISUPPORT: the user mode that marks a bot (e.g. 'B'); 0 if unadvertised
```

- [ ] **Step 4: Add the parse case**

In `applyISUPPORTToken`, add a case inside the `switch` (after the `EXTBAN=` case):

```go
	case strings.HasPrefix(param, "BOT="):
		// Ratified bot mode: BOT=<letter> advertises the user mode that marks a
		// client as a bot. We record the server's letter (conventionally 'B', but
		// servers may use 'b') so both recognition and self-announce use it verbatim.
		letters := []rune(param[len("BOT="):])
		if len(letters) == 1 {
			c.mu.Lock()
			c.serverCapabilities.BotModeChar = letters[0]
			c.mu.Unlock()
		}
```

- [ ] **Step 5: Add the accessor**

In `internal/irc/client.go`, after `ExtbanInfo` (~line 4309):

```go
// BotMode returns the server-advertised bot user-mode letter (from the BOT=
// ISUPPORT token) as a string, or "" when the server did not advertise one.
// Self-announce and WHO-flag recognition both key off this letter.
func (c *IRCClient) BotMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.serverCapabilities == nil || c.serverCapabilities.BotModeChar == 0 {
		return ""
	}
	return string(c.serverCapabilities.BotModeChar)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run 'TestApplyISUPPORTToken_BotMode|TestBotMode' -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/irc/client.go internal/irc/isupport_test.go
git commit -m "feat(irc): parse BOT= ISUPPORT token (bot mode letter)"
```

---

### Task 2: Recognize `+B` bots via WHO/WHOX flags

**Files:**
- Modify: `internal/irc/client.go` (`applyWhoxRow` ~line 3138)
- Test: `internal/irc/whox_test.go`

**Interfaces:**
- Consumes: `ServerCapabilities.BotModeChar` (Task 1); existing `markBot(nick string)` (client.go:713).
- Produces: a WHOX row whose flags field contains the advertised bot letter marks that nick via `markBot`.

- [ ] **Step 1: Write the failing test**

Add to `internal/irc/whox_test.go` (match the existing harness in that file for constructing an `IRCClient` with a bot-detection sink; mirror an existing `applyWhoxRow` test's setup):

```go
func TestApplyWhoxRow_BotFlagMarksBot(t *testing.T) {
	c := newTestClientForWhox(t) // existing helper used by other applyWhoxRow tests
	c.serverCapabilities.BotModeChar = 'B'

	// WHOX %tcuhnfar: token, channel, user, host, nick, flags, account, realname.
	// flags "H*B" = here, oper, bot.
	c.applyWhoxRow([]string{"42", "#chan", "~u", "host", "robodan", "H*B", "0", "Robo Dan"})

	if !c.isBot("robodan") {
		t.Fatalf("expected robodan to be marked as a bot from WHOX B flag")
	}
}

func TestApplyWhoxRow_NoBotFlag(t *testing.T) {
	c := newTestClientForWhox(t)
	c.serverCapabilities.BotModeChar = 'B'
	c.applyWhoxRow([]string{"42", "#chan", "~u", "host", "alice", "H", "0", "Alice"})
	if c.isBot("alice") {
		t.Fatalf("alice has no B flag and must not be marked a bot")
	}
}
```

> Note: if no `isBot`/`newTestClientForWhox` helper exists, reuse the assertion style already present in `whox_test.go` (e.g. inspect the bot set directly or the same accessor the bot-detection tests in `client_test.go` use). Do not invent a new public API beyond what the detection half already exposes.

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run TestApplyWhoxRow_BotFlag -count=1`
Expected: FAIL — bot not marked.

- [ ] **Step 3: Implement the flag scan**

In `applyWhoxRow`, after the existing `away := len(flags) > 0 && flags[0] == 'G'` line, add:

```go
	// Bot mode: the BOT= letter appears within the WHO flags field (after the
	// here/gone marker and any '*' oper flag), so scan the whole field — not just
	// index 0. Recognizes a bot the instant we WHOX a channel, before it speaks.
	c.mu.RLock()
	botChar := c.serverCapabilities.BotModeChar
	c.mu.RUnlock()
	if botChar != 0 && strings.ContainsRune(flags, botChar) {
		c.markBot(nick)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run TestApplyWhoxRow -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/irc/client.go internal/irc/whox_test.go
git commit -m "feat(irc): recognize +B bots from WHOX flags"
```

---

### Task 3: Mark self as bot from the `+B` MODE echo

**Files:**
- Modify: `internal/irc/client.go` (MODE callback ~line 2036; add helper `markSelfBotFromUserMode`)
- Test: `internal/irc/client_test.go` (or a new `bot_mode_test.go` alongside it)

**Interfaces:**
- Consumes: `ServerCapabilities.BotModeChar` (Task 1); `CurrentNick()` (client.go:255); `markBot` (client.go:713).
- Produces: a user-targeted `MODE <self> +<letter>` echo marks our own nick via `markBot`.

- [ ] **Step 1: Write the failing test**

Add to `internal/irc/client_test.go`:

```go
func TestUserModeEcho_SelfBotMarksSelf(t *testing.T) {
	c := newTestClient(t) // existing helper; must have a CurrentNick and serverCapabilities
	c.setCurrentNick("robodan")          // use the existing nick-setting helper/path
	c.serverCapabilities.BotModeChar = 'B'

	// Simulate the server echo ":robodan MODE robodan :+B" reaching the MODE callback.
	c.handleSelfUserModeForTest("robodan", "+B") // thin test entrypoint calling the same branch

	if !c.isBot("robodan") {
		t.Fatalf("expected self (robodan) to be marked a bot after +B echo")
	}
}
```

> Note: match the actual helpers in `client_test.go` for building a client and setting the current nick. If the MODE callback is only reachable via the raw-line path, drive it with the existing message-injection helper used by other MODE tests instead of adding `handleSelfUserModeForTest`. The behavioral assertion (self gets marked) is what matters.

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run TestUserModeEcho_SelfBot -count=1`
Expected: FAIL — self not marked.

- [ ] **Step 3: Add the self-user-mode branch**

In the MODE callback (`internal/irc/client.go` ~line 2036), the early-return currently drops *all* non-channel targets:

```go
		// Only handle channel modes (target starts with # or &); ignore user modes.
		if len(target) == 0 || (target[0] != '#' && target[0] != '&') {
			return
		}
```

Replace that block with:

```go
		// Channel modes start with # or &. A non-channel target is a user mode;
		// we only care about our own +<botletter> echo (so our own nick carries the
		// bot badge consistently), then return — other user modes are still ignored.
		if len(target) == 0 || (target[0] != '#' && target[0] != '&') {
			if len(e.Params) >= 2 {
				c.markSelfBotFromUserMode(target, e.Params[1])
			}
			return
		}
```

Then add the helper (near `markBot`, ~line 713):

```go
// markSelfBotFromUserMode handles a user-mode MODE line targeting ourselves. If
// it ADDS the advertised bot letter to our own nick, we mark ourselves via the
// shared markBot path so the self nick shows the bot badge. All other user-mode
// changes are intentionally ignored (we track no general self-usermode state).
func (c *IRCClient) markSelfBotFromUserMode(target, modeStr string) {
	if !strings.EqualFold(target, c.CurrentNick()) {
		return
	}
	botChar := c.serverCapabilities.BotModeChar // read under lock if BotModeChar can race; see Task 1 pattern
	if botChar == 0 {
		return
	}
	adding := true
	for _, r := range modeStr {
		switch r {
		case '+':
			adding = true
		case '-':
			adding = false
		case botChar:
			if adding {
				c.markBot(c.CurrentNick())
			}
		}
	}
}
```

> Read `botChar` under `c.mu.RLock()` if the linter/race detector flags it (the field is written under lock in Task 1). Match whatever locking the surrounding MODE callback already uses.

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run 'TestUserModeEcho|TestMode' -count=1`
Expected: PASS (and existing channel-MODE tests still pass).

- [ ] **Step 5: Commit**

```bash
git add internal/irc/client.go internal/irc/client_test.go
git commit -m "feat(irc): mark self as bot from +B user-mode echo"
```

---

### Task 4: Durable `identify_as_bot` per-network config

**Files:**
- Modify: `internal/storage/schema.sql` (networks table ~lines 4-22)
- Modify: `internal/storage/migrations.go` (add `migrateIdentifyAsBot`, call it from `Migrate`)
- Modify: `internal/storage/queries/networks.sql` (`CreateNetwork`, `UpdateNetwork`)
- Modify: `internal/storage/models.go` (`Network` struct ~line 6)
- Modify: `internal/storage/convert.go` (`convertNetworkFromDB`, `convertNetworkToDBCreateParams`, `convertNetworkToDBUpdateParams`)
- Regenerate: `internal/storage/generated/` (via `task sqlc-generate`)
- Test: `internal/storage/database_test.go`

**Interfaces:**
- Produces: `Network.IdentifyAsBot bool` (`db:"identify_as_bot" json:"identify_as_bot"`), round-tripped through create/update/get.

- [ ] **Step 1: Write the failing test**

Add to `internal/storage/database_test.go` (match the existing network create/get test style in that file):

```go
func TestNetwork_IdentifyAsBot_RoundTrips(t *testing.T) {
	s := newTestStorage(t) // existing helper
	n := &Network{Name: "libera", Address: "irc.libera.chat", Port: 6697, TLS: true,
		Nickname: "robodan", Username: "robodan", Realname: "Robo Dan", IdentifyAsBot: true}
	created, err := s.CreateNetwork(n)
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	got, err := s.GetNetwork(created.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if !got.IdentifyAsBot {
		t.Fatalf("IdentifyAsBot did not persist: got false, want true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/storage/ -run TestNetwork_IdentifyAsBot -count=1`
Expected: FAIL — `Network` has no `IdentifyAsBot` field.

- [ ] **Step 3: Add the schema column**

In `internal/storage/schema.sql`, add to the `networks` table (after `auto_connect`):

```sql
    identify_as_bot BOOLEAN NOT NULL DEFAULT 0,
```

- [ ] **Step 4: Add the migration**

In `internal/storage/migrations.go`, add after the `migrateAutoConnect` call in `Migrate`:

```go
	// Handle identify_as_bot field migration (bot mode +B self-announce)
	if err := migrateIdentifyAsBot(db); err != nil {
		return fmt.Errorf("identify_as_bot migration failed: %w", err)
	}
```

And add the function (copy `migrateAutoConnect` verbatim, swapping the column name):

```go
func migrateIdentifyAsBot(db *sqlx.DB) error {
	var columnExists int
	err := db.Get(&columnExists,
		"SELECT COUNT(*) FROM pragma_table_info('networks') WHERE name='identify_as_bot'")
	if err != nil {
		return fmt.Errorf("failed to check for identify_as_bot column: %w", err)
	}
	if columnExists == 0 {
		if _, err := db.Exec("ALTER TABLE networks ADD COLUMN identify_as_bot BOOLEAN NOT NULL DEFAULT 0"); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add identify_as_bot column: %w", err)
			}
		}
	}
	return nil
}
```

- [ ] **Step 5: Update the queries**

In `internal/storage/queries/networks.sql`:

`CreateNetwork` — add `identify_as_bot` to the column list (after `auto_connect`) and one more `?` to the VALUES list:

```sql
-- name: CreateNetwork :one
INSERT INTO networks (name, address, port, tls, nickname, username, realname, password, sasl_enabled, sasl_mechanism, sasl_username, sasl_password, sasl_external_cert, auto_connect, identify_as_bot, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;
```

`UpdateNetwork` — add `identify_as_bot = ?` to the SET list (after `auto_connect = ?`):

```sql
-- name: UpdateNetwork :exec
UPDATE networks 
SET name = ?, address = ?, port = ?, tls = ?, 
    nickname = ?, username = ?, realname = ?, 
    password = ?, sasl_enabled = ?, sasl_mechanism = ?,
    sasl_username = ?, sasl_password = ?, sasl_external_cert = ?,
    auto_connect = ?, identify_as_bot = ?, updated_at = ?
WHERE id = ?;
```

- [ ] **Step 6: Regenerate sqlc**

Run: `task sqlc-generate`
Expected: `internal/storage/generated/` now has `IdentifyAsBot` on `db.Network`, `CreateNetworkParams`, `UpdateNetworkParams`.

- [ ] **Step 7: Add the model field**

In `internal/storage/models.go`, add to `Network` (after `AutoConnect`, line 21):

```go
	IdentifyAsBot    bool      `db:"identify_as_bot" json:"identify_as_bot"`
```

- [ ] **Step 8: Wire the conversions**

In `internal/storage/convert.go`, add `IdentifyAsBot` alongside each `AutoConnect` mapping in all three functions:

- `convertNetworkFromDB`: `IdentifyAsBot: n.IdentifyAsBot,`
- `convertNetworkToDBCreateParams`: `IdentifyAsBot: n.IdentifyAsBot,`
- `convertNetworkToDBUpdateParams`: `IdentifyAsBot: n.IdentifyAsBot,`

- [ ] **Step 9: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/storage/ -run TestNetwork_IdentifyAsBot -count=1`
Expected: PASS

- [ ] **Step 10: Run the full storage suite (catch generated-code drift)**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/storage/ -count=1`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add internal/storage/
git commit -m "feat(storage): add durable identify_as_bot network setting"
```

---

### Task 5: Announce `+B` at connect time

**Files:**
- Modify: `internal/irc/client.go` (`doAutoJoin` ~line 3884; add `announceBotMode` helper)
- Test: `internal/irc/client_test.go` (or `bot_mode_test.go`)

**Interfaces:**
- Consumes: `BotMode()` (Task 1); `c.network.IdentifyAsBot` (Task 4); `CurrentNick()`; the existing status-line writer (`writeStatusLine`, used by `handleStandardReply`).
- Produces: on connect, when `IdentifyAsBot` is set, either sends `MODE <nick> +<letter>` (server supports it) or writes one status line (it doesn't).

- [ ] **Step 1: Write the failing test**

Add to `internal/irc/client_test.go`. Use the existing fake-connection/spy pattern other command-sending tests use (e.g. whatever captures `conn` sends):

```go
func TestAnnounceBotMode_SendsModeWhenSupportedAndEnabled(t *testing.T) {
	c := newTestClient(t)
	c.setCurrentNick("robodan")
	c.network.IdentifyAsBot = true
	c.serverCapabilities.BotModeChar = 'B'

	c.announceBotMode()

	// Assert a "MODE robodan +B" was sent via the spy/fake conn used by sibling tests.
	assertSent(t, c, "MODE robodan +B")
}

func TestAnnounceBotMode_NoopWhenDisabled(t *testing.T) {
	c := newTestClient(t)
	c.setCurrentNick("robodan")
	c.network.IdentifyAsBot = false
	c.serverCapabilities.BotModeChar = 'B'
	c.announceBotMode()
	assertNotSent(t, c, "MODE")
}

func TestAnnounceBotMode_StatusLineWhenUnsupported(t *testing.T) {
	c := newTestClient(t)
	c.setCurrentNick("robodan")
	c.network.IdentifyAsBot = true
	c.serverCapabilities.BotModeChar = 0 // server did not advertise BOT=
	c.announceBotMode()
	assertNotSent(t, c, "MODE")
	// And a status line was written — assert via the same path handleStandardReply tests use.
}
```

> Note: use the project's existing send-capture and status-line assertion helpers; match `standard_replies_test.go` for the status-line assertion and an existing MODE/command test for the send assertion. Do not introduce new test infrastructure if equivalents exist.

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run TestAnnounceBotMode -count=1`
Expected: FAIL — `announceBotMode` undefined.

- [ ] **Step 3: Implement the helper**

Add near `doAutoJoin` in `internal/irc/client.go`:

```go
// announceBotMode marks this connection as a bot when the network is configured
// to (IdentifyAsBot) and the server advertised a BOT= letter. It is called once
// per connection from doAutoJoin, alongside auto-join and the MONITOR re-arm, so
// it re-asserts on every (re)connect. When the setting is on but the server has
// no bot mode, it writes a single status line instead of failing silently.
func (c *IRCClient) announceBotMode() {
	if !c.network.IdentifyAsBot {
		return
	}
	letter := c.BotMode()
	if letter == "" {
		c.writeStatusLine("warning", "Server does not support bot mode; +B not set")
		return
	}
	nick := c.CurrentNick()
	if err := c.conn.Send(nil, "", "MODE", nick, "+"+letter); err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to send bot mode (+B)")
	}
}
```

> Match the actual send API the codebase uses for raw commands (e.g. `c.conn.Send(...)` / `SendRaw` / the helper used elsewhere to send `MODE`). Match the exact `writeStatusLine` signature used by `handleStandardReply` (arg order/severity strings).

- [ ] **Step 4: Call it from `doAutoJoin`**

In `doAutoJoin`, after `c.sendInitialMonitor()` (~line 3891):

```go
	// Announce ourselves as a bot if configured (gated on server BOT= support).
	c.announceBotMode()
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -run 'TestAnnounceBotMode|TestDoAutoJoin' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/irc/client.go internal/irc/client_test.go
git commit -m "feat(irc): announce +B at connect when identify_as_bot is set"
```

---

### Task 6: Settings UI + Wails bindings

**Files:**
- Modify: `frontend/src/components/settings-panel.tsx` (network form — the SASL/auto-connect section)
- Regenerate: Wails bindings (`frontend/wailsjs/` etc.)
- Test: extend an existing settings-panel vitest if one covers the network form; otherwise a focused render test asserting the checkbox binds to `identify_as_bot`.

**Interfaces:**
- Consumes: `Network.identify_as_bot` (Task 4, exposed through regenerated bindings).

- [ ] **Step 1: Regenerate bindings (model now carries the field)**

Run: `wails3 generate bindings -clean=true`
Expected: the generated `Network` model gains `identify_as_bot: boolean`.

- [ ] **Step 2: Add the checkbox to the network form**

In `settings-panel.tsx`, near the `auto_connect` / SASL controls, add a labeled checkbox bound to the network's `identify_as_bot` field (follow the exact controlled-input pattern the adjacent `auto_connect` checkbox uses — same state setter, same persistence path on save):

```tsx
<label className="flex items-center gap-2">
  <Checkbox
    checked={!!network.identify_as_bot}
    onCheckedChange={(v) => setNetwork({ ...network, identify_as_bot: !!v })}
  />
  <span>Identify as a bot (+B)</span>
</label>
<p className="text-xs text-muted-foreground">
  Marks this connection as an automated bot when the server supports IRCv3 bot mode.
</p>
```

> Match the actual component imports (`Checkbox` from `components/ui/`), the `network`/`setNetwork` names, and the save handler already used by the surrounding fields.

- [ ] **Step 3: Run the frontend tests + type-check**

Run: `cd frontend && npx vitest run && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/ 
git commit -m "feat(ui): add 'Identify as a bot (+B)' network setting"
```

---

### Task 7: Documentation — flip the cap to ✅

**Files:**
- Modify: `docs/public/developers/ircv3-support.md` (Bot mode row in the matrix → ✅; rewrite the Bot mode section's "Partial" paragraph; update "Not yet supported")
- Modify: `docs/public/developers/ircv3-roadmap.md` (tick Bot mode; update the ratified-gaps table + "Suggested sequencing")

**Interfaces:** none (docs).

- [ ] **Step 1: Update the support matrix**

In `ircv3-support.md`, change the `Bot mode` row status from `◐` to `✅` and the Notes to reflect that the `+B` user-mode half (BOT= token parsing, WHO-flag recognition, durable self-announce) is now implemented. Replace the "**Partial — the `+B` user-mode half is not implemented.**" paragraph in the Bot mode section with a description of the new behavior (token → `ServerCapabilities.BotModeChar`; WHO `B` flag → `markBot`; `identify_as_bot` network setting → `MODE +<letter>` at connect; self-echo → `markBot(self)`). Update the **Not yet supported** section so Bot mode is no longer listed as the ratified gap — state the ratified set is now complete.

- [ ] **Step 2: Update the roadmap**

In `ircv3-roadmap.md`, change the Bot mode row in the ratified-gaps table to ✅ (done), check its `### Detail` box, and update "Suggested sequencing" so item 1 (Bot mode) is struck/marked done — the ratified set is now fully complete.

- [ ] **Step 3: Verify the requestedCaps note is still accurate**

Bot mode needs no `requestedCaps` entry (it rides on `message-tags` + ISUPPORT, like MONITOR/WHOX). Confirm the support doc does not imply otherwise.

- [ ] **Step 4: Commit**

```bash
git add docs/public/developers/ircv3-support.md docs/public/developers/ircv3-roadmap.md
git commit -m "docs(ircv3): mark bot mode +B complete"
```

---

## Final verification

- [ ] Run all Go checks: `task check`
- [ ] Run the full Go test suite: `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1`
- [ ] Run frontend tests: `cd frontend && npx vitest run`
- [ ] Manual smoke (optional, against Ergo/Libera): enable "Identify as a bot", connect, confirm `MODE <nick> +B` is sent and own nick shows the bot badge; join a channel with a `+B` bot and confirm it's badged at rest.

## Self-Review Notes

- **Spec coverage:** BOT= token (T1), set-self via advertised letter (T4/T5), WHO-flag recognition (T2), WHOIS 335 + bot tag (pre-existing detection half, unchanged), advertised-letter-not-hardcoded (T1/T2/T3/T5), valueless tag ignored (pre-existing) — all covered.
- **Type consistency:** `BotModeChar rune`, `BotMode() string`, `IdentifyAsBot bool`, `announceBotMode()`, `markSelfBotFromUserMode(target, modeStr string)` used consistently across tasks.
- **Out of scope (YAGNI):** general self-usermode tracking, runtime toggle command, live `-B` un-set, persisting bot recognition of others.

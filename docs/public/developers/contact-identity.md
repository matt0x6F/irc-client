# Contact identity and reconnect recovery

Status: implemented and validated locally on 2026-10-04. The implementation uses
standard server-reported account metadata and contains no network-name special cases.

## Behavior

A saved contact should survive nickname changes while Cascade is closed. When
Cascade reconnects and observes the same authenticated account under a new nick,
the existing DM should show that nick and its current presence while retaining
its history, unread count, reply context, and read position. Receiving the rename
event is not a prerequisite when fresh account evidence is available.

This behavior depends on the identity and visibility the server provides. It
must not require a hardcoded network name or assume a continuously running client.

## Identity model

Keep these concepts separate:

| Concept | Lifetime and purpose |
| --- | --- |
| Local contact ID | Stable within Cascade; independent of nickname and connection lifetime. |
| Conversation ID | Stable owner of PM history and UI state; associated with the contact. |
| Network/account binding | Persisted association supported by server-reported authenticated account evidence, with its source and observation time. |
| Nickname alias | Historical display/routing name; an alias alone does not prove identity. |
| Current session | Connection-scoped, verified nick/account association used for presence and routing. A contact can have multiple sessions. |

Network scope is required: identical account names on different networks do not
identify the same contact automatically. Account bindings identify accounts; they
do not establish a person's identity across networks or across account changes.

Use the negotiated `CASEMAPPING` for nicknames. Account normalization must follow
the identifier's documented rules rather than automatically inheriting nick rules.

The existing `private_message_conversations.id` now serves as both the local contact
and conversation ID. `messages.conversation_id` owns PM history. The persisted
`target_user` is a mutable preferred destination; `pm_target` and message senders
retain the nickname used at receipt. Distinct contacts can have used the same nick
without merging their histories. Bindings are unique within a network and compare
account names exactly as reported by the server; account names do not inherit IRC
nickname case folding.

Migration preserves conversation IDs, open state, history, and nickname buddy
watches. Legacy PM history without a conversation row gets a closed contact.
Bindings start unresolved and are learned from fresh server evidence.

## Detecting support

Discover the available mechanisms for each connection, and consume verified
server replies through one identity reconciler:

| Signal | Use |
| --- | --- |
| Negotiated `account-tag` | Associate the sender of a live message with its authenticated account. |
| Negotiated `extended-join` | Associate joining users with their accounts. |
| Negotiated `account-notify` | Update session bindings when users log in, log out, or switch accounts. |
| `WHOX` in `005 ISUPPORT` | Seed nick/account associations from fresh permitted roster queries after joining/reconnecting. |
| Server-provided WHOIS account reply | Supplement the identity of a known current nick where supported; this does not find an unknown replacement nick. |

All these sources feed `internal/irc/contacts.go`. Roster observations can bind an
existing saved contact but do not create chats for unrelated channel members.
Messages and explicit opens can create contacts. Capabilities being advertised do
not prove that a particular user is identified or visible. Disconnect, QUIT,
logout, account replacement, and withdrawn capabilities remove current evidence
without discarding saved identities. Unrelated roster updates cannot restore it.

The standard mechanisms are documented in [capability negotiation](https://ircv3.net/specs/extensions/capability-negotiation),
[account-tag](https://ircv3.net/specs/extensions/account-tag),
[extended-join](https://ircv3.net/specs/extensions/extended-join),
[account-notify](https://ircv3.net/specs/extensions/account-notify), and
[WHOX](https://ircv3.net/specs/extensions/whox).

## Presence and routing

Recognizing an account and locating its current sessions are separate operations.
[MONITOR](https://ircv3.net/specs/extensions/monitor) subscribes to nicknames; it
does not provide a general account directory. Rebuild current session associations
from fresh data before deriving contact presence or selecting a send target.

- **Online:** a current session is verified as belonging to the saved contact.
- **Unknown:** the client is disconnected, identity is unresolved, or discovery
  cannot establish the account's current presence. An old nick's absence alone
  is insufficient to declare an account-bound contact offline.
- **Offline:** use only when the available evidence actually covers the contact's
  presence. A complete account lookup would require a documented provider contract.

Keep an explicit nickname watch's semantics distinct from contact presence:
MONITOR can authoritatively report that a particular nick is absent. Existing
saved buddy nicks should retain that meaning during migration; account following
must be an explicit contact association rather than a guessed conversion.

An observed `NICK` remains useful continuity evidence for the current session,
including an unidentified user. Without account evidence, that continuity cannot
recover a rename missed while disconnected. Hostnames, idents, IPs, and realnames
must not automatically link saved contacts across disconnected sessions.

If fresh facts identify a different account at a saved nickname, do not treat it
as the original contact or automatically route that contact's reply there. Keep
the original history attached to its contact and make selecting the new recipient
an explicit user action. If several matching sessions exist, retain the last
verified preferred target when available and expose other current sessions for
selection instead of repeatedly switching to whichever roster row arrived last.

These extensions expose account names without promising immutable lifetime IDs.
Account renames need stronger evidence to rebind; reuse of the identical account
name cannot be distinguished using standard account metadata alone. Add provider
adapters only for documented stronger identity or discovery mechanisms, keeping
their guarantees explicit.

## Storage and frontend integration

`GetPrivateContacts` returns saved identities with computed presence, target, and
current sessions. `OpenPrivateContact` returns the same snapshot. Local references
use `@<id>` and pane keys use `pm:@<id>`; these references are resolved before any
IRC request and are never sent as wire nicknames. Selection, history, unread state,
reply context, typing, search navigation, activity items, and notification replies
use the stable ID. Current nickname labels refresh without resetting read position.

The header exposes a session selector when an account has multiple visible nicks.
An unresolved account-bound send fails before reaching IRC and leaves the draft
in the composer. Recipient actions in its context menu are disabled; closing the
conversation remains available. Explicit raw-nickname commands keep ordinary IRC
addressing semantics.

Historical account tags can establish ownership of a replayed message when
`account-tag` is negotiated, but never establish current presence. Account-bound
remote history without trustworthy peer account metadata is skipped; existing
local history remains available. Outgoing replay tags identify the sender rather
than the peer and therefore do not prove historical recipient ownership. Message
deduplication uses conversation ID so a replay across nick aliases stays singular.

## Acceptance evidence

Behavioral tests cover these cases:

| Scenario | Expected result |
| --- | --- |
| Rename while Cascade is connected | One contact/conversation; current nick and routing update; history and read position remain stable. |
| Rename while Cascade is closed, then reconnect | Fresh evidence for the saved account resolves the same contact under the new nick without replaying `NICK`. |
| Same nick becomes occupied by a different account | No identity or history merge; no automatic reply to the replacement user. |
| Destination nick already has saved history for another contact | Preserve both histories under their original conversation IDs. |
| Same account has multiple current nicks | One account-bound contact with multiple verified sessions; stable recipient selection. |
| User logs out or switches accounts | Current account association is withdrawn or replaced; the old contact's binding/history is preserved. |
| No account information, or contact is hidden from discovery | No guessed cross-session linkage; unresolved contact presence is unknown. |
| Standard caps/WHOX vary or disappear | Use only the evidence actually available; retain local contact IDs across capability changes. |
| Same account text on two networks | No automatic cross-network association. |
| Existing database is migrated | Preserve history, open conversations, and explicit nickname watches; learn bindings only from fresh server evidence. |

Validation on 2026-10-04:

- `go test -tags fts5 ./...` passes, including migration, identity provenance,
  capability loss, logout/account replacement, network scope, alias reuse,
  session preference, historical ownership, and protected outbound routing.
- All 531 frontend tests pass. SQLC validation and production frontend build pass.
- Focused identity/storage tests also pass with Go's race detector enabled.
- Eight focused Playwright tests pass against real IRC and the Wails server-mode
  backend. Ergo 2.19.1 covers observed renames, a fully stopped backend during a
  rename, nickname reuse by a different account, and two current sessions for one
  account. InspIRCd 4 without services covers observed unidentified renames and
  refusing to guess continuity after a missed rename. This does not establish
  account recovery on an InspIRCd deployment with services.
- Native macOS Wails acceptance uses a separate temporary app bundle, data dir,
  and Ergo instance. Only the application name and single-instance identifier are
  changed through a build overlay to avoid the installed app. Conversation ID 1
  binds to account `nativeautumn` via WHOX, keeps its saved history after the app
  exits during `nativeautumn` → `nativeGhost`, restores the current header/sidebar
  and online dot after restarting, and delivers a native-composer reply to
  `nativeGhost`. Database inspection confirms both messages retain ID 1 and their
  original receipt-time routing names. The installed app and user database are
  untouched.

No general account directory, cross-network person identity, account-rename
adapter, or account-name reuse protection is claimed. Discovery remains bounded
by the current connection's visibility and server contract.

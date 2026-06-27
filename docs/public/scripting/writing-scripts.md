# Writing scripts

This page covers every concept you need to author scripts for Cascade: what a script is on disk, the manifest header, the handler functions, how to read messages and reply to them, how to send messages proactively with timers, the sandbox rules, and how to get editor autocomplete working.

---

## A script is a directory

A script is a **folder** inside the Cascade scripts directory. The folder name is the script's ID — what Cascade uses to track, enable, and reload it. Inside that folder you place one or more `.go` source files, all declaring `package main`.

```
~/.cascade-chat/scripts/
└── greeter/
    ├── greeter.go     ← handler functions
    └── helpers.go     ← optional extra file
```

When Cascade loads a script, it merges all `.go` files in the folder into a single compilation unit. Every file must declare `package main`. Subfolders are ignored.

---

## The manifest header

Cascade reads `// cascade:` comment lines from any file in the script directory. These lines form the script's manifest. The recognized keys are:

| Key | Purpose |
|---|---|
| `name` | Display name shown in the Scripts panel. Defaults to the folder name. |
| `description` | Free-text description shown in the panel. |
| `permissions` | Comma-separated capability labels (e.g. `network`). |

Example:

```go
package main

// cascade:name My Greeter
// cascade:description Greets people who say !hi
// cascade:permissions network

import "github.com/matt0x6f/irc-client/cascade"
```

!!! note "Permissions in v1"
    Permissions are parsed and displayed in the Scripts panel, but they are **not enforced in v1**. Declare them now for forward-compatibility — enforcement will be added in a future release.

---

## Handlers

A script works by exporting functions with specific names and signatures. Cascade calls them when the corresponding event occurs. All are optional — define only the ones your script needs.

| Handler | When it fires |
|---|---|
| `func Setup(c *cascade.Client)` | Once when the script loads (and on every reload or enable). Use it for timers and any proactive setup. |
| `func OnText(e cascade.TextEvent)` | An inbound channel or DM message (PRIVMSG). |
| `func OnNotice(e cascade.NoticeEvent)` | An inbound NOTICE. |
| `func OnJoin(e cascade.JoinEvent)` | Someone joined a channel you are in. |
| `func OnPart(e cascade.PartEvent)` | Someone left a channel you are in. |

Cascade **never** delivers your own messages back to you — `OnText` and `OnNotice` only fire for messages from other users.

---

## Reading and replying to messages

Both `TextEvent` and `NoticeEvent` carry the same core fields and share two methods:

| Field / method | What it gives you |
|---|---|
| `e.Nick` | The sender's nick |
| `e.Channel` | The channel name (or your own nick if it's a DM) |
| `e.Message` | The full message text |
| `e.IsDM()` | `true` if the message was sent directly to you, not a channel |
| `e.Reply(text)` | Send `text` back to the channel, or to the sender if it's a DM |

`TextEvent` also provides two convenience helpers that `NoticeEvent` does **not** have:

| Method | What it gives you |
|---|---|
| `e.HasPrefix("!cmd")` | `true` if the message starts with `!cmd` (case-sensitive) |
| `e.Arg(n)` | The nth whitespace-separated word of the message (1-based). Returns `""` if out of range. |

Calling `e.HasPrefix(...)` or `e.Arg(...)` inside an `OnNotice` handler will produce a load-time error. If you need prefix matching in a notice handler, compare `e.Message` directly using string operations — for example `len(e.Message) >= 5 && e.Message[:5] == "!cmd "` — since there is no `strings` package in the sandbox. The length guard is important: a slice expression like `e.Message[:5]` panics if the message is shorter than 5 characters, which would strike the script toward `runaway`.

`e.Arg(1)` is the first word of the message — typically the command name. Here is a complete script that uses it:

```go
package main

// cascade:name echo

import "github.com/matt0x6f/irc-client/cascade"

func OnText(e cascade.TextEvent) {
	if e.Arg(1) == "!echo" {
		e.Reply(e.Message)
	}
}
```

---

## Acting on your own — `Setup`, timers, and `Say`

`Setup` receives a `*cascade.Client` that you can use to schedule work and send messages independently of incoming events.

**Sending a message:**

```go
c.Network("Libera").Say("#team", "hello from a script")
```

`c.Network("<name>")` returns a handle to the named network. `.Say(target, message)` sends a PRIVMSG to `target` (a channel or nick).

**Scheduling work:**

| Method | Behavior |
|---|---|
| `c.Every("30m", fn)` | Calls `fn` repeatedly on the given interval |
| `c.After("10s", fn)` | Calls `fn` once after the given delay |

Durations are Go duration strings (`"30s"`, `"5m"`, `"1h30m"`, etc.). An invalid duration string is silently ignored. All timers are stopped automatically when the script is reloaded or disabled.

Here is a complete script that posts a reminder every hour:

```go
package main

// cascade:name standup

import "github.com/matt0x6f/irc-client/cascade"

func Setup(c *cascade.Client) {
	c.Every("1h", func() {
		c.Network("Libera").Say("#team", "hourly check-in")
	})
}
```

---

## The sandbox — what you can and can't import

!!! warning "No standard library"
    The script sandbox exposes **only** `github.com/matt0x6f/irc-client/cascade`. There is no standard library inside the interpreter — not `fmt`, not `strings`, not `os`, nothing. Importing any other package causes the script to fail to load and its status badge shows **Error**.

Inside that constraint you have everything the `cascade` package provides (event helpers, `Reply`, `Say`, timers) plus Go built-ins: string concatenation with `+`, `len`, `append`, `switch`, `range`, maps, and slices. Those are enough for most personal automation.

If your script genuinely needs a third-party library, external process access, or network I/O beyond the IRC connection, the right tool is a [plugin](../developers/plugin-system.md) instead. See [Lifecycle & limits](lifecycle-and-limits.md) for the full sandbox rules and watchdog behavior.

---

## Editor setup

Cascade scaffolds a `go.mod` in the scripts directory when it is first created:

```
module cascade-scripts

go 1.21

require github.com/matt0x6f/irc-client/cascade v1.0.0
```

This file exists so your editor and gopls give you real autocomplete and type-checking for the `cascade` package. The `cascade` module is published, so gopls resolves the import directly — no extra setup needed. The `go.mod` powers editor tooling only; the running app interprets your script regardless of what it says.

---

See [Examples](examples.md) for complete recipes and the [API reference](api-reference.md) for every type and method.

# Scripting

Cascade Scripting puts mIRC-style automation right in the client, with no separate process to run or protocol to learn. You write plain Go; Cascade runs it in-process, where it can react to channel events or fire on a timer. Scripts sit a level below the plugin system: a lightweight, statically typed way to automate the personal workflows you trust.

## Scripts vs. plugins

Both scripts and plugins can react to IRC events and send messages. They trade off power against simplicity in different ways:

| | Scripts | Plugins |
|---|---|---|
| **Runs in** | In-process (Yaegi-interpreted Go) | Separate subprocess (JSON-RPC over stdin/stdout) |
| **Best for** | Quick, trusted, personal automation | Substantial, shippable, or untrusted/heavy tools |
| **Install** | Drop a `.go` file in a folder | Build and ship a binary |
| **Isolation** | Contained by a watchdog, not killed | Full process isolation |

When a script starts needing real isolation, arbitrary third-party dependencies, or hostile-input safety, that's the signal to make it a [plugin](../developers/plugin-system.md) instead.

## How it works

A script is a **directory** of `package main` Go files placed inside the Cascade scripts folder. When Cascade starts (or when you save a file), it reads those files, type-checks them against the `cascade` SDK, and loads any exported handler functions it finds: `OnText`, `OnNotice`, `OnJoin`, `OnPart`. If the script also exports `Setup`, that runs once right after loading so you can register timers and proactive behavior.

Because the type check happens at load time, errors surface right away in the Scripts panel rather than at runtime. Fix the error, save, and Cascade hot-reloads the script automatically.

## The capability sandbox

A script can call the `cascade` API and nothing else. The interpreter exposes no standard library: no `fmt`, no `strings`, no `os`, no network, no filesystem. That missing surface is the sandbox. A script can send messages by replying to an event (`e.Reply(...)`) or proactively via `client.Network("name").Say(...)`, and it can read the event it receives. Those are the only ways in and out.

See [Lifecycle & limits](lifecycle-and-limits.md) for the full picture, including the watchdog that auto-disables misbehaving scripts.

## Where to go next

- [Quickstart](quickstart.md) — write and run your first script in about five minutes
- [Writing scripts](writing-scripts.md) — the complete authoring guide: handlers, the manifest header, timers, the sandbox, and editor setup
- [Examples](examples.md) — a cookbook of copy-paste-ready recipes
- [API reference](api-reference.md) — every type, method, and handler signature in the `cascade` package
- [Lifecycle & limits](lifecycle-and-limits.md) — load/reload/unload lifecycle, status values, the watchdog, sandbox rules, and when to graduate to a plugin
- [Managing scripts](managing-scripts.md) — the Scripts panel: creating, enabling, disabling, and reloading scripts from the UI

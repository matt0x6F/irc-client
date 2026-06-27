# Lifecycle & limits

This page covers what Cascade does with a script from the moment it loads until it is deleted, the four status values you will see in the Scripts panel, the watchdog that bounds handler execution, the sandbox rules, and when to graduate to a plugin.

---

## Lifecycle

A script moves through the following stages:

1. **Load** — Cascade discovers your script folder under `~/.cascade-chat/scripts/`, compiles all `.go` files in it, and calls `Setup` (if defined). The status becomes `loaded`.
2. **Run** — Cascade dispatches incoming events (`OnText`, `OnJoin`, etc.) and fires any timers your `Setup` registered. The script lives here until something changes.
3. **Hot-reload on save** — When you save a source file, Cascade automatically reloads the script. The interpreter is recreated: `Setup` runs again, and all timers are re-registered from scratch.
4. **Enable / disable** — You can toggle a script off in the Scripts panel. Disabling it stops all dispatch and cancels timers. Re-enabling it runs `Setup` again, exactly as a fresh load.
5. **Unload** — Deleting the script folder removes it entirely. All timers stop and the script disappears from the panel.

!!! warning "Package-level state does not survive a reload"
    The interpreter is recreated on every reload or enable. Package-level variables are reset to their zero values — do not rely on in-memory state persisting across a save.

---

## Status values

The Scripts panel shows one of four statuses for each script:

| Status | Meaning |
|---|---|
| `loaded` | Running normally; events are being dispatched. |
| `disabled` | Turned off — either by you in the panel or persisted off from a previous session. No events are dispatched and no timers run. |
| `error` | Failed to load. A compile or type error, a forbidden import, or a `Setup` panic prevented the script from starting. The panel shows the error message. |
| `runaway` | Auto-disabled by the watchdog (see below). The panel shows the reason. Re-enable from the panel to reset the strike count and re-run `Setup`. |

---

## The watchdog — keep handlers fast

!!! warning "Handlers must complete within 2 seconds"
    Every handler call and every timer callback is bounded to a **2-second deadline**. If a handler exceeds that deadline, Cascade immediately marks the script as `runaway` and stops dispatching further events to it.

    Cascade **contains** the damage — it stops new dispatch — but **cannot kill an already-spinning goroutine**. A handler that hangs will continue consuming a goroutine until the process exits. Never block, sleep-loop, or run unbounded work inside a handler.

Separately, **3 consecutive failing dispatches** (including panics) also auto-disable the script as `runaway`. A single successful dispatch resets the strike counter. You do not have to exceed the deadline to hit this limit — a handler that panics three times in a row will be disabled.

To recover a `runaway` script: click **Enable** in the Scripts panel. That clears the strike count and re-runs `Setup`.

**What "fast" means in practice:**

- Do not attempt to call `time.Sleep` or any blocking I/O. There is no `time` package in the sandbox, so code that imports it will fail to load with an `error` status — it never reaches the point of being blocked at runtime.
- Do not loop over unbounded input inside a handler. If you need to process a large data set, break it into smaller chunks driven by timers.
- Return from handlers as soon as you have dispatched a reply; do not wait for confirmation.

---

## The sandbox

!!! warning "Only the `cascade` package is available"
    Scripts can import **only** `github.com/matt0x6f/irc-client/cascade`. There is no standard library inside the script interpreter — not `fmt`, not `strings`, not `os`, nothing. Importing any other package is a load-time error and the script status shows `error`.

This is the security model: a script can do exactly what the `cascade` API allows and nothing more. The API deliberately limits you to IRC operations (reply, say, join/part via timers) so a script cannot read files, make arbitrary network connections, or spawn processes.

You still have Go built-ins — string concatenation with `+`, `len`, `append`, `switch`, `range`, maps, and slices — and the full event, reply, and timer surface from the `cascade` package. See [Writing scripts](writing-scripts.md) for the complete list of available types and methods.

---

## Permissions (v1)

The `// cascade:permissions` header is parsed and displayed in the Scripts panel alongside the script name and description. For example:

```go
// cascade:permissions network, admin
```

**In v1, permissions are not enforced.** They are a forward-looking declaration — a signal to you and other users about what capabilities the script intends to use. Do not treat the permissions list as a security boundary in v1; enforcement will be added in a future release.

---

## When to use a plugin instead

Scripts are designed for personal, single-user automation inside the Cascade sandbox. If you need any of the following, graduate to a [plugin](../developers/plugin-system.md):

- **Real process isolation** — plugins run as separate subprocesses; a crashing plugin cannot take down Cascade.
- **Third-party dependencies** — plugins can import any Go or other-language library.
- **Untrusted or hostile input** — if you are processing input from external sources that could be adversarial, the plugin subprocess boundary provides a much stronger containment model than the script watchdog.
- **Persistent background I/O** — long-lived connections (webhooks, websocket listeners, database access) fit naturally into a plugin's process model.

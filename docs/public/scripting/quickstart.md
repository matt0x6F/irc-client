# Quickstart

You'll have a working script in about five minutes. By the end you'll know how to create, edit, and hot-reload a script without ever restarting Cascade.

## 1. Open the Scripts panel

In the Cascade menu bar, go to **Settings → Scripts**. This is your command center for creating and monitoring scripts. If you need a full tour of the panel's controls, see [Managing scripts](managing-scripts.md).

## 2. Create a script

In the Scripts panel, type `greeter` in the name field and click **New script**. Cascade creates a `greeter/` directory inside the scripts folder, scaffolds a starter `greeter.go` file, and reveals it in the panel.

To find the file on disk, click **Open scripts folder** (macOS: **Reveal in Finder**; Windows/Linux: **Reveal in folder**).

## 3. Edit the script

Open `greeter/greeter.go` in any text editor and replace its entire contents with the following:

```go
package main

// cascade:name greeter
// cascade:description Replies "hello!" when someone says !hi

import "github.com/matt0x6f/irc-client/cascade"

func OnText(e cascade.TextEvent) {
	if e.HasPrefix("!hi") {
		e.Reply("hello, " + e.Nick + "!")
	}
}
```

The comment header declares the script's display name and description; the `OnText` handler fires whenever a message arrives in any channel you're watching.

## 4. Save — Cascade hot-reloads automatically

Save the file. Cascade detects the change and reloads the script immediately — no restart required. Within a second or two the Scripts panel shows the script's status badge as **Loaded**.

## 5. Try it

In any channel where you're present, type `!hi`. You should see Cascade reply with:

```
hello, <yournick>!
```

That's it — your first live, running script.

## If it didn't load

If the status badge shows **Error** instead of **Loaded**, click the badge to expand the error message. A typo in a function signature or an unrecognized import is the most common cause — fix it, save again, and Cascade hot-reloads once more.

For a detailed look at the load/reload/unload lifecycle and what the watchdog does when a script misbehaves, see [Lifecycle & limits](lifecycle-and-limits.md). For help reading error messages and managing individual scripts from the UI, see [Managing scripts](managing-scripts.md).

---

**Next:** [Writing scripts](writing-scripts.md) — handlers, timers, the manifest header, and the full sandbox rules.

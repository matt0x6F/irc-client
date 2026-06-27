# Examples

Each recipe is a complete script — make a folder under your scripts directory, drop the file in, and save.

---

## Keyword greeter

Replies when someone says `!hi` in a channel.

```go
package main

// cascade:name greeter
// cascade:description Replies when someone says !hi

import "github.com/matt0x6f/irc-client/cascade"

func OnText(e cascade.TextEvent) {
	if e.HasPrefix("!hi") {
		e.Reply("hello, " + e.Nick + "!")
	}
}
```

---

## Prefix-command responder

Responds to simple `!`-prefixed commands.

```go
package main

// cascade:name commands
// cascade:description Responds to simple !-prefixed commands

import "github.com/matt0x6f/irc-client/cascade"

func OnText(e cascade.TextEvent) {
	switch e.Arg(1) {
	case "!ping":
		e.Reply("pong")
	case "!echo":
		e.Reply(e.Message)
	}
}
```

---

## Scheduled announcement

Posts a reminder to a channel every 30 minutes using a timer and `Say`.

```go
package main

// cascade:name announcer
// cascade:description Posts a reminder to #ops every 30 minutes

import "github.com/matt0x6f/irc-client/cascade"

func Setup(c *cascade.Client) {
	c.Every("30m", func() {
		c.Network("Libera").Say("#ops", "reminder: stand-up in 5")
	})
}
```

---

## Join welcomer

Greets users as they join a channel you are in.

```go
package main

// cascade:name welcomer
// cascade:description Greets users as they join

import "github.com/matt0x6f/irc-client/cascade"

func OnJoin(e cascade.JoinEvent) {
	e.Reply("welcome, " + e.Nick + "!")
}
```

---

!!! note "Customize for your setup"
    Replace `Libera` with your own network name (as shown in the Networks settings) and `#ops` or `#team` with the channels you want to target.

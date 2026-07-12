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
	case "!hi":
		e.Reply("hello, " + e.Nick + "!")
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

## Away-aware lookup

Checks the latest session-local status Cascade knows for a user. Keep the client from `Setup` when an event handler needs proactive network access.

```go
package main

// cascade:name away-check

import "github.com/matt0x6f/irc-client/cascade"

var client *cascade.Client

func Setup(c *cascade.Client) { client = c }

func OnText(e cascade.TextEvent) {
	if e.Message != "!where-alice" {
		return
	}
	alice := client.Network(e.Network).User("alice")
	if !alice.Known() {
		e.Reply("I don't have current status for alice")
	} else if alice.IsAway() {
		e.Reply("alice is away: " + alice.Status().AwayMessage)
	} else {
		e.Reply("alice is not marked away")
	}
}
```

---

## React to away changes

```go
package main

// cascade:name away-log

import "github.com/matt0x6f/irc-client/cascade"

var client *cascade.Client

func Setup(c *cascade.Client) { client = c }

func OnUserStatus(e cascade.UserStatusEvent) {
	if e.IsSelf() {
		return
	}
	if e.Status.Away {
		client.Network(e.Network).Say("#ops", e.Nick+" is now away")
	}
}
```

---

!!! note "Customize for your setup"
    Replace `Libera` with your own network name and `#ops` with the channel you want to target (both are shown in the Networks settings).

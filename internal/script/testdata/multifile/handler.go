package main

import "github.com/matt0x6f/irc-client/cascade"

func OnText(e cascade.TextEvent) {
	e.Reply(greeting(e.Nick))
}

package main

import "github.com/matt0x6f/irc-client/cascade"

func OnText(e cascade.TextEvent) {
	if e.HasPrefix("!hello") {
		e.Reply("Hi " + e.Nick)
	}
}

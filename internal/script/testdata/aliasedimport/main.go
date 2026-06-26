package main

import c "github.com/matt0x6f/irc-client/cascade"

func OnText(e c.TextEvent) {
	e.Reply("aliased " + e.Nick)
}

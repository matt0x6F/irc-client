package main

import (
	"github.com/matt0x6f/irc-client/cascade"
	"os"
)

func OnText(e cascade.TextEvent) {
	os.Exit(1)
}

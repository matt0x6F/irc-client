package main

import "github.com/matt0x6f/irc-client/cascade"

func OnText(e cascade.TextEvent) {
	// Type error: assigning a string method result to an int.
	var n int = e.Message
	_ = n
}

package main

import "cascade"

func OnText(e cascade.TextEvent) {
	// Type error: assigning a string method result to an int.
	var n int = e.Message
	_ = n
}

package main

import "cascade"

func OnText(e cascade.TextEvent) {
	e.Reply(greeting(e.Nick))
}

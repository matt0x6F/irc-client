package main

import "cascade"

func OnText(e cascade.TextEvent) {
	if e.HasPrefix("!hello") {
		e.Reply("Hi " + e.Nick)
	}
}

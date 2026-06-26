package main

import (
	"cascade"
	"os"
)

func OnText(e cascade.TextEvent) {
	os.Exit(1)
}

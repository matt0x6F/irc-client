package main

import c "cascade"

func OnText(e c.TextEvent) {
	e.Reply("aliased " + e.Nick)
}

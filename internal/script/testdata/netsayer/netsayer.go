package main

import "github.com/matt0x6f/irc-client/cascade"

func Setup(c *cascade.Client) { c.Network("netA").Say("#x", "hello") }

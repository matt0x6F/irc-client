package main

import "github.com/matt0x6f/irc-client/cascade"

func Setup(c *cascade.Client) { c.After("0s", func() {}) }

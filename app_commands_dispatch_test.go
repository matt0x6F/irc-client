package main

import (
	"strings"
	"testing"
)

func TestSendCommandUsageError(t *testing.T) {
	a := &App{commands: buildBuiltinRegistry()}
	// JOIN requires 1 arg; expect the generated usage error, not a panic.
	err := a.dispatchCommand(nil, 1, "JOIN", nil, "JOIN")
	if err == nil || !strings.Contains(err.Error(), "usage: /join #channel [key]") {
		t.Fatalf("got %v; want JOIN usage error", err)
	}
}

func TestSendCommandFrontendNoOp(t *testing.T) {
	a := &App{commands: buildBuiltinRegistry()}
	if err := a.dispatchCommand(nil, 1, "HELP", nil, "HELP"); err != nil {
		t.Fatalf("HELP (frontend) dispatch should no-op, got %v", err)
	}
}

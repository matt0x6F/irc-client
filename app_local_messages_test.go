package main

import "testing"

func TestPrintLocalLinesValidation(t *testing.T) {
	a := &App{}
	if err := a.PrintLocalLines(1, "status", nil); err != nil {
		t.Fatalf("empty lines should be a no-op, got %v", err)
	}
}

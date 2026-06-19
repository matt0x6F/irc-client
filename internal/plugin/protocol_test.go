package plugin

import (
	"encoding/json"
	"testing"
)

func TestInitializeResultCommandsRoundTrip(t *testing.T) {
	in := InitializeResult{Name: "weather", Commands: []CommandSpecWire{{Name: "weather", Usage: "<city>", Description: "Show weather"}}}
	b, _ := json.Marshal(in)
	var out InitializeResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Commands) != 1 || out.Commands[0].Name != "weather" {
		t.Fatalf("commands did not round-trip: %+v", out.Commands)
	}
}

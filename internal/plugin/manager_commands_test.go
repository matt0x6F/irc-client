package plugin

import "testing"

func TestRegisterPluginCommandsConflicts(t *testing.T) {
	pm := &Manager{pluginCommands: map[string]pluginCommandEntry{}}
	builtins := map[string]bool{"JOIN": true, "J": true}
	isBuiltin := func(k string) bool { return builtins[k] }

	pm.registerPluginCommands("p1", []CommandSpecWire{{Name: "weather", Aliases: []string{"wx"}}}, isBuiltin)
	if _, ok := pm.LookupPluginCommand("weather"); !ok {
		t.Fatal("weather should be registered")
	}
	if _, ok := pm.LookupPluginCommand("WX"); !ok {
		t.Fatal("alias wx should be registered")
	}

	// Cannot shadow a built-in.
	pm.registerPluginCommands("p1", []CommandSpecWire{{Name: "join"}}, isBuiltin)
	if e, _ := pm.LookupPluginCommand("JOIN"); e.Plugin == "p1" {
		t.Fatal("plugin must not shadow built-in JOIN")
	}

	// Second plugin cannot steal an existing plugin command.
	pm.registerPluginCommands("p2", []CommandSpecWire{{Name: "weather"}}, isBuiltin)
	if e, _ := pm.LookupPluginCommand("weather"); e.Plugin != "p1" {
		t.Fatalf("first registration wins; got %q", e.Plugin)
	}

	pm.unregisterPluginCommands("p1")
	if _, ok := pm.LookupPluginCommand("weather"); ok {
		t.Fatal("weather should be gone after unregister")
	}
}

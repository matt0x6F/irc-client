package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/plugin"
	"github.com/matt0x6f/irc-client/internal/storage"
)

func TestPluginEnableDisableEndpointsUsePersistentLifecycle(t *testing.T) {
	stor, err := storage.NewStorage(filepath.Join(t.TempDir(), "app.db"), 10, time.Hour)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = stor.Close() })

	manager := plugin.NewManager(events.NewEventBus(), t.TempDir())
	manager.SetStorage(stor)
	t.Cleanup(func() { _ = manager.Close() })
	app := &App{storage: stor, pluginManager: manager}

	// Disabling an installed-but-not-running plugin is a valid, durable action.
	if err := app.DisablePlugin("weather"); err != nil {
		t.Fatalf("DisablePlugin: %v", err)
	}
	config, err := stor.GetPluginConfig("weather")
	if err != nil {
		t.Fatalf("GetPluginConfig: %v", err)
	}
	if config.Enabled {
		t.Fatal("DisablePlugin did not persist disabled state")
	}

	// Enabling is routed through discovery/loading, and a failed load restores
	// the prior durable state instead of leaving the plugin enabled on restart.
	err = app.EnablePlugin("weather")
	if err == nil || !strings.Contains(err.Error(), "plugin not found") {
		t.Fatalf("EnablePlugin error = %v", err)
	}
	config, err = stor.GetPluginConfig("weather")
	if err != nil {
		t.Fatalf("GetPluginConfig after failed enable: %v", err)
	}
	if config.Enabled {
		t.Fatal("failed EnablePlugin did not restore disabled state")
	}
}

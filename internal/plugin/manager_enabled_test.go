package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

func newPluginTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	stor, err := storage.NewStorage(filepath.Join(t.TempDir(), "plugins.db"), 10, time.Hour)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = stor.Close() })
	return stor
}

func writeTestPlugin(t *testing.T, dir, name string, markerPath string) {
	t.Helper()
	markerLine := ""
	if markerPath != "" {
		markerLine = "printf 'started\\n' >> " + markerPath + "\n"
	}
	script := "#!/bin/sh\n" + markerLine + `while IFS= read -r line; do
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"name":"` + name + `","version":"1.0.0"}}'
done
`
	path := filepath.Join(dir, "cascade-"+name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
}

func TestSetPluginEnabledPersistsAndReconcilesProcess(t *testing.T) {
	pluginDir := t.TempDir()
	writeTestPlugin(t, pluginDir, "weather", "")
	stor := newPluginTestStorage(t)
	if err := stor.SetPluginEnabled("weather", false); err != nil {
		t.Fatalf("seed disabled state: %v", err)
	}

	pm := NewManager(events.NewEventBus(), pluginDir)
	pm.SetStorage(stor)
	t.Cleanup(func() { _ = pm.Close() })

	if err := pm.SetPluginEnabled("weather", true, stor); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if _, loaded := pm.plugins["weather"]; !loaded {
		t.Fatal("enabled plugin was not loaded")
	}
	config, err := stor.GetPluginConfig("weather")
	if err != nil {
		t.Fatalf("get enabled config: %v", err)
	}
	if !config.Enabled {
		t.Fatal("enabled state was not persisted")
	}

	if err := pm.SetPluginEnabled("weather", false, stor); err != nil {
		t.Fatalf("disable plugin: %v", err)
	}
	if _, loaded := pm.plugins["weather"]; loaded {
		t.Fatal("disabled plugin is still loaded")
	}
	config, err = stor.GetPluginConfig("weather")
	if err != nil {
		t.Fatalf("get disabled config: %v", err)
	}
	if config.Enabled {
		t.Fatal("disabled state was not persisted")
	}

	// Disabling an already-stopped plugin is idempotent and must not surface the
	// old "plugin not loaded" error to the settings UI.
	if err := pm.SetPluginEnabled("weather", false, stor); err != nil {
		t.Fatalf("disable plugin again: %v", err)
	}
}

func TestSetPluginEnabledRestoresStateWhenLoadFails(t *testing.T) {
	pluginDir := t.TempDir()
	path := filepath.Join(pluginDir, "cascade-broken")
	if err := os.WriteFile(path, []byte("not executable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stor := newPluginTestStorage(t)
	if err := stor.SetPluginEnabled("broken", false); err != nil {
		t.Fatalf("seed disabled state: %v", err)
	}

	pm := NewManager(events.NewEventBus(), pluginDir)
	pm.SetStorage(stor)
	t.Cleanup(func() { _ = pm.Close() })

	err := pm.SetPluginEnabled("broken", true, stor)
	if err == nil || !strings.Contains(err.Error(), "plugin validation failed") {
		t.Fatalf("enable broken plugin error = %v", err)
	}
	config, getErr := stor.GetPluginConfig("broken")
	if getErr != nil {
		t.Fatalf("get restored config: %v", getErr)
	}
	if config.Enabled {
		t.Fatal("failed enable left plugin persisted as enabled")
	}
}

func TestListPluginsDoesNotStartDisabledPlugin(t *testing.T) {
	pluginDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	writeTestPlugin(t, pluginDir, "quiet", marker)
	stor := newPluginTestStorage(t)
	if err := stor.SetPluginEnabled("quiet", false); err != nil {
		t.Fatalf("seed disabled state: %v", err)
	}

	pm := NewManager(events.NewEventBus(), pluginDir)
	pm.SetStorage(stor)
	t.Cleanup(func() { _ = pm.Close() })

	plugins := pm.ListPlugins()
	if len(plugins) != 1 || plugins[0].Name != "quiet" || plugins[0].Enabled {
		t.Fatalf("unexpected plugin listing: %+v", plugins)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("disabled plugin was started while listing; marker stat error = %v", err)
	}
}

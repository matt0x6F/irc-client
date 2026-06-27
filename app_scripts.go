package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/matt0x6f/irc-client/internal/extension"
)

// ScriptInfo is the Wails-bound view of a loaded script.
type ScriptInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Enabled     bool     `json:"enabled"`
	Error       string   `json:"error"`
	Perms       []string `json:"perms"`
}

// ListScripts returns a snapshot of every loaded script and its current status.
func (a *App) ListScripts() []ScriptInfo {
	exts := a.scriptMgr.Snapshot()
	out := make([]ScriptInfo, 0, len(exts))
	for _, e := range exts {
		out = append(out, ScriptInfo{
			ID:          string(e.ID),
			Name:        e.Name,
			Description: e.Description,
			Status:      string(e.Status),
			Enabled:     e.Enabled,
			Error:       e.Err,
			Perms:       e.Perms,
		})
	}
	return out
}

// EnableScript re-enables a previously disabled or runaway script.
func (a *App) EnableScript(id string) error {
	a.scriptMgr.Enable(extension.ID(id))
	return nil
}

// DisableScript stops a script's timers and marks it disabled.
func (a *App) DisableScript(id string) error {
	a.scriptMgr.Disable(extension.ID(id))
	return nil
}

// ReloadScript reloads a script from disk by its ID.
func (a *App) ReloadScript(id string) error {
	a.scriptMgr.ReloadByID(extension.ID(id))
	return nil
}

// NewScript creates a new script directory with a starter file under the
// scripts directory and returns the path to the created file.
func (a *App) NewScript(name string) (string, error) {
	return a.scriptMgr.NewScript(name)
}

// OpenScriptsDir reveals the scripts directory in the OS file manager.
// Uses "open" on macOS, "xdg-open" on Linux, and "explorer" on Windows.
// Note: no existing reveal-in-finder helper was found in the codebase;
// this is a minimal exec-based implementation.
func (a *App) OpenScriptsDir() error {
	dir := filepath.Join(a.dataDir, "scripts")
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "linux":
		cmd = exec.Command("xdg-open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		return fmt.Errorf("OpenScriptsDir: unsupported OS %q", runtime.GOOS)
	}
	return cmd.Start()
}

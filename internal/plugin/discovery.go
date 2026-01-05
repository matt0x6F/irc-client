package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matt0x6f/irc-client/internal/logger"
)

// DiscoverPlugins discovers plugins in the system
func DiscoverPlugins(pluginDir string) ([]*PluginInfo, error) {
	var plugins []*PluginInfo

	// Discover plugins in dedicated directory
	if pluginDir != "" {
		dirPlugins, err := discoverInDirectory(pluginDir)
		if err != nil {
			return nil, fmt.Errorf("failed to discover plugins in directory: %w", err)
		}
		plugins = append(plugins, dirPlugins...)
	}

	// Discover plugins in PATH
	pathPlugins, err := discoverInPATH()
	if err != nil {
		// Non-fatal, just log
		logger.Log.Warn().Err(err).Msg("Failed to discover plugins in PATH")
	}
	plugins = append(plugins, pathPlugins...)

	return plugins, nil
}

// discoverInDirectory discovers plugins in a specific directory
func discoverInDirectory(dir string) ([]*PluginInfo, error) {
	var plugins []*PluginInfo

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return plugins, nil // Directory doesn't exist, return empty
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Check if it's executable and matches naming pattern
		path := filepath.Join(dir, entry.Name())
		if strings.HasPrefix(entry.Name(), "cascade-") || isExecutable(path) {
			info, err := getPluginInfo(path)
			if err != nil {
				logger.Log.Warn().Err(err).Str("path", path).Msg("Failed to get plugin info")
				continue
			}
			plugins = append(plugins, info)
		}
	}

	return plugins, nil
}

// discoverInPATH discovers plugins in system PATH
func discoverInPATH() ([]*PluginInfo, error) {
	var plugins []*PluginInfo

	pathEnv := os.Getenv("PATH")
	paths := strings.Split(pathEnv, string(os.PathListSeparator))

	for _, path := range paths {
		entries, err := os.ReadDir(path)
		if err != nil {
			continue // Skip directories we can't read
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			// Check for cascade- prefix
			if strings.HasPrefix(entry.Name(), "cascade-") {
				fullPath := filepath.Join(path, entry.Name())
				info, err := getPluginInfo(fullPath)
				if err != nil {
					continue
				}
				plugins = append(plugins, info)
			}
		}
	}

	return plugins, nil
}

// getPluginInfo retrieves plugin metadata
func getPluginInfo(path string) (*PluginInfo, error) {
	info := &PluginInfo{
		Path:    path,
		Enabled: true, // Default to enabled
	}

	// Try to read plugin.json in the same directory
	dir := filepath.Dir(path)
	metadataPath := filepath.Join(dir, "plugin.json")
	if data, err := os.ReadFile(metadataPath); err == nil {
		if err := json.Unmarshal(data, info); err == nil {
			// Metadata loaded successfully
		}
	}

	// If name not set, derive from filename
	if info.Name == "" {
		name := filepath.Base(path)
		if strings.HasPrefix(name, "cascade-") {
			info.Name = strings.TrimPrefix(name, "cascade-")
		} else {
			info.Name = name
		}
	}

	return info, nil
}

// isExecutable checks if a file is executable
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Perm()&0111 != 0
}

// ValidatePlugin validates that a plugin executable exists and is valid
func ValidatePlugin(path string) error {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("plugin not found: %s", path)
	}

	// Try to execute with --version or similar to validate
	cmd := exec.Command(path, "--version")
	if err := cmd.Run(); err != nil {
		// Not necessarily an error, plugin might not support --version
		// Just check that the file is executable
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("plugin is not executable: %s", path)
		}
	}

	return nil
}


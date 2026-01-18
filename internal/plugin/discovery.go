package plugin

import (
	"fmt"
	"os"
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
			// Check if directory contains a cascade- executable
			subDir := filepath.Join(dir, entry.Name())
			subEntries, err := os.ReadDir(subDir)
			if err != nil {
				continue
			}
			for _, subEntry := range subEntries {
				if subEntry.IsDir() {
					continue
				}
				subPath := filepath.Join(subDir, subEntry.Name())
				if strings.HasPrefix(subEntry.Name(), "cascade-") || isExecutable(subPath) {
					info, err := getPluginInfo(subPath)
					if err != nil {
						logger.Log.Warn().Err(err).Str("path", subPath).Msg("Failed to get plugin info")
						continue
					}
					plugins = append(plugins, info)
					break // Only one plugin per directory
				}
			}
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

// getPluginInfo creates minimal plugin info from executable path
// Full metadata will be retrieved from the plugin's initialize response
func getPluginInfo(path string) (*PluginInfo, error) {
	// Derive name from filename
	name := filepath.Base(path)
	if strings.HasPrefix(name, "cascade-") {
		name = strings.TrimPrefix(name, "cascade-")
	}

	info := &PluginInfo{
		Name:    name,
		Path:    path,
		Enabled: true, // Default to enabled, database will override
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
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("plugin not found: %s", path)
	}
	if err != nil {
		return fmt.Errorf("failed to stat plugin: %w", err)
	}

	// Check that the file is executable
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("plugin is not executable: %s", path)
	}

	// Don't actually run the plugin here - it reads from stdin and will hang
	// Just verify it exists and is executable
	return nil
}

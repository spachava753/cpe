package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultConversationStoragePath is used when defaults.conversationStoragePath is not set.
const DefaultConversationStoragePath = ".cpeconvo"

// ResolveConversationStoragePath resolves defaults.conversationStoragePath into
// the effective SQLite path used at runtime.
//
// Resolution contract:
//   - empty value => DefaultConversationStoragePath
//   - supports ~ and ~/... home expansion
//   - absolute paths are cleaned and used directly
//   - relative paths are resolved against config file directory when known;
//     otherwise they remain relative to the current working directory
//
// This function only resolves/normalizes paths and does not create directories
// or check filesystem permissions.
func ResolveConversationStoragePath(defaults Defaults, configFilePath string) (string, error) {
	rawPath := defaults.ConversationStoragePath
	if rawPath == "" {
		return DefaultConversationStoragePath, nil
	}

	path, err := expandHomePath(rawPath)
	if err != nil {
		return "", err
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	if configFilePath == "" {
		return filepath.Clean(path), nil
	}

	return filepath.Clean(filepath.Join(filepath.Dir(configFilePath), path)), nil
}

// expandHomePath expands ~ prefixes for the current user.
// Supported forms are "~" and "~/..." (including "~\\..." on Windows-style input).
// User-qualified forms such as "~otheruser/..." are rejected.
func expandHomePath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory for %q: %w", path, err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}

	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("unsupported home path format %q (use ~/...)", path)
	}

	return path, nil
}

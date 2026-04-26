package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultConversationStoragePath is used when no conversation storage path flag or environment variable is set.
const DefaultConversationStoragePath = ".cpeconvo"

// ConversationStorageEnvVar is the environment variable used for conversation database path selection.
const ConversationStorageEnvVar = "CPE_DB_PATH"

// ResolveConversationStoragePath resolves a CLI/env conversation storage path into the effective SQLite path.
//
// Resolution contract:
//   - empty value => DefaultConversationStoragePath
//   - supports ~ and ~/... home expansion
//   - absolute paths are cleaned and used directly
//   - relative paths remain relative to the current working directory
//
// This function only resolves/normalizes paths and does not create directories or check filesystem permissions.
func ResolveConversationStoragePath(rawPath string) (string, error) {
	if rawPath == "" {
		return DefaultConversationStoragePath, nil
	}

	path, err := expandHomePath(rawPath)
	if err != nil {
		return "", err
	}

	return filepath.Clean(path), nil
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

package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// normalizeCodeModeConfigPaths returns a normalized copy of codeMode where each
// localModulePaths entry is trimmed, resolved to an absolute canonical path, and
// deduplicated after resolution.
//
// The input config object is never mutated.
func normalizeCodeModeConfigPaths(codeMode *CodeModeConfig, configFilePath string) (*CodeModeConfig, error) {
	if codeMode == nil {
		return nil, nil
	}

	normalized := *codeMode
	normalized.ExcludedTools = append([]string(nil), codeMode.ExcludedTools...)
	normalized.LocalModulePaths = make([]string, 0, len(codeMode.LocalModulePaths))

	seen := make(map[string]struct{}, len(codeMode.LocalModulePaths))
	for i, rawPath := range codeMode.LocalModulePaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return nil, fmt.Errorf("localModulePaths[%d] must not be empty", i)
		}

		resolvedPath, err := resolveCodeModePath(trimmed, configFilePath)
		if err != nil {
			return nil, fmt.Errorf("localModulePaths[%d]: %w", i, err)
		}

		if _, exists := seen[resolvedPath]; exists {
			return nil, fmt.Errorf("localModulePaths contains duplicate path: %s", resolvedPath)
		}
		seen[resolvedPath] = struct{}{}
		normalized.LocalModulePaths = append(normalized.LocalModulePaths, resolvedPath)
	}

	return &normalized, nil
}

// resolveCodeModePath resolves one codeMode path using the following rules:
//   - expand ~ via expandHomePath
//   - if still relative and configFilePath is known, resolve relative to config dir
//   - convert to absolute path, clean it, then best-effort symlink evaluation
//
// The returned path is suitable for duplicate detection and filesystem checks.
func resolveCodeModePath(path, configFilePath string) (string, error) {
	expandedPath, err := expandHomePath(path)
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(expandedPath) && configFilePath != "" {
		expandedPath = filepath.Join(filepath.Dir(configFilePath), expandedPath)
	}

	absolutePath, err := filepath.Abs(expandedPath)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path for %q: %w", path, err)
	}

	resolvedPath := filepath.Clean(absolutePath)
	if realPath, err := filepath.EvalSymlinks(resolvedPath); err == nil {
		resolvedPath = filepath.Clean(realPath)
	}

	return resolvedPath, nil
}

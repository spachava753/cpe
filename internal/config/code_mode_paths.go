package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

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

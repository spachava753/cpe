package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrConfigNotFound indicates no config file was found in the standard search locations.
var ErrConfigNotFound = errors.New("configuration file not found")

// LoadRawConfig loads raw config for commands that need to list/inspect all models.
func LoadRawConfig(explicitPath string) (*RawConfig, error) {
	cfg, _, err := LoadRawConfigWithPath(explicitPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadRawConfigWithPath loads raw config and returns the resolved config file path.
func LoadRawConfigWithPath(explicitPath string) (*RawConfig, string, error) {
	var configPath string
	var err error

	if explicitPath != "" {
		configPath = explicitPath
		if _, err := os.Stat(configPath); err != nil {
			if os.IsNotExist(err) {
				return nil, "", fmt.Errorf("specified config file does not exist: %s", configPath)
			}
			return nil, "", fmt.Errorf("cannot access config file %s: %w", configPath, err)
		}
	} else {
		configPath, err = findConfigFile()
		if err != nil {
			return nil, "", fmt.Errorf("%w: %w", ErrConfigNotFound, err)
		}
	}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open config file %s: %w", configPath, err)
	}
	defer file.Close()

	config, err := loadRawConfigFromFile(file)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load config from %s: %w", configPath, err)
	}

	if err := config.ValidateWithConfigPath(configPath); err != nil {
		return nil, "", err
	}

	return config, configPath, nil
}

// findConfigFile searches for configuration files in the expected locations
func findConfigFile() (string, error) {
	configNames := []string{"cpe.yaml", "cpe.yml"}

	// Check current directory first
	for _, name := range configNames {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
	}

	// Check user config directory
	userConfigDir, err := os.UserConfigDir()
	if err == nil {
		cpeConfigDir := filepath.Join(userConfigDir, "cpe")
		for _, name := range configNames {
			path := filepath.Join(cpeConfigDir, name)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	userConfigPath := "~/.config/cpe/cpe.yaml"
	if userConfigDir != "" {
		userConfigPath = filepath.Join(userConfigDir, "cpe", "cpe.yaml")
	}
	return "", fmt.Errorf(`configuration file not found. Create one of:
  - ./cpe.yaml (current directory)
  - %s (user config directory)`, userConfigPath)
}

// loadRawConfigFromFile reads and parses a configuration file from any fs.File source.
// This design allows for flexible testing with in-memory files, embedded configs,
// network sources, or any other io.Reader implementation.
// The file extension is automatically detected from the file's stat info.
func loadRawConfigFromFile(file fs.File) (*RawConfig, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("error getting file info: %w", err)
	}
	filename := stat.Name()

	return parseConfigData(data, filename)
}

// parseConfigData parses config data based on the filename extension.
// Environment variables are expanded in the raw content before parsing,
// supporting both $VAR and ${VAR} syntax throughout the entire config.
func parseConfigData(data []byte, filename string) (*RawConfig, error) {
	// Expand environment variables in the raw content before parsing
	expandedData := os.ExpandEnv(string(data))

	// Helper to add expansion hint to error messages
	addExpansionHint := func(parseErr error) error {
		if strings.Contains(string(data), "$") {
			return fmt.Errorf("%w (hint: environment variable expansion may have introduced invalid syntax if values contain special characters)", parseErr)
		}
		return parseErr
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".json":
		var config RawConfig
		if err := json.Unmarshal([]byte(expandedData), &config); err != nil {
			return nil, addExpansionHint(fmt.Errorf("error parsing JSON config: %w", err))
		}
		return &config, nil
	case ".yaml", ".yml":
		var config RawConfig
		if err := yaml.Unmarshal([]byte(expandedData), &config); err != nil {
			return nil, addExpansionHint(fmt.Errorf("error parsing YAML config: %w", err))
		}
		return &config, nil
	default:
		// Try YAML first, then JSON as fallback
		// Use separate structs to avoid partial corruption if one parser partially succeeds
		var yamlConfig RawConfig
		yamlErr := yaml.Unmarshal([]byte(expandedData), &yamlConfig)
		if yamlErr == nil {
			return &yamlConfig, nil
		}
		var jsonConfig RawConfig
		jsonErr := json.Unmarshal([]byte(expandedData), &jsonConfig)
		if jsonErr == nil {
			return &jsonConfig, nil
		}
		return nil, addExpansionHint(fmt.Errorf("failed to parse config file: YAML error: %v, JSON error: %v", yamlErr, jsonErr))
	}
}

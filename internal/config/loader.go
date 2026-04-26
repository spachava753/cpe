package config

import (
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

// LoadRawConfig loads and validates configuration, returning only the parsed
// RawConfig. It is a convenience wrapper for callers that do not need the
// resolved config file path.
func LoadRawConfig(explicitPath string) (*RawConfig, error) {
	cfg, _, err := LoadRawConfigWithPath(explicitPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadRawConfigWithPath loads, parses, and validates configuration, returning
// both RawConfig and the concrete file path used.
//
// Path resolution contract:
//   - explicitPath: used as-is and must exist/readable.
//   - empty explicitPath: discover via findConfigFile search order.
//
// Validation is always executed with ValidateWithConfigPath so path-based config
// fields can be interpreted relative to the actual config file location.
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

// findConfigFile searches for configuration in deterministic precedence order:
// current working directory first, then the user config directory.
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

// loadRawConfigFromFile reads and parses YAML config from any fs.File.
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

// parseConfigData expands environment variables and parses YAML configuration data.
//
// Parsing contract:
//   - $VAR and ${VAR} are expanded on raw bytes before decoding.
//   - .json files are rejected; CPE config is YAML-only.
func parseConfigData(data []byte, filename string) (*RawConfig, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".json" {
		return nil, fmt.Errorf("JSON config files are no longer supported; use YAML (.yaml or .yml)")
	}

	expandedData := os.ExpandEnv(string(data))
	var config RawConfig
	decoder := yaml.NewDecoder(strings.NewReader(expandedData))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		parseErr := fmt.Errorf("error parsing YAML config: %w", err)
		if strings.Contains(string(data), "$") {
			parseErr = fmt.Errorf("%w (hint: environment variable expansion may have introduced invalid syntax if values contain special characters)", parseErr)
		}
		return nil, parseErr
	}
	return &config, nil
}

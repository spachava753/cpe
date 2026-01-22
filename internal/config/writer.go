package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FindDefaultConfigPath returns the default config path for the current platform
func FindDefaultConfigPath() string {
	// Check current directory first
	if _, err := os.Stat("cpe.yaml"); err == nil {
		return "cpe.yaml"
	}
	if _, err := os.Stat("cpe.yml"); err == nil {
		return "cpe.yml"
	}

	// Fall back to user config directory (platform-specific)
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "cpe.yaml"
	}
	return filepath.Join(configDir, "cpe", "cpe.yaml")
}

// LoadOrCreateRawConfig loads an existing config or returns an empty one
func LoadOrCreateRawConfig(path string) (*RawConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &RawConfig{
			Version: "1.0",
			Models:  []ModelConfig{},
		}, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg RawConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}

// WriteRawConfig writes the config to a file
func WriteRawConfig(path string, cfg *RawConfig) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// AddModel adds a model to the config, checking for duplicate refs
func (c *RawConfig) AddModel(model ModelConfig) error {
	for _, m := range c.Models {
		if m.Ref == model.Ref {
			return fmt.Errorf("model with ref %q already exists in config", model.Ref)
		}
	}

	c.Models = append(c.Models, model)

	if c.Version == "" {
		c.Version = "1.0"
	}

	return nil
}

// RemoveModel removes a model from the config by ref
func (c *RawConfig) RemoveModel(ref string) error {
	for i, m := range c.Models {
		if m.Ref == ref {
			c.Models = append(c.Models[:i], c.Models[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("model with ref %q not found in config", ref)
}

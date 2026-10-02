package config

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
)

// Config represents the settings used for different behavior
//
// TODO: fill in fields
type Config struct {
}

const configPath = ".cpe"

// Path returns the directory where runtime configuration is stored
func Path() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, configPath), nil
}

const settingsFile = "settings.json"

// LoadConfig reads settings.json relative to fsys. The caller selects the
// configuration directory, for example by passing os.DirFS after calling Path.
func LoadConfig(fsys fs.FS) (Config, error) {
	f, err := fsys.Open(settingsFile)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	var c Config
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return Config{}, err
	}

	return c, nil
}

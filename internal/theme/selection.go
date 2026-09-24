package theme

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// Names lists built-in and user-defined themes in alphabetical order. It reads
// the latest configuration but does not load palettes, so a broken active
// palette does not prevent choosing a different theme.
func Names(dir string) ([]string, error) {
	c, _, err := readConfiguration(dir)
	if err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(c.Themes)), nil
}

// Select validates and resolves name before atomically saving it as active in
// themes.json, using the supplied appearance snapshot for system sources. Other configuration values are preserved; a missing file is
// initialized from StarterJSON. Existing file symlinks are followed and kept.
// On error, callers must keep their previous theme. Successful selections also
// become the default for future sessions and other running CPE instances.
func Select(dir, name string, appearance Appearance) (Theme, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Theme{}, err
	}
	c, data, err := readConfiguration(dir)
	if err != nil {
		return Theme{}, err
	}
	c.Active = name
	t, err := c.resolve(dir, home, appearance)
	if err != nil {
		return Theme{}, err
	}
	// Retain the original definitions instead of writing the merged built-ins or
	// adding zero-valued fields to user definitions.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Theme{}, err
	}
	fields["active"], err = json.Marshal(name)
	if err != nil {
		return Theme{}, err
	}
	data, err = json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return Theme{}, err
	}
	if err := saveSelection(filepath.Join(dir, "themes.json"), append(data, '\n')); err != nil {
		return Theme{}, err
	}
	return t, nil
}

func saveSelection(path string, data []byte) error {
	mode := os.FileMode(0600)
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			info, err = os.Stat(path)
			if err != nil {
				return err
			}
		}
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".themes-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

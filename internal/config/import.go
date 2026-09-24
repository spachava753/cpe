package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// AddModels atomically adds missing profiles to config.json and returns the
// resulting profiles. Existing names, settings, defaults, and file permissions
// are preserved. A symlink is followed without replacing the link itself.
// The merged configuration is parsed before writing; concurrent CPE imports
// serialize through a stable lock beside the resolved configuration file.
func AddModels(ctx context.Context, dir string, additions map[string]Model) (map[string]Model, error) {
	path, err := filepath.EvalSymlinks(filepath.Join(dir, "config.json"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lock := flock.New(path+".lock", flock.SetPermissions(0600))
	defer lock.Close()
	if held, err := lock.TryLockContext(ctx, 50*time.Millisecond); err != nil {
		return nil, err
	} else if !held {
		return nil, ctx.Err()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := parse(dir, data)
	if err != nil {
		return nil, err
	}
	var document map[string]json.RawMessage
	var models map[string]json.RawMessage
	// parse has already established an object with valid model profiles.
	_ = json.Unmarshal(data, &document)
	_ = json.Unmarshal(document["models"], &models)
	added := 0
	for name, model := range additions {
		if _, exists := models[name]; exists {
			continue
		}
		models[name], err = json.Marshal(model)
		if err != nil {
			return nil, err
		}
		added++
	}
	if added == 0 {
		return c.Models, nil
	}
	document["models"], err = json.Marshal(models)
	if err != nil {
		return nil, err
	}
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	c, err = parse(dir, data)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("config.json must resolve to a regular file")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cpe-config-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(info.Mode().Perm()); err != nil {
		return nil, err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return nil, err
	}
	return c.Models, nil
}

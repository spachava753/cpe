package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// AddModels atomically adds missing profiles without replacing existing names or
// defaults. An initially empty catalog selects its first added profile.
func AddModels(ctx context.Context, dir string, additions map[string]Model) (map[string]Model, error) {
	c, err := editConfig(ctx, dir, func(document map[string]json.RawMessage, c Config) (bool, error) {
		models := make(map[string]json.RawMessage)
		_ = json.Unmarshal(document["models"], &models)
		if models == nil {
			models = make(map[string]json.RawMessage)
		}
		changed := false
		for name, model := range additions {
			if _, exists := c.Models[name]; exists {
				continue
			}
			data, err := json.Marshal(model)
			if err != nil {
				return false, err
			}
			models[name] = data
			changed = true
		}
		if changed {
			document["models"], _ = json.Marshal(models)
		}
		return changed, nil
	})
	return c.Models, err
}

// SaveDefaultModel persists the profile used by future conversations. Other
// profile settings, including reasoning defaults, are preserved.
func SaveDefaultModel(ctx context.Context, dir, name string) (Config, error) {
	return editConfig(ctx, dir, func(document map[string]json.RawMessage, c Config) (bool, error) {
		if _, ok := c.Models[name]; !ok {
			return false, fmt.Errorf("unknown model profile %q", name)
		}
		document["default_model"], _ = json.Marshal(name)
		return name != c.DefaultModel, nil
	})
}

// SaveReasoning persists a profile's reasoning default without changing the
// default model. Empty effort removes the option, letting the provider choose.
func SaveReasoning(ctx context.Context, dir, name, effort string) (Config, error) {
	return editConfig(ctx, dir, func(document map[string]json.RawMessage, c Config) (bool, error) {
		model, ok := c.Models[name]
		if !ok {
			return false, fmt.Errorf("unknown model profile %q", name)
		}
		if _, err := model.WithReasoningEffort(effort); err != nil {
			return false, err
		}
		var models map[string]json.RawMessage
		var profile map[string]json.RawMessage
		_ = json.Unmarshal(document["models"], &models)
		_ = json.Unmarshal(models[name], &profile)
		if effort == "" {
			delete(profile, "reasoning_effort")
		} else {
			profile["reasoning_effort"], _ = json.Marshal(effort)
		}
		models[name], _ = json.Marshal(profile)
		document["models"], _ = json.Marshal(models)
		return model.ReasoningEffort != effort, nil
	})
}

// editConfig serializes CPE writers, follows symlinks, and parses both sides of
// the edit before atomically replacing the file with its original permissions.
func editConfig(ctx context.Context, dir string, edit func(map[string]json.RawMessage, Config) (bool, error)) (Config, error) {
	path, err := filepath.EvalSymlinks(filepath.Join(dir, "config.json"))
	if err != nil {
		return Config{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lock := flock.New(path+".lock", flock.SetPermissions(0600))
	defer lock.Close()
	if held, err := lock.TryLockContext(ctx, 50*time.Millisecond); err != nil {
		return Config{}, err
	} else if !held {
		return Config{}, ctx.Err()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	c, err := parse(dir, data)
	if err != nil {
		return Config{}, err
	}
	var document map[string]json.RawMessage
	_ = json.Unmarshal(data, &document)
	changed := c.needsDefault
	if changed {
		document["default_model"], _ = json.Marshal(c.DefaultModel)
	}
	if edit != nil {
		edited, err := edit(document, c)
		if err != nil {
			return Config{}, err
		}
		changed = changed || edited
	}
	if !changed {
		return c, nil
	}
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return Config{}, err
	}
	c, err = parse(dir, data)
	if err != nil {
		return Config{}, err
	}
	if c.needsDefault {
		document["default_model"], _ = json.Marshal(c.DefaultModel)
		data, _ = json.MarshalIndent(document, "", "  ")
		c.needsDefault = false
	}
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, err
	}
	if !info.Mode().IsRegular() {
		return Config{}, errors.New("config.json must resolve to a regular file")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cpe-config-*.json")
	if err != nil {
		return Config{}, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(info.Mode().Perm()); err != nil {
		return Config{}, err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return Config{}, err
	}
	if err := f.Sync(); err != nil {
		return Config{}, err
	}
	if err := f.Close(); err != nil {
		return Config{}, err
	}
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return Config{}, err
	}
	return c, nil
}

package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Info is a read-only discovery snapshot, not a guarantee that replay will succeed.
type Info struct {
	Path    string
	Updated time.Time
	Label   string
}

// List finds sessions for cwd without locking or repairing their files. Discovery
// reads at most 1 MiB per file; a checkpoint label is optional. Open remains the
// authoritative format, lock, and replay boundary.
func List(ctx context.Context, dir, cwd string) ([]Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []Info
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.HasSuffix(entry.Name(), ".jsonl") || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, entry.Name())
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
		var e Entry
		var h header
		if decoder.Decode(&e) != nil || e.Type != sessionEntry || json.Unmarshal(e.Data, &h) != nil || h.CWD != cwd {
			_ = f.Close()
			continue
		}
		item := Info{Path: path, Updated: info.ModTime()}
		for decoder.Decode(&e) == nil {
			if err := ctx.Err(); err != nil {
				_ = f.Close()
				return nil, err
			}
			if e.Type == checkpointEntry {
				var label struct {
					Label string `json:"label"`
				}
				if json.Unmarshal(e.Data, &label) == nil {
					item.Label = label.Label
					break
				}
			}
		}
		_ = f.Close()
		result = append(result, item)
	}
	slices.SortFunc(result, func(a, b Info) int {
		if order := b.Updated.Compare(a.Updated); order != 0 {
			return order
		}
		return strings.Compare(a.Path, b.Path)
	})
	return result, ctx.Err()
}

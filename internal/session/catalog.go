package session

import (
	"bufio"
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
	Title   Title
}

// List finds sessions for cwd without locking or repairing their files. Discovery
// scans a fixed file snapshot with at most 1 MiB of buffering per record. Open remains the
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
		reader := bufio.NewReader(io.LimitReader(f, info.Size()))
		line, readErr := catalogLine(ctx, reader)
		var e Entry
		var h header
		if readErr != nil || json.Unmarshal(line, &e) != nil || e.Type != sessionEntry || json.Unmarshal(e.Data, &h) != nil || h.CWD != cwd {
			_ = f.Close()
			continue
		}
		item := Info{Path: path, Updated: info.ModTime()}
		for {
			line, err := catalogLine(ctx, reader)
			if err != nil {
				break
			}
			if json.Unmarshal(line, &e) != nil {
				continue
			}
			switch e.Type {
			case titleEntry:
				if title, err := decodeTitle(e.Data); err == nil {
					item.Title = title
				}
			case checkpointEntry:
				var label struct {
					Label string `json:"label"`
				}
				if item.Label == "" && json.Unmarshal(e.Data, &label) == nil {
					item.Label = label.Label
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

// Oversized records (for example image/tool results) are skipped, not truncated
// into valid-looking metadata. An incomplete trailing record is never published.
func catalogLine(ctx context.Context, reader *bufio.Reader) ([]byte, error) {
	var line []byte
	overflow := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		part, err := reader.ReadSlice('\n')
		if !overflow && len(line)+len(part) <= 1<<20 {
			line = append(line, part...)
		} else {
			overflow = true
			line = nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return line, err
	}
}

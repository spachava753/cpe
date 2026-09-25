package authstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gofrs/flock"

	"github.com/spachava753/cpe/internal/jsonconfig"
)

// Entry identifies one service in the shared auth.json document.
type Entry struct {
	Path, Service string
}

// Read returns a snapshot for local availability checks without locking or
// writing. Update is required when using or refreshing credentials.
func (e Entry) Read() (json.RawMessage, error) {
	document, err := readDocument(e.Path)
	return document[e.Service], err
}

// Update runs a read/refresh/save transaction. The callback receives nil for a
// missing entry and must parse credentials before using them. Returning nil
// leaves the entry unchanged.
// Callback errors abort the transaction. Callbacks must not reenter this store.
func (e Entry) Update(ctx context.Context, update func(json.RawMessage) (json.RawMessage, error)) error {
	return e.transact(ctx, update, ordinaryUpdate)
}

// Refresh preserves a returned token replacement even if ctx was canceled after
// the callback succeeded: the service may already have invalidated the old token.
// A nil replacement, callback error, or cancellation before acquiring the lock
// has the same behavior as Update. The caller still controls request cancellation.
func (e Entry) Refresh(ctx context.Context, update func(json.RawMessage) (json.RawMessage, error)) error {
	return e.transact(ctx, update, tokenRefresh)
}

type transactionKind uint8

const (
	ordinaryUpdate transactionKind = iota
	tokenRefresh
)

func (e Entry) transact(ctx context.Context, update func(json.RawMessage) (json.RawMessage, error), kind transactionKind) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lock, err := acquireLock(ctx, e.Path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	document, err := readDocument(e.Path)
	if err != nil {
		return err
	}
	next, err := update(document[e.Service])
	if err != nil {
		return err
	}
	if kind == tokenRefresh && next != nil {
		// This only detaches the short local commit, never the network exchange.
		ctx = context.WithoutCancel(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if next == nil {
		return nil
	}
	var fields map[string]json.RawMessage
	if jsonconfig.Decode(next, &fields) != nil {
		return errors.New("invalid credential entry; existing credentials were not overwritten")
	}
	document[e.Service] = next
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return errors.New("cannot encode auth.json")
	}
	return writeFile(ctx, e.Path, append(data, '\n'))
}

func readDocument(path string) (map[string]json.RawMessage, error) {
	document := map[string]json.RawMessage{}
	data, err := readFile(path)
	if err == nil {
		if jsonconfig.Decode(data, &document) != nil {
			return nil, errors.New("invalid auth.json; existing credentials were not overwritten")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return document, nil
}

func acquireLock(ctx context.Context, path string) (*flock.Flock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock := flock.New(path, flock.SetPermissions(0600))
	held, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil || !held {
		_ = lock.Close()
		if err == nil {
			err = ctx.Err()
		}
		return nil, err
	}
	return lock, nil
}

func readFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("credential file must be regular, not a symlink")
	}
	return os.ReadFile(path)
}

func writeFile(ctx context.Context, path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".cpe-auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

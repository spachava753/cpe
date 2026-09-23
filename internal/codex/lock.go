package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// CPE processes share a stable OS lock file. The kernel releases the lock on
// exit or crash; the file itself must remain in place to preserve its identity.
type fileLock struct{ lock *flock.Flock }

func acquireLock(ctx context.Context, path string) (*fileLock, error) {
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
		return nil, fmt.Errorf("lock CPE credentials: %w", err)
	}
	return &fileLock{lock: lock}, nil
}
func (l *fileLock) close() { _ = l.lock.Close() }

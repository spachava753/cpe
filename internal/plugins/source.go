package plugins

import (
	"context"
	"io/fs"
)

// SourceClient acquires a source tree. It owns source access, not plugin
// discovery, installation, or destination metadata.
type SourceClient interface {
	// Fetch returns a non-nil FS rooted at the acquired source. The caller
	// releases the checkout after using it. On failure, the client cleans up
	// any partially acquired resources.
	Fetch(ctx context.Context, source string) (Checkout, error)
}

// Checkout provides read access to a source tree for one operation. Paths are
// relative to FS and use io/fs conventions, not native filesystem paths.
type Checkout struct {
	FS fs.FS
	// Cleanup releases only resources owned by the source client, never
	// installed directories. Nil means no resources need to be released.
	Cleanup func() error
}

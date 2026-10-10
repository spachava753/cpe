// Package source fetches source trees and manages their acquisition resources.
package source

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Client fetches source trees using implementation-defined locators.
type Client interface {
	// Fetch returns a checkout with a non-nil FS that the caller must Close.
	// Implementations must release partially acquired resources on failure.
	Fetch(ctx context.Context, locator string) (Checkout, error)
}

// Checkout provides read access to a fetched tree. The caller must Close it
// after reading or copying the required files.
type Checkout struct {
	// FS is rooted at the fetched tree and uses io/fs-relative paths.
	FS fs.FS
	// GitTreeHash returns a Git tree object ID for a source-relative directory.
	// Nil means Git metadata is unavailable. "." selects the source root.
	GitTreeHash func(context.Context, string) (string, error)
	// Cleanup releases acquisition resources. Nil means no cleanup is needed.
	Cleanup func() error
}

// Close invokes Cleanup and returns its error, or nil if Cleanup is nil.
// Each call invokes Cleanup; callers should close an owned checkout once.
func (checkout Checkout) Close() error {
	if checkout.Cleanup != nil {
		return checkout.Cleanup()
	}
	return nil
}

// Acquire fetches locator through client and returns a checkout with a non-nil FS.
// The caller must Close a successful checkout. Nil clients, blank locators, canceled
// contexts, and unusable checkouts return an empty Checkout and an error.
// An unusable checkout is closed before returning, including any cleanup error.
func Acquire(ctx context.Context, client Client, locator string) (Checkout, error) {
	if err := ctx.Err(); err != nil {
		return Checkout{}, err
	}
	if client == nil {
		return Checkout{}, errors.New("source client is unavailable")
	}
	if strings.TrimSpace(locator) == "" {
		return Checkout{}, errors.New("source is required")
	}
	checkout, err := client.Fetch(
		ctx,
		locator,
	)
	if err != nil {
		return Checkout{}, fmt.Errorf(
			"fetch source: %w",
			err,
		)
	}
	if checkout.FS == nil {
		return Checkout{}, errors.Join(
			errors.New("source filesystem is unavailable"),
			checkout.Close(),
		)
	}
	return checkout, nil
}

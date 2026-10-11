// Package bundlefs publishes directory bundles with rollback and recovery support.
package bundlefs

import "io/fs"

// Entry describes a regular file or directory to create inside a bundle.
type Entry struct {
	// Path is an io/fs-relative bundle path. "." is allowed for directories.
	Path string
	// Data is the complete file content; directory entries require empty Data.
	Data []byte
	// Mode selects the file type and permissions. File permissions default to
	// 0644 when zero; new directories use 0700.
	Mode fs.FileMode
}

// Action selects a bundle installation, replacement, or removal.
type Action uint8

const (
	// Install creates a new bundle. An existing destination causes an error.
	Install Action = iota
	// Replace installs a complete payload, replacing an existing bundle or creating it.
	Replace
	// Remove deletes an existing bundle; an absent destination is a no-op.
	Remove
)

// Change describes a change to one named bundle beneath Apply's root.
// Names must be unique within an Apply call.
type Change struct {
	// Name is the child directory name and must satisfy ValidDirectoryName.
	Name string
	// Payload is the complete desired content. Remove requires an empty payload.
	Payload []Entry
	Action  Action
}

// Outcome reports whether Apply published all requested changes.
type Outcome uint8

const (
	// NotCommitted means changes were not fully published. Rollback may also
	// have failed; the error describes any retained recovery files.
	NotCommitted Outcome = iota
	// Committed means every change was published. An accompanying error reports
	// staging cleanup failure.
	Committed
)

package bundlefs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/afero"
)

// Apply stages and publishes changes to root/Name in filesystem, creating root
// when needed. Publication proceeds in slice order after all payloads are staged.
//
// checkExisting is required and must inspect an existing directory's eligibility
// before replacement or removal, returning an error to abort. root and callback
// paths use native syntax; Entry.Path uses io/fs slash paths. Inputs are checked
// before mutation.
//
// Cancellation or publication failure triggers rollback. Failed rollback retains
// recovery files identified in the error. Committed with an error means changes
// took effect but staging cleanup failed. NotCommitted may include failed rollback.
func Apply(ctx context.Context, filesystem afero.Fs, root string, changes []Change, checkExisting func(string) error) (Outcome, error) {
	return apply(
		ctx,
		filesystem,
		root,
		changes,
		checkExisting,
		nil,
	)
}

func apply(ctx context.Context, filesystem afero.Fs, root string, changes []Change, checkExisting func(string) error, file *FileChange) (outcome Outcome, err error) {
	if err := CheckDestination(
		ctx,
		filesystem,
		root,
	); err != nil {
		return NotCommitted, err
	}
	if checkExisting == nil {
		return NotCommitted, errors.New("existing bundle check is unavailable")
	}
	if err := checkChanges(changes); err != nil {
		return NotCommitted, err
	}
	if len(changes) == 0 {
		return Committed, nil
	}
	var metadata *stagedFile
	if file != nil && file.Data != nil {
		metadata, err = prepareFile(
			ctx,
			filesystem,
			*file,
		)
		if err != nil {
			return NotCommitted, err
		}
		defer func() {
			err = errors.Join(
				err,
				metadata.cleanup(),
			)
		}()
	}
	_, statErr := filesystem.Stat(root)
	created := errors.Is(
		statErr,
		fs.ErrNotExist,
	)
	if statErr != nil && !created {
		return NotCommitted, statErr
	}
	if err := filesystem.MkdirAll(
		root,
		0o700,
	); err != nil {
		return NotCommitted, err
	}
	stage, err := afero.TempDir(
		filesystem,
		root,
		".cpe-stage-",
	)
	if err != nil {
		if created {
			_ = filesystem.Remove(root)
		}
		return NotCommitted, err
	}
	preserveStage := false
	defer func() {
		if preserveStage {
			return
		}
		cleanupErr := filesystem.RemoveAll(stage)
		if outcome == Committed && cleanupErr != nil {
			err = fmt.Errorf(
				"bundle changes committed; cleanup failed for %s: %w",
				stage,
				cleanupErr,
			)
			return
		}
		if outcome != Committed && created {
			if removeErr := filesystem.Remove(root); removeErr != nil && !errors.Is(
				removeErr,
				fs.ErrNotExist,
			) {
				cleanupErr = errors.Join(
					cleanupErr,
					removeErr,
				)
			}
		}
		err = errors.Join(
			err,
			cleanupErr,
		)
	}()
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return NotCommitted, err
		}
		if change.Action == Remove {
			continue
		}
		newRoot := filepath.Join(
			stage,
			"new",
			change.Name,
		)
		if err := filesystem.MkdirAll(
			newRoot,
			0o700,
		); err != nil {
			return NotCommitted, err
		}
		for _, entry := range change.Payload {
			if err := ctx.Err(); err != nil {
				return NotCommitted, err
			}
			filename := filepath.Join(
				newRoot,
				filepath.FromSlash(entry.Path),
			)
			if entry.Mode.IsDir() {
				if err := filesystem.MkdirAll(
					filename,
					0o700,
				); err != nil {
					return NotCommitted, err
				}
				continue
			}
			if err := filesystem.MkdirAll(
				filepath.Dir(filename),
				0o700,
			); err != nil {
				return NotCommitted, err
			}
			mode := entry.Mode.Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := afero.WriteFile(
				filesystem,
				filename,
				entry.Data,
				mode,
			); err != nil {
				return NotCommitted, err
			}
		}
	}
	var moved, installed []string
	rollback := func(cause error) error {
		var restoreErr error
		for _, name := range slices.Backward(installed) {
			restoreErr = errors.Join(
				restoreErr,
				filesystem.RemoveAll(filepath.Join(
					root,
					name,
				)),
			)
		}
		for _, name := range slices.Backward(moved) {
			restoreErr = errors.Join(
				restoreErr,
				filesystem.Rename(
					filepath.Join(
						stage,
						"old",
						name,
					),
					filepath.Join(
						root,
						name,
					),
				),
			)
		}
		if restoreErr != nil {
			preserveStage = true
			return errors.Join(
				cause,
				fmt.Errorf(
					"rollback failed; recovery files retained at %s: %w",
					stage,
					restoreErr,
				),
			)
		}
		return cause
	}
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return NotCommitted, rollback(err)
		}
		filename := filepath.Join(
			root,
			change.Name,
		)
		info, statErr := Lstat(
			filesystem,
			filename,
		)
		if statErr == nil {
			if change.Action == Install {
				return NotCommitted, rollback(fmt.Errorf(
					"destination %s already exists: %w",
					filename,
					fs.ErrExist,
				))
			}
			if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
				return NotCommitted, rollback(fmt.Errorf(
					"destination %s is not a regular bundle directory",
					filename,
				))
			}
			if err := checkExisting(filename); err != nil {
				return NotCommitted, rollback(err)
			}
			if err := filesystem.MkdirAll(
				filepath.Join(
					stage,
					"old",
				),
				0o700,
			); err != nil {
				return NotCommitted, rollback(err)
			}
			if err := filesystem.Rename(
				filename,
				filepath.Join(
					stage,
					"old",
					change.Name,
				),
			); err != nil {
				return NotCommitted, rollback(err)
			}
			moved = append(
				moved,
				change.Name,
			)
		} else if !errors.Is(
			statErr,
			fs.ErrNotExist,
		) {
			return NotCommitted, rollback(statErr)
		}
		if change.Action != Remove {
			if err := filesystem.Rename(
				filepath.Join(
					stage,
					"new",
					change.Name,
				),
				filename,
			); err != nil {
				return NotCommitted, rollback(err)
			}
			installed = append(
				installed,
				change.Name,
			)
		}
	}
	if err := ctx.Err(); err != nil {
		return NotCommitted, rollback(err)
	}
	if metadata != nil {
		if err := metadata.publish(); err != nil {
			return NotCommitted, rollback(err)
		}
	}
	return Committed, nil
}

// Changes are prepared in memory by callers. Establish their filesystem
// invariants once, before creating any staging or destination directories.
func checkChanges(changes []Change) error {
	seen := map[string]bool{}
	for _, change := range changes {
		if !ValidDirectoryName(change.Name) || seen[change.Name] {
			return fmt.Errorf(
				"invalid or duplicate bundle directory name %q",
				change.Name,
			)
		}
		seen[change.Name] = true
		switch change.Action {
		case Install, Replace:
		case Remove:
			if len(change.Payload) != 0 {
				return errors.New("removal must not contain a payload")
			}
		default:
			return errors.New("unsupported bundle action")
		}
		for _, entry := range change.Payload {
			if !fs.ValidPath(entry.Path) || strings.ContainsAny(
				entry.Path,
				"\\\x00",
			) {
				return fmt.Errorf(
					"invalid payload path %q",
					entry.Path,
				)
			}
			if entry.Mode.IsDir() {
				if len(entry.Data) != 0 {
					return errors.New("directory payload must not contain file data")
				}
			} else if !entry.Mode.IsRegular() || entry.Path == "." {
				return fmt.Errorf(
					"payload %q must be a regular file or directory",
					entry.Path,
				)
			}
		}
	}
	return nil
}

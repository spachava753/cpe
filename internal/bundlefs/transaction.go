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
// paths use native syntax; Entry.Path uses io/fs slash paths. ReplaceExisting calls
// checkExisting on the captured old directory, restoring it if rejected. Inputs
// are checked before mutation.
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

func apply(ctx context.Context, filesystem afero.Fs, root string, changes []Change, checkExisting func(string) error, file *FileChange) (outcome Outcome, applyErr error) {
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
	if file != nil {
		metadata, applyErr = prepareFile(
			ctx,
			filesystem,
			*file,
		)
		if applyErr != nil {
			return NotCommitted, applyErr
		}
		defer func() {
			applyErr = errors.Join(
				applyErr,
				metadata.cleanup(),
			)
		}()
	}
	created, applyErr := missingDirectories(
		filesystem,
		root,
	)
	if applyErr != nil {
		return NotCommitted, applyErr
	}
	if err := filesystem.MkdirAll(
		root,
		0o700,
	); err != nil {
		return NotCommitted, errors.Join(
			err,
			removeDirectories(
				filesystem,
				created,
			),
		)
	}
	stage, applyErr := afero.TempDir(
		filesystem,
		root,
		".cpe-stage-",
	)
	if applyErr != nil {
		return NotCommitted, errors.Join(
			applyErr,
			removeDirectories(
				filesystem,
				created,
			),
		)
	}
	preserveStage := false
	defer func() {
		if preserveStage {
			return
		}
		cleanupErr := filesystem.RemoveAll(stage)
		if outcome == Committed && cleanupErr != nil {
			applyErr = fmt.Errorf(
				"bundle changes committed; cleanup failed for %s: %w",
				stage,
				cleanupErr,
			)
			return
		}
		if outcome != Committed {
			cleanupErr = errors.Join(
				cleanupErr,
				removeDirectories(
					filesystem,
					created,
				),
			)
		}
		applyErr = errors.Join(
			applyErr,
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
		restoreErr := metadata.rollback()
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
			if change.Action != ReplaceExisting {
				if err := checkExisting(filename); err != nil {
					return NotCommitted, rollback(err)
				}
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
			if change.Action == ReplaceExisting {
				captured := filepath.Join(
					stage,
					"old",
					change.Name,
				)
				info, err := Lstat(
					filesystem,
					captured,
				)
				if err != nil {
					return NotCommitted, rollback(err)
				}
				if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
					return NotCommitted, rollback(fmt.Errorf(
						"captured bundle %s is not a regular directory",
						captured,
					))
				}
				if err := checkExisting(captured); err != nil {
					return NotCommitted, rollback(err)
				}
			}
		} else if !errors.Is(
			statErr,
			fs.ErrNotExist,
		) {
			return NotCommitted, rollback(statErr)
		} else if change.Action == ReplaceExisting {
			return NotCommitted, rollback(fmt.Errorf(
				"destination %s no longer exists: %w",
				filename,
				fs.ErrNotExist,
			))
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
		if err := ctx.Err(); err != nil {
			return NotCommitted, rollback(err)
		}
	}
	return Committed, nil
}

func missingDirectories(filesystem afero.Fs, directory string) ([]string, error) {
	var missing []string
	for {
		if _, err := filesystem.Stat(directory); err == nil {
			return missing, nil
		} else if !errors.Is(
			err,
			fs.ErrNotExist,
		) {
			return nil, err
		}
		missing = append(
			missing,
			directory,
		)
		parent := filepath.Dir(directory)
		if parent == directory {
			return missing, nil
		}
		directory = parent
	}
}

func removeDirectories(filesystem afero.Fs, directories []string) error {
	var result error
	for _, directory := range directories {
		if err := filesystem.Remove(directory); err != nil && !errors.Is(
			err,
			fs.ErrNotExist,
		) {
			result = errors.Join(
				result,
				err,
			)
		}
	}
	return result
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
		case Install, Replace, ReplaceExisting:
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

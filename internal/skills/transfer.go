package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"

	"github.com/spf13/afero"
)

type payloadEntry struct {
	name      string
	data      []byte
	mode      fs.FileMode
	directory bool
}
type bundleAction uint8

const (
	installBundle bundleAction = iota
	replaceBundle
	removeBundle
)

type bundleChange struct {
	name    string
	payload []payloadEntry
	action  bundleAction
}

func readPayload(ctx context.Context, filesystem fs.FS, directory string) ([]payloadEntry, error) {
	if err := ensureSourceDirectory(
		filesystem,
		directory,
	); err != nil {
		return nil, err
	}
	var result []payloadEntry
	err := fs.WalkDir(
		filesystem,
		directory,
		func(filename string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			if filename != directory && entry.IsDir() && excludedDirectory(entry.Name()) {
				return fs.SkipDir
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				return fmt.Errorf(
					"payload symlink %s is not supported",
					filename,
				)
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf(
					"payload %s is not a regular file",
					filename,
				)
			}
			name := filename
			if directory != "." {
				if filename == directory {
					name = "."
				} else {
					name = filename[len(directory)+1:]
				}
			}
			if !fs.ValidPath(name) {
				return fmt.Errorf(
					"invalid payload path %q",
					name,
				)
			}
			item := payloadEntry{
				name:      name,
				mode:      info.Mode().Perm(),
				directory: info.IsDir(),
			}
			if !item.directory {
				item.data, err = fs.ReadFile(
					filesystem,
					filename,
				)
				if err != nil {
					return fmt.Errorf(
						"read payload %s: %w",
						filename,
						err,
					)
				}
			}
			result = append(
				result,
				item,
			)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// applyChanges stages every bundle before replacing any destination. Old bundles
// remain recoverable until every selected change has been published successfully.
// The outcome is separate from errors: cleanup can fail after changes commit.
func (m *Manager) applyChanges(ctx context.Context, root string, changes []bundleChange) (committed bool, err error) {
	if len(changes) == 0 {
		return true, nil
	}
	_, statErr := m.destinations.Stat(root)
	created := errors.Is(
		statErr,
		fs.ErrNotExist,
	)
	if statErr != nil && !created {
		return false, statErr
	}
	if err := m.destinations.MkdirAll(
		root,
		0o700,
	); err != nil {
		return false, err
	}
	stage, err := afero.TempDir(
		m.destinations,
		root,
		".cpe-stage-",
	)
	if err != nil {
		if created {
			_ = m.destinations.Remove(root)
		}
		return false, err
	}
	preserveStage := false
	defer func() {
		if preserveStage {
			return
		}
		cleanupErr := m.destinations.RemoveAll(stage)
		if committed && cleanupErr != nil {
			err = fmt.Errorf(
				"skills changes committed; cleanup failed for %s: %w",
				stage,
				cleanupErr,
			)
			return
		}
		if !committed && created {
			if removeErr := m.destinations.Remove(root); removeErr != nil && !errors.Is(
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
			return false, err
		}
		if change.action == removeBundle {
			continue
		}
		newRoot := filepath.Join(
			stage,
			"new",
			change.name,
		)
		if err := m.destinations.MkdirAll(
			newRoot,
			0o700,
		); err != nil {
			return false, err
		}
		for _, entry := range change.payload {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			filename := filepath.Join(
				newRoot,
				filepath.FromSlash(entry.name),
			)
			if entry.directory {
				if err := m.destinations.MkdirAll(
					filename,
					0o700,
				); err != nil {
					return false, err
				}
				continue
			}
			if err := m.destinations.MkdirAll(
				filepath.Dir(filename),
				0o700,
			); err != nil {
				return false, err
			}
			mode := entry.mode
			if mode == 0 {
				mode = 0o644
			}
			if err := afero.WriteFile(
				m.destinations,
				filename,
				entry.data,
				mode,
			); err != nil {
				return false, err
			}
		}
	}
	var moved, installed []string
	rollback := func(cause error) error {
		var restoreErr error
		for _, name := range slices.Backward(installed) {
			restoreErr = errors.Join(
				restoreErr,
				m.destinations.RemoveAll(filepath.Join(
					root,
					name,
				)),
			)
		}
		for _, name := range slices.Backward(moved) {
			restoreErr = errors.Join(
				restoreErr,
				m.destinations.Rename(
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
			return false, rollback(err)
		}
		filename := filepath.Join(
			root,
			change.name,
		)
		info, statErr := m.pathInfo(filename)
		if statErr == nil {
			if change.action == installBundle {
				return false, rollback(fmt.Errorf(
					"destination %s already exists: %w",
					filename,
					fs.ErrExist,
				))
			}
			if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
				return false, rollback(fmt.Errorf(
					"destination %s is not a regular bundle directory",
					filename,
				))
			}
			definition, err := m.pathInfo(filepath.Join(
				filename,
				"SKILL.md",
			))
			if err != nil {
				return false, rollback(err)
			}
			if !definition.Mode().IsRegular() {
				return false, rollback(fmt.Errorf(
					"destination %s must contain a regular SKILL.md",
					filename,
				))
			}
			if err := m.destinations.MkdirAll(
				filepath.Join(
					stage,
					"old",
				),
				0o700,
			); err != nil {
				return false, rollback(err)
			}
			if err := m.destinations.Rename(
				filename,
				filepath.Join(
					stage,
					"old",
					change.name,
				),
			); err != nil {
				return false, rollback(err)
			}
			moved = append(
				moved,
				change.name,
			)
		} else if !errors.Is(
			statErr,
			fs.ErrNotExist,
		) {
			return false, rollback(statErr)
		}
		if change.action != removeBundle {
			if err := m.destinations.Rename(
				filepath.Join(
					stage,
					"new",
					change.name,
				),
				filename,
			); err != nil {
				return false, rollback(err)
			}
			installed = append(
				installed,
				change.name,
			)
		}
	}
	if err := ctx.Err(); err != nil {
		return false, rollback(err)
	}
	return true, nil
}

func sourceDirectory(skill Skill) string { return path.Dir(skill.Path) }

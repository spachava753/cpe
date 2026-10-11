package bundlefs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spf13/afero"
)

// FileChange supplies a regular file to publish alongside bundle changes.
// Path uses native filesystem syntax. Non-nil Data replaces the complete contents;
// nil Data checks the snapshot without changing the file.
// Previous must match the existing bytes; nil requires the file to be absent.
type FileChange struct {
	Path     string
	Data     []byte
	Previous []byte
}

// ApplyWithFile publishes bundles and a file in one rollback transaction.
// It follows Apply's outcome contract. The file's parents are created as needed;
// symlinks and non-regular existing files are rejected. Empty changes leave the
// file untouched. Native filesystems retain a .cpe-lock sidecar to serialize
// publications; changed snapshots abort instead of overwriting another writer.
func ApplyWithFile(ctx context.Context, filesystem afero.Fs, root string, changes []Change, checkExisting func(string) error, file FileChange) (outcome Outcome, err error) {
	if file.Path == "" {
		return NotCommitted, errors.New("publication file path is required")
	}
	for _, change := range changes {
		bundle := filepath.Join(
			root,
			change.Name,
		)
		relative, err := filepath.Rel(
			bundle,
			file.Path,
		)
		if err == nil && filepath.IsLocal(relative) {
			return NotCommitted, errors.New("publication file must be outside changed bundles")
		}
	}
	if err := CheckDestination(
		ctx,
		filesystem,
		root,
	); err != nil {
		return NotCommitted, err
	}
	if err := checkFileParent(
		ctx,
		filesystem,
		file.Path,
	); err != nil {
		return NotCommitted, err
	}
	if err := checkChanges(changes); err != nil {
		return NotCommitted, err
	}
	if checkExisting == nil {
		return NotCommitted, errors.New("existing bundle check is unavailable")
	}
	if len(changes) == 0 {
		return Committed, nil
	}
	guard, err := lockFile(
		ctx,
		filesystem,
		file.Path,
		writerGuard,
	)
	if err != nil {
		return NotCommitted, err
	}
	defer func() {
		err = errors.Join(
			err,
			guard.close(),
		)
	}()
	return apply(
		ctx,
		filesystem,
		root,
		changes,
		checkExisting,
		&file,
	)
}

type stagedFile struct {
	filesystem        afero.Fs
	path              string
	newPath           string
	oldPath           string
	backupPath        string
	previous          []byte
	published         *publishedFile
	displacedPath     string
	preserveDisplaced bool
	preserve          bool
	created           []string
}

func prepareFile(ctx context.Context, filesystem afero.Fs, file FileChange) (_ *stagedFile, err error) {
	if err := checkFileParent(
		ctx,
		filesystem,
		file.Path,
	); err != nil {
		return nil, err
	}
	if err := checkFileSnapshot(
		filesystem,
		file.Path,
		file.Previous,
	); err != nil {
		return nil, err
	}
	staged := &stagedFile{
		filesystem: filesystem,
		path:       file.Path,
		previous:   file.Previous,
	}
	defer func() {
		if err != nil {
			err = errors.Join(
				err,
				staged.cleanup(),
			)
		}
	}()
	if file.Data == nil {
		return staged, nil
	}
	staged.created, err = missingDirectories(
		filesystem,
		filepath.Dir(file.Path),
	)
	if err != nil {
		return nil, err
	}
	if err := filesystem.MkdirAll(
		filepath.Dir(file.Path),
		0o700,
	); err != nil {
		return nil, err
	}
	temporary, err := afero.TempFile(
		filesystem,
		filepath.Dir(file.Path),
		".cpe-file-",
	)
	if err != nil {
		return nil, err
	}
	staged.newPath = temporary.Name()
	_, writeErr := temporary.Write(file.Data)
	if err := errors.Join(
		writeErr,
		temporary.Close(),
	); err != nil {
		return nil, err
	}
	if err := filesystem.Chmod(
		staged.newPath,
		0o644,
	); err != nil {
		return nil, err
	}
	return staged, nil
}

func checkFileSnapshot(filesystem afero.Fs, filename string, previous []byte) error {
	if err := checkRegularFile(
		filesystem,
		filename,
	); err != nil {
		return err
	}
	data, err := afero.ReadFile(
		filesystem,
		filename,
	)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) && previous == nil {
		return nil
	}
	if err != nil && !errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return err
	}
	if err != nil || previous == nil || !bytes.Equal(
		data,
		previous,
	) {
		return fmt.Errorf(
			"publication file %s changed since it was read; retry the operation",
			filename,
		)
	}
	return nil
}

func checkRegularFile(filesystem afero.Fs, filename string) error {
	info, err := Lstat(
		filesystem,
		filename,
	)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(
			"publication file %s is not a regular file",
			filename,
		)
	}
	return nil
}

func (file *stagedFile) publish() error {
	if file.newPath == "" {
		return checkFileSnapshot(
			file.filesystem,
			file.path,
			file.previous,
		)
	}
	if err := checkFileSnapshot(
		file.filesystem,
		file.path,
		file.previous,
	); err != nil {
		return err
	}
	if _, err := file.filesystem.Stat(file.path); err == nil {
		backup, err := afero.TempFile(
			file.filesystem,
			filepath.Dir(file.path),
			".cpe-backup-",
		)
		if err != nil {
			return err
		}
		file.backupPath = backup.Name()
		if err := errors.Join(
			backup.Close(),
			file.filesystem.Remove(file.backupPath),
		); err != nil {
			return err
		}
		if err := file.filesystem.Rename(
			file.path,
			file.backupPath,
		); err != nil {
			return err
		}
		file.oldPath = file.backupPath
		if err := checkFileSnapshot(
			file.filesystem,
			file.oldPath,
			file.previous,
		); err != nil {
			return fmt.Errorf(
				"publication file %s changed while publishing; retry the operation: %w",
				file.path,
				err,
			)
		}
	} else if !errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return err
	}
	var err error
	file.published, err = publishPreparedFile(
		file.filesystem,
		file.newPath,
		file.path,
	)
	return err
}

func (file *stagedFile) rollback() error {
	if file == nil {
		return nil
	}
	if file.published != nil {
		if err := file.withdrawPublished(); err != nil {
			file.preserve = true
			if file.oldPath != "" {
				return errors.Join(
					err,
					fmt.Errorf(
						"original recovery file retained at %s",
						file.oldPath,
					),
				)
			}
			return err
		}
		file.published = nil
	}
	if file.oldPath != "" {
		restored, err := publishPreparedFile(
			file.filesystem,
			file.oldPath,
			file.path,
		)
		if restored != nil && restored.complete() {
			file.oldPath = ""
			if err != nil {
				return fmt.Errorf(
					"file restored at %s; cleanup failed: %w",
					file.path,
					err,
				)
			}
		} else {
			file.preserve = true
			file.published = restored
			return errors.Join(
				fmt.Errorf(
					"file rollback failed; recovery file retained at %s",
					file.oldPath,
				),
				err,
			)
		}
	}
	return nil
}

// Capture the current file atomically before checking ownership. A replacement
// arriving between inspection and deletion must never be removed by rollback.
func (file *stagedFile) withdrawPublished() error {
	temporary, err := afero.TempFile(
		file.filesystem,
		filepath.Dir(file.path),
		".cpe-displaced-",
	)
	if err != nil {
		return fmt.Errorf(
			"file rollback could not inspect %s: %w",
			file.path,
			err,
		)
	}
	file.displacedPath = temporary.Name()
	if err := errors.Join(
		temporary.Close(),
		file.filesystem.Remove(file.displacedPath),
	); err != nil {
		return fmt.Errorf(
			"file rollback could not capture %s; published file remains at %s: %w",
			file.path,
			file.path,
			err,
		)
	}
	if err := file.filesystem.Rename(
		file.path,
		file.displacedPath,
	); err != nil {
		return fmt.Errorf(
			"file rollback could not capture %s: %w",
			file.path,
			err,
		)
	}
	file.preserveDisplaced = true
	info, err := Lstat(
		file.filesystem,
		file.displacedPath,
	)
	if err != nil {
		return fmt.Errorf(
			"file rollback could not inspect replacement; recovery file retained at %s: %w",
			file.displacedPath,
			err,
		)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(
			"file rollback captured a non-regular replacement; recovery file retained at %s",
			file.displacedPath,
		)
	}
	data, err := afero.ReadFile(
		file.filesystem,
		file.displacedPath,
	)
	if err != nil {
		return fmt.Errorf(
			"file rollback could not inspect replacement; recovery file retained at %s: %w",
			file.displacedPath,
			err,
		)
	}
	if !file.published.owns(
		info,
		data,
	) {
		restored, err := publishPreparedFile(
			file.filesystem,
			file.displacedPath,
			file.path,
		)
		if restored != nil && restored.complete() {
			file.preserveDisplaced = false
			return errors.Join(
				fmt.Errorf(
					"file rollback preserved replacement at %s",
					file.path,
				),
				err,
			)
		}
		return errors.Join(
			fmt.Errorf(
				"file rollback could not restore replacement; recovery file retained at %s",
				file.displacedPath,
			),
			err,
		)
	}
	if err := file.filesystem.Remove(file.displacedPath); err != nil {
		return fmt.Errorf(
			"file rollback failed; published file remains at %s: %w",
			file.displacedPath,
			err,
		)
	}
	file.displacedPath = ""
	file.preserveDisplaced = false
	return nil
}

func (file *stagedFile) cleanup() error {
	if file == nil {
		return nil
	}
	var err error
	for _, filename := range []string{
		file.newPath,
		file.backupPath,
		file.displacedPath,
	} {
		if filename == "" || (file.preserve && filename == file.backupPath) || (file.preserveDisplaced && filename == file.displacedPath) {
			continue
		}
		if removeErr := file.filesystem.Remove(filename); removeErr != nil && !errors.Is(
			removeErr,
			fs.ErrNotExist,
		) {
			err = errors.Join(
				err,
				fmt.Errorf(
					"cleanup file %s: %w",
					filename,
					removeErr,
				),
			)
		}
	}
	if file.published == nil && !file.preserve {
		err = errors.Join(
			err,
			removeDirectories(
				file.filesystem,
				file.created,
			),
		)
	}
	return err
}

package bundlefs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spf13/afero"
)

// FileChange supplies a regular file to publish alongside bundle changes.
// Path uses native filesystem syntax; Data replaces the complete file contents.
type FileChange struct {
	Path string
	Data []byte
}

// ApplyWithFile publishes bundles and a file in one rollback transaction.
// It follows Apply's outcome contract. The file's parents are created as needed;
// symlinks and non-regular existing files are rejected. Empty changes leave the
// file untouched.
func ApplyWithFile(ctx context.Context, filesystem afero.Fs, root string, changes []Change, checkExisting func(string) error, file FileChange) (Outcome, error) {
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
	filesystem afero.Fs
	path       string
	newPath    string
	oldPath    string
	backupPath string
	published  bool
	preserve   bool
	created    []string
}

func prepareFile(ctx context.Context, filesystem afero.Fs, file FileChange) (_ *stagedFile, err error) {
	if err := CheckDestination(
		ctx,
		filesystem,
		filepath.Dir(file.Path),
	); err != nil {
		return nil, err
	}
	if err := checkRegularFile(
		filesystem,
		file.Path,
	); err != nil {
		return nil, err
	}
	staged := &stagedFile{
		filesystem: filesystem,
		path:       file.Path,
	}
	defer func() {
		if err != nil {
			err = errors.Join(
				err,
				staged.cleanup(),
			)
		}
	}()
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
	if err := checkRegularFile(
		file.filesystem,
		file.path,
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
	} else if !errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return err
	}
	if err := file.filesystem.Rename(
		file.newPath,
		file.path,
	); err != nil {
		return err
	}
	file.newPath = ""
	file.published = true
	return nil
}

func (file *stagedFile) rollback() error {
	if file == nil {
		return nil
	}
	if file.published {
		if err := file.filesystem.Remove(file.path); err != nil {
			file.preserve = true
			return fmt.Errorf(
				"file rollback failed; recovery file retained at %s: %w",
				file.oldPath,
				err,
			)
		}
		file.published = false
	}
	if file.oldPath != "" {
		if err := file.filesystem.Rename(
			file.oldPath,
			file.path,
		); err != nil {
			file.preserve = true
			return fmt.Errorf(
				"file rollback failed; recovery file retained at %s: %w",
				file.oldPath,
				err,
			)
		}
		file.oldPath = ""
	}
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
	} {
		if filename == "" || (file.preserve && filename == file.backupPath) {
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
	if !file.published && !file.preserve {
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

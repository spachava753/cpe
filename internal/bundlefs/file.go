package bundlefs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spf13/afero"
)

// FileChange supplies tracking bytes to publish after bundle changes.
// Nil Data leaves the file untouched. Path uses native filesystem syntax.
type FileChange struct {
	Path string
	Data []byte
}

// ReadFile reads a regular file, returning nil when it does not exist.
func ReadFile(ctx context.Context, filesystem afero.Fs, filename string) ([]byte, error) {
	if err := checkFile(
		ctx,
		filesystem,
		filename,
	); err != nil {
		return nil, err
	}
	data, err := afero.ReadFile(
		filesystem,
		filename,
	)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return nil, nil
	}
	return data, err
}

// ApplyWithFile publishes tracking bytes by rename after applying bundle changes.
// Failure before that rename rolls back bundles; concurrent writers are unsupported.
func ApplyWithFile(ctx context.Context, filesystem afero.Fs, root string, changes []Change, checkExisting func(string) error, file FileChange) (Outcome, error) {
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
	temporary  string
}

func prepareFile(ctx context.Context, filesystem afero.Fs, file FileChange) (*stagedFile, error) {
	if err := checkFile(
		ctx,
		filesystem,
		file.Path,
	); err != nil {
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
	staged := &stagedFile{
		filesystem: filesystem,
		path:       file.Path,
		temporary:  temporary.Name(),
	}
	_, writeErr := temporary.Write(file.Data)
	err = errors.Join(
		writeErr,
		temporary.Close(),
	)
	if err == nil {
		err = filesystem.Chmod(
			staged.temporary,
			0o644,
		)
	}
	if err != nil {
		return nil, errors.Join(
			err,
			staged.cleanup(),
		)
	}
	return staged, nil
}

func checkFile(ctx context.Context, filesystem afero.Fs, filename string) error {
	parent, name := filepath.Split(filename)
	if name == "" || name == "." || name == ".." {
		return errors.New("file path must name a file")
	}
	if parent == "" {
		parent = "."
	}
	if err := CheckDestination(
		ctx,
		filesystem,
		parent,
	); err != nil {
		return err
	}
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
			"file %s is not regular",
			filename,
		)
	}
	return nil
}

func (file *stagedFile) publish() error {
	return file.filesystem.Rename(
		file.temporary,
		file.path,
	)
}

func (file *stagedFile) cleanup() error {
	err := file.filesystem.Remove(file.temporary)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return nil
	}
	return err
}

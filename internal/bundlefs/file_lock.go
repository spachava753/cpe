package bundlefs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// ReadFile reads a regular file without overlapping a tracked publication.
// Parent symlinks are rejected before path cleaning. Missing files return nil
// data; empty files return a non-nil empty slice. Native filesystems retain a
// .cpe-lock sidecar.
func ReadFile(ctx context.Context, filesystem afero.Fs, filename string) (data []byte, err error) {
	if err := checkFileParent(
		ctx,
		filesystem,
		filename,
	); err != nil {
		return nil, err
	}
	guard, err := lockFile(
		ctx,
		filesystem,
		filename,
		readerGuard,
	)
	if err != nil {
		if errors.Is(
			err,
			fs.ErrPermission,
		) {
			if _, fileErr := Lstat(
				filesystem,
				filename,
			); errors.Is(
				fileErr,
				fs.ErrNotExist,
			) {
				if _, guardErr := Lstat(
					filesystem,
					filename+".cpe-lock",
				); errors.Is(
					guardErr,
					fs.ErrNotExist,
				) {
					return nil, nil
				}
			}
		}
		return nil, err
	}
	defer func() {
		err = errors.Join(
			err,
			guard.close(),
		)
	}()
	if err := checkRegularFile(
		filesystem,
		filename,
	); err != nil {
		return nil, err
	}
	data, err = afero.ReadFile(
		filesystem,
		filename,
	)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return nil, nil
	}
	if err == nil && data == nil {
		data = []byte{}
	}
	return data, err
}

type fileGuard struct {
	filesystem afero.Fs
	file       afero.File
	native     bool
	unlock     func() error
	created    []string
}

func fileParent(filename string) (string, error) {
	if strings.TrimSpace(filename) == "" {
		return "", errors.New("file path is required")
	}
	filename = filepath.FromSlash(filename)
	index := strings.LastIndexByte(
		filename,
		byte(filepath.Separator),
	)
	name := filename[index+1:]
	if name == "" || name == "." || name == ".." {
		return "", errors.New("file path must name a file")
	}
	if index >= 0 {
		// Retain every component and the trailing separator for safety checks.
		return filename[:index+1], nil
	}
	if volume := filepath.VolumeName(filename); volume != "" {
		return volume, nil
	}
	return ".", nil
}

func checkFileParent(ctx context.Context, filesystem afero.Fs, filename string) error {
	parent, err := fileParent(filename)
	if err != nil {
		return err
	}
	return CheckDestination(
		ctx,
		filesystem,
		parent,
	)
}

type guardAccess uint8

const (
	readerGuard guardAccess = iota
	writerGuard
)

func lockFile(ctx context.Context, filesystem afero.Fs, filename string, access guardAccess) (_ *fileGuard, err error) {
	if err := checkFileParent(
		ctx,
		filesystem,
		filename,
	); err != nil {
		return nil, err
	}
	lockPath := filename + ".cpe-lock"
	if err := checkRegularFile(
		filesystem,
		lockPath,
	); err != nil {
		return nil, err
	}
	guard := &fileGuard{
		filesystem: filesystem,
	}
	defer func() {
		if err != nil {
			err = errors.Join(
				err,
				guard.close(),
			)
		}
	}()
	if access == readerGuard {
		file, openErr := filesystem.Open(lockPath)
		if openErr == nil {
			if _, native := nativeDescriptor(file); !native {
				_ = file.Close()
				return nil, fmt.Errorf(
					"another operation is publishing %s; retry the operation",
					filename,
				)
			}
			guard.file = file
		} else if !errors.Is(
			openErr,
			fs.ErrNotExist,
		) {
			return nil, openErr
		}
	}
	if guard.file == nil {
		guard.created, err = missingDirectories(
			filesystem,
			filepath.Dir(filename),
		)
		if err != nil {
			return nil, err
		}
		if len(guard.created) != 0 {
			if err := filesystem.MkdirAll(
				filepath.Dir(filename),
				0o700,
			); err != nil {
				return nil, err
			}
		}
		guard.file, err = filesystem.OpenFile(
			lockPath,
			os.O_RDWR|os.O_CREATE|os.O_EXCL,
			0o600,
		)
		if errors.Is(
			err,
			fs.ErrExist,
		) {
			// Native sidecars are stable inodes shared by every publishing process.
			// In-memory filesystems use exclusive creation instead of native locks.
			flags := os.O_RDONLY
			if access == writerGuard {
				flags = os.O_RDWR
			}
			file, openErr := filesystem.OpenFile(
				lockPath,
				flags,
				0o600,
			)
			if openErr != nil {
				return nil, openErr
			}
			if _, native := nativeDescriptor(file); !native {
				_ = file.Close()
				return nil, fmt.Errorf(
					"another operation is publishing %s; retry the operation",
					filename,
				)
			}
			guard.file = file
			err = nil
		}
		if err != nil {
			return nil, err
		}
	}
	descriptor, native := nativeDescriptor(guard.file)
	guard.native = native
	if !guard.native {
		return guard, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		locked, err := tryFileLock(descriptor)
		if err != nil {
			return nil, fmt.Errorf(
				"lock publication file %s: %w",
				filename,
				err,
			)
		}
		if locked {
			guard.unlock = func() error { return unlockFile(descriptor) }
			return guard, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func nativeDescriptor(file afero.File) (uintptr, bool) {
	if native, ok := file.(interface{ Fd() uintptr }); ok {
		return native.Fd(), true
	}
	if base, ok := file.(*afero.BasePathFile); ok {
		return nativeDescriptor(base.File)
	}
	return 0, false
}

func (guard *fileGuard) close() error {
	var err error
	if guard.file != nil {
		if guard.unlock != nil {
			err = guard.unlock()
		}
		err = errors.Join(
			err,
			guard.file.Close(),
		)
		if !guard.native {
			err = errors.Join(
				err,
				guard.filesystem.Remove(guard.file.Name()),
			)
		}
	}
	for _, directory := range guard.created {
		entries, readErr := afero.ReadDir(
			guard.filesystem,
			directory,
		)
		if errors.Is(
			readErr,
			fs.ErrNotExist,
		) {
			continue
		}
		if readErr != nil || len(entries) != 0 {
			return errors.Join(
				err,
				readErr,
			)
		}
		err = errors.Join(
			err,
			guard.filesystem.Remove(directory),
		)
	}
	return err
}

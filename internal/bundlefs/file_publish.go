package bundlefs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/spf13/afero/mem"
)

type publishedFile struct {
	info     fs.FileInfo
	identity fs.FileInfo
	data     []byte
}

func (file *publishedFile) complete() bool {
	return int64(len(file.data)) == file.info.Size()
}

func (file *publishedFile) owns(info fs.FileInfo, data []byte) bool {
	return samePublicationFile(
		file.identity,
		info,
	) && bytes.Equal(
		file.data,
		data,
	)
}

func samePublicationFile(previous fs.FileInfo, current fs.FileInfo) bool {
	if previous == nil || current == nil {
		return false
	}
	if previous, ok := previous.(*mem.FileInfo); ok {
		current, ok := current.(*mem.FileInfo)
		return ok && previous.FileData == current.FileData
	}
	return os.SameFile(
		previous,
		current,
	)
}

// Native publication installs complete staged bytes without replacing a file
// that appeared meanwhile. Other injected filesystems reserve the destination
// exclusively; cooperating readers are protected by the publication guard.
func publishPreparedFile(filesystem afero.Fs, staged string, filename string) (published *publishedFile, err error) {
	info, err := Lstat(
		filesystem,
		staged,
	)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf(
			"staged publication file %s is not regular",
			staged,
		)
	}
	source, err := filesystem.Open(staged)
	if err != nil {
		return nil, err
	}
	// Handle-based Stat eagerly captures Windows file IDs before MoveFile
	// consumes the staged pathname; pathname-based Lstat resolves IDs lazily.
	info, statErr := source.Stat()
	data, readErr := io.ReadAll(source)
	if err := errors.Join(
		statErr,
		readErr,
		source.Close(),
	); err != nil {
		return nil, err
	}
	directory, err := filesystem.Open(filepath.Dir(filename))
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(
			err,
			directory.Close(),
		)
	}()
	if descriptor, native := nativeDescriptor(directory); native {
		if err := publishNativeFile(
			descriptor,
			filepath.Base(staged),
			filepath.Base(filename),
		); err != nil {
			return nil, err
		}
		return &publishedFile{
			info:     info,
			identity: info,
			data:     data,
		}, nil
	}
	output, err := filesystem.OpenFile(
		filename,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o644,
	)
	if err != nil {
		return nil, err
	}
	identity, statErr := output.Stat()
	if statErr != nil {
		return &publishedFile{
			info: info,
		}, errors.Join(
			statErr,
			output.Close(),
		)
	}
	written, copyErr := io.Copy(
		output,
		bytes.NewReader(data),
	)
	return &publishedFile{
		info:     info,
		identity: identity,
		data:     data[:written],
	}, errors.Join(
		copyErr,
		output.Close(),
	)
}

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
)

type publishedFile struct {
	info   fs.FileInfo
	data   []byte
	native bool
}

func (file *publishedFile) complete() bool {
	return int64(len(file.data)) == file.info.Size()
}

func (file *publishedFile) owns(info fs.FileInfo, data []byte) bool {
	return (!file.native || os.SameFile(
		file.info,
		info,
	)) && bytes.Equal(
		file.data,
		data,
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
	data, err := afero.ReadFile(
		filesystem,
		staged,
	)
	if err != nil {
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
			info:   info,
			data:   data,
			native: true,
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
	written, copyErr := io.Copy(
		output,
		bytes.NewReader(data),
	)
	return &publishedFile{
		info: info,
		data: data[:written],
	}, errors.Join(
		copyErr,
		output.Close(),
	)
}

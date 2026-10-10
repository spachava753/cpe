package bundlefs

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// Native publication installs complete staged bytes without replacing a file
// that appeared meanwhile. Other injected filesystems reserve the destination
// exclusively; cooperating readers are protected by the publication guard.
func publishPreparedFile(filesystem afero.Fs, staged string, filename string) (published bool, err error) {
	directory, err := filesystem.Open(filepath.Dir(filename))
	if err != nil {
		return false, err
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
			return false, err
		}
		return true, nil
	}
	input, err := filesystem.Open(staged)
	if err != nil {
		return false, err
	}
	defer func() {
		err = errors.Join(
			err,
			input.Close(),
		)
	}()
	output, err := filesystem.OpenFile(
		filename,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o644,
	)
	if err != nil {
		return false, err
	}
	_, copyErr := io.Copy(
		output,
		input,
	)
	return true, errors.Join(
		copyErr,
		output.Close(),
	)
}

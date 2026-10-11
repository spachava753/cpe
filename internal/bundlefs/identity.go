package bundlefs

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/spf13/afero"
)

// SameDirectory reports whether existing paths identify one directory in filesystem.
// Missing paths and non-directories compare unequal; unavailable identity is an error.
func SameDirectory(filesystem afero.Fs, first string, second string) (bool, error) {
	if filesystem == nil {
		return false, errors.New("directory filesystem is unavailable")
	}
	firstInfo, err := directoryIdentity(
		filesystem,
		first,
	)
	if err != nil {
		return false, err
	}
	secondInfo, err := directoryIdentity(
		filesystem,
		second,
	)
	if err != nil {
		return false, err
	}
	return samePublicationFile(
		firstInfo,
		secondInfo,
	), nil
}

func directoryIdentity(filesystem afero.Fs, filename string) (_ fs.FileInfo, err error) {
	file, err := filesystem.Open(filename)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(
			err,
			file.Close(),
		)
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	if !samePublicationFile(
		info,
		info,
	) {
		return nil, fmt.Errorf(
			"filesystem does not expose a stable directory identity for %s",
			filename,
		)
	}
	return info, nil
}

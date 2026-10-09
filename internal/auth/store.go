package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unicode/utf8"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

const maxCredentialFile = 2 * maxAuthResponse

// Store retrieves and saves provider credentials using an injected filesystem.
// Reuse one Store for concurrent access to backends without native file descriptors.
type Store struct {
	filesystem afero.Fs
	gate       chan struct{}
}

// NewStore uses filesystem for all credential file operations.
// Operations return an error if filesystem is nil.
func NewStore(filesystem afero.Fs) *Store {
	return &Store{
		filesystem: filesystem,
		gate: make(
			chan struct{},
			1,
		),
	}
}

// Retrieve reads filename and returns its provider entries. An absent file returns empty credentials.
// Invalid or unsafe files return empty credentials and an error. filename uses native path syntax.
func (store *Store) Retrieve(ctx context.Context, filename string) (Credentials, error) {
	if err := store.configured(filename); err != nil {
		return Credentials{}, err
	}
	if err := store.enter(ctx); err != nil {
		return Credentials{}, err
	}
	defer store.leave()
	if err := checkCredentialDirectory(
		ctx,
		store.filesystem,
		filename,
		false,
	); err != nil {
		return Credentials{}, err
	}
	return readCredentials(
		store.filesystem,
		filename,
	)
}

// Save replaces provider's entry in filename with the corresponding entry from credentials.
// A nil entry removes that provider. Other entries are preserved; failed updates leave the file unchanged.
func (store *Store) Save(ctx context.Context, filename string, provider Provider, credentials Credentials) error {
	if err := store.configured(filename); err != nil {
		return err
	}
	if _, err := provider.name(); err != nil {
		return err
	}
	if err := credentials.check(); err != nil {
		return err
	}
	if err := store.enter(ctx); err != nil {
		return err
	}
	defer store.leave()
	removing := (provider == OpenAI && credentials.OpenAI == nil) || (provider == OpenCode && credentials.OpenCode == nil)
	if err := checkCredentialDirectory(
		ctx,
		store.filesystem,
		filename,
		!removing,
	); err != nil {
		return err
	}
	if removing {
		if _, err := bundlefs.Lstat(
			store.filesystem,
			filename,
		); errors.Is(
			err,
			fs.ErrNotExist,
		) {
			return nil
		}
	}
	lock, err := lockCredentials(
		ctx,
		store.filesystem,
		filename,
	)
	if err != nil {
		return err
	}
	defer closeCredentialLock(lock)
	document, err := readCredentials(
		store.filesystem,
		filename,
	)
	if err != nil {
		return err
	}
	switch provider {
	case OpenAI:
		if removing && document.OpenAI == nil {
			return nil
		}
		document.OpenAI = credentials.OpenAI
	case OpenCode:
		if removing && document.OpenCode == nil {
			return nil
		}
		document.OpenCode = credentials.OpenCode
	}
	return writeCredentials(
		ctx,
		store.filesystem,
		filename,
		document,
	)
}

func (store *Store) configured(filename string) error {
	if store == nil || store.filesystem == nil || store.gate == nil {
		return errors.New("authentication store requires a filesystem")
	}
	if filename == "" || filepath.Base(filename) == "." || filepath.Base(filename) == string(filepath.Separator) {
		return errors.New("authentication file path is required")
	}
	return nil
}

func (store *Store) enter(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case store.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (store *Store) leave() {
	<-store.gate
}

func checkCredentialDirectory(ctx context.Context, filesystem afero.Fs, filename string, create bool) error {
	directory := filepath.Dir(filename)
	// Check the raw parent components, including link/.., before path cleaning.
	separator := stringsLastSeparator(filename)
	if separator >= 0 {
		directory = filename[:separator+1]
	}
	if err := bundlefs.CheckDestination(
		ctx,
		filesystem,
		directory,
	); err != nil {
		return fmt.Errorf(
			"check credential directory: %w",
			err,
		)
	}
	if create {
		if err := filesystem.MkdirAll(
			directory,
			0o700,
		); err != nil {
			return fmt.Errorf(
				"create credential directory: %w",
				err,
			)
		}
	}
	info, err := filesystem.Stat(directory)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) && !create {
		return nil
	}
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 {
		return errors.New("credential directory must not be writable by other users")
	}
	return nil
}

func stringsLastSeparator(filename string) int {
	for index := len(filename) - 1; index >= 0; index-- {
		if os.IsPathSeparator(filename[index]) {
			return index
		}
	}
	return -1
}

func lockCredentials(ctx context.Context, filesystem afero.Fs, filename string) (afero.File, error) {
	lockPath := filename + ".lock"
	if err := checkCredentialFile(
		filesystem,
		lockPath,
	); err != nil {
		return nil, err
	}
	file, err := filesystem.OpenFile(
		lockPath,
		os.O_RDWR|os.O_CREATE,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open credential lock: %w",
			err,
		)
	}
	if descriptor, ok := credentialDescriptor(file); ok {
		for {
			locked, err := tryCredentialLock(descriptor)
			if err != nil {
				_ = file.Close()
				return nil, errors.New("lock credential file")
			}
			if locked {
				break
			}
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				_ = file.Close()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return file, nil
}

func credentialDescriptor(file afero.File) (uintptr, bool) {
	if file, ok := file.(interface{ Fd() uintptr }); ok {
		return file.Fd(), true
	}
	if file, ok := file.(*afero.BasePathFile); ok {
		return credentialDescriptor(file.File)
	}
	return 0, false
}

func closeCredentialLock(file afero.File) {
	if descriptor, ok := credentialDescriptor(file); ok {
		_ = unlockCredentialFile(descriptor)
	}
	_ = file.Close()
}

func readCredentials(filesystem afero.Fs, filename string) (Credentials, error) {
	if err := checkCredentialFile(
		filesystem,
		filename,
	); err != nil {
		return Credentials{}, err
	}
	file, err := filesystem.Open(filename)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, fmt.Errorf(
			"read credentials: %w",
			err,
		)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(
		file,
		maxCredentialFile+1,
	))
	if err != nil {
		return Credentials{}, errors.New("read credential file")
	}
	defer clear(data)
	if len(data) > maxCredentialFile || !utf8.Valid(data) || bytes.Equal(
		bytes.TrimSpace(data),
		[]byte("null"),
	) {
		return Credentials{}, errors.New("invalid credential file")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document Credentials
	if err := decoder.Decode(&document); err != nil {
		return Credentials{}, errors.New("invalid credential file")
	}
	if _, err := decoder.Token(); !errors.Is(
		err,
		io.EOF,
	) {
		return Credentials{}, errors.New("invalid credential file")
	}
	if err := document.check(); err != nil {
		return Credentials{}, err
	}
	return document, nil
}

func writeCredentials(ctx context.Context, filesystem afero.Fs, filename string, document Credentials) error {
	data, err := json.Marshal(document)
	if err != nil {
		return errors.New("encode credentials")
	}
	data = append(
		data,
		'\n',
	)
	defer clear(data)
	if len(data) > maxCredentialFile {
		return errors.New("credential file exceeds the size limit")
	}
	// Keep the raw parent path so staging and publication refer to the same directory.
	temporary := filename + ".stage-" + rand.Text()
	file, err := filesystem.OpenFile(
		temporary,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return fmt.Errorf(
			"stage credentials: %w",
			err,
		)
	}
	defer func() {
		_ = file.Close()
		_ = filesystem.Remove(temporary)
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf(
			"write credentials: %w",
			err,
		)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf(
			"sync credentials: %w",
			err,
		)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf(
			"close credentials: %w",
			err,
		)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkCredentialFile(
		filesystem,
		filename,
	); err != nil {
		return err
	}
	if err := filesystem.Rename(
		temporary,
		filename,
	); err != nil {
		return fmt.Errorf(
			"publish credentials: %w",
			err,
		)
	}
	return nil
}

func checkCredentialFile(filesystem afero.Fs, filename string) error {
	info, err := bundlefs.Lstat(
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
		return fmt.Errorf(
			"inspect credentials: %w",
			err,
		)
	}
	if !info.Mode().IsRegular() {
		return errors.New("credential file must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return errors.New("credential files must be private (0600)")
	}
	return nil
}

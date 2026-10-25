package auth

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
)

func TestWriteCredentials(t *testing.T) {
	for _, test := range []struct {
		name  string
		phase string
	}{
		{
			name:  "stage creation fails",
			phase: "open",
		},
		{
			name:  "write fails",
			phase: "write",
		},
		{
			name:  "sync fails",
			phase: "sync",
		},
		{
			name:  "close fails",
			phase: "close",
		},
		{
			name:  "rename fails",
			phase: "rename",
		},
		{
			name:  "canceled after sync",
			phase: "cancel",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				memory := afero.NewMemMapFs()
				if err := afero.WriteFile(
					memory,
					credentialFile,
					[]byte(testAuthDocument),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				filesystem := &credentialFaultFS{
					Fs:     memory,
					phase:  test.phase,
					cancel: cancel,
				}
				err := writeCredentials(
					ctx,
					filesystem,
					credentialFile,
					Credentials{
						OpenCode: &APIKeyCredentials{
							APIKey: "new-key",
						},
					},
				)
				if err == nil {
					t.Fatal("missing filesystem error")
				}
				data, err := afero.ReadFile(
					memory,
					credentialFile,
				)
				if err != nil || !bytes.Equal(
					data,
					[]byte(testAuthDocument),
				) {
					t.Error("failed publication changed credentials")
				}
				entries, err := afero.ReadDir(
					memory,
					".",
				)
				if err != nil || len(entries) != 1 {
					t.Error("failed publication left staging files")
				}
			},
		)
	}
}

func TestStoreMemoryRoundTrip(t *testing.T) {
	filesystem := afero.NewMemMapFs()
	store := NewStore(filesystem)
	path := filepath.Join(
		"settings",
		"custom-auth.json",
	)
	credentials := Credentials{
		OpenCode: &APIKeyCredentials{
			APIKey: "synthetic-key",
		},
	}
	if err := store.Save(
		t.Context(),
		path,
		OpenCode,
		credentials,
	); err != nil {
		t.Fatal(err)
	}
	got, retrieveErr := store.Retrieve(
		t.Context(),
		path,
	)
	if retrieveErr != nil || got.OpenCode == nil || got.OpenCode.APIKey != "synthetic-key" {
		t.Fatal("injected filesystem did not retain credentials")
	}
	if _, err := filesystem.Stat(credentialFile); !errors.Is(
		err,
		fs.ErrNotExist,
	) {
		t.Fatal("store substituted a default filename")
	}
	if err := store.Save(
		t.Context(),
		path,
		OpenCode,
		Credentials{},
	); err != nil {
		t.Fatal(err)
	}
	got, retrieveErr = store.Retrieve(
		t.Context(),
		path,
	)
	if retrieveErr != nil || got.OpenAI != nil || got.OpenCode != nil {
		t.Fatal("injected filesystem did not remove credentials")
	}
}

func TestStoreGateCancellation(t *testing.T) {
	for _, operation := range []string{
		"retrieve",
		"save",
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				store := NewStore(filesystem)
				if err := store.enter(t.Context()); err != nil {
					t.Fatal(err)
				}
				defer store.leave()
				ctx, cancel := context.WithTimeout(
					t.Context(),
					10*time.Millisecond,
				)
				defer cancel()
				var err error
				if operation == "retrieve" {
					_, err = store.Retrieve(
						ctx,
						credentialFile,
					)
				} else {
					err = store.Save(
						ctx,
						credentialFile,
						OpenCode,
						Credentials{
							OpenCode: &APIKeyCredentials{
								APIKey: "synthetic-key",
							},
						},
					)
				}
				if !errors.Is(
					err,
					context.DeadlineExceeded,
				) {
					t.Errorf(
						"gate wait ignored cancellation: %v",
						err,
					)
				}
			},
		)
	}
}

type credentialFaultFS struct {
	afero.Fs
	cancel context.CancelFunc
	phase  string
}

func (filesystem *credentialFaultFS) OpenFile(name string, flags int, mode fs.FileMode) (afero.File, error) {
	if !strings.Contains(
		name,
		".stage-",
	) {
		return filesystem.Fs.OpenFile(
			name,
			flags,
			mode,
		)
	}
	if filesystem.phase == "open" {
		return nil, errors.New("injected stage failure")
	}
	file, err := filesystem.Fs.OpenFile(
		name,
		flags,
		mode,
	)
	if err != nil {
		return nil, err
	}
	return &credentialFaultFile{
		File:   file,
		phase:  filesystem.phase,
		cancel: filesystem.cancel,
	}, nil
}

func (filesystem *credentialFaultFS) Rename(oldName, newName string) error {
	if filesystem.phase == "rename" {
		return errors.New("injected publication failure")
	}
	return filesystem.Fs.Rename(
		oldName,
		newName,
	)
}

type credentialFaultFile struct {
	afero.File
	cancel context.CancelFunc
	phase  string
}

func (file *credentialFaultFile) Write(data []byte) (int, error) {
	if file.phase == "write" {
		return 0, errors.New("injected write failure")
	}
	return file.File.Write(data)
}

func (file *credentialFaultFile) Sync() error {
	if file.phase == "sync" {
		return errors.New("injected sync failure")
	}
	if file.phase == "cancel" {
		file.cancel()
	}
	return file.File.Sync()
}

func (file *credentialFaultFile) Close() error {
	err := file.File.Close()
	if file.phase == "close" {
		return errors.New("injected close failure")
	}
	return err
}

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
)

const credentialFile = "auth.json"
const credentialLock = "auth.json.lock"

const testOpenAIEntry = `{"access_token":"access","refresh_token":"refresh","id_token":"id","expires_at":"2026-01-01T00:00:00Z","account_id":"account"}`
const testAuthDocument = `{"openai":` + testOpenAIEntry + `,"opencode":{"api_key":"old"}}`

func TestStoreSave(t *testing.T) {
	for _, test := range []struct {
		name        string
		original    string
		setup       func(*testing.T, string, string)
		cancel      bool
		provider    Provider
		missingPath bool
		badOAuth    string
		missingFS   bool
		zeroStore   bool
		nilStore    bool
		wantErr     bool
	}{
		{
			name:     "create",
			provider: OpenCode,
		},
		{
			name:     "replace preserving OpenAI",
			provider: OpenCode,
			original: testAuthDocument,
		},
		{
			name:     "invalid UTF8 access token",
			provider: OpenAI,
			original: testAuthDocument,
			badOAuth: "access",
			wantErr:  true,
		},
		{
			name:     "invalid UTF8 refresh token",
			provider: OpenAI,
			original: testAuthDocument,
			badOAuth: "refresh",
			wantErr:  true,
		},
		{
			name:     "invalid UTF8 ID token",
			provider: OpenAI,
			original: testAuthDocument,
			badOAuth: "id",
			wantErr:  true,
		},
		{
			name:     "invalid UTF8 account ID",
			provider: OpenAI,
			original: testAuthDocument,
			badOAuth: "account",
			wantErr:  true,
		},
		{
			name:     "empty document",
			provider: OpenCode,
			original: `{}`,
		},
		{
			name:     "canceled",
			provider: OpenCode,
			cancel:   true,
			wantErr:  true,
		},
		{
			name:        "missing path",
			provider:    OpenCode,
			missingPath: true,
			wantErr:     true,
		},
		{
			name:      "missing filesystem",
			provider:  OpenCode,
			missingFS: true,
			wantErr:   true,
		},
		{
			name:      "zero store",
			provider:  OpenCode,
			zeroStore: true,
			wantErr:   true,
		},
		{
			name:     "nil store",
			provider: OpenCode,
			nilStore: true,
			wantErr:  true,
		},
		{
			name:     "invalid provider",
			provider: 0,
			wantErr:  true,
		},
		{
			name:     "malformed document",
			provider: OpenCode,
			original: `secret-value-not-json`,
			wantErr:  true,
		},
		{
			name:     "null document",
			provider: OpenCode,
			original: `null`,
			wantErr:  true,
		},
		{
			name:     "array document",
			provider: OpenCode,
			original: `[]`,
			wantErr:  true,
		},
		{
			name:     "trailing data",
			provider: OpenCode,
			original: testAuthDocument + ` {}`,
			wantErr:  true,
		},
		{
			name:     "unknown provider",
			provider: OpenCode,
			original: `{"unknown":{"api_key":"secret"}}`,
			wantErr:  true,
		},
		{
			name:     "invalid OpenAI credentials",
			provider: OpenCode,
			original: `{"openai":{}}`,
			wantErr:  true,
		},
		{
			name:     "invalid OpenCode credentials",
			provider: OpenCode,
			original: `{"opencode":{"api_key":""}}`,
			wantErr:  true,
		},
		{
			name:     "invalid UTF8",
			provider: OpenCode,
			original: "{\"opencode\":{\"api_key\":\"secret\xff\"}}",
			wantErr:  true,
		},
		{
			name:     "oversized document",
			provider: OpenCode,
			original: strings.Repeat("x", maxCredentialFile+1),
			wantErr:  true,
		},
		{
			name:     "configuration directory is file",
			provider: OpenCode,
			wantErr:  true,
			setup: func(t *testing.T, directory, _ string) {
				if err := os.WriteFile(
					directory,
					[]byte("keep"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:     "configuration directory is symlink",
			provider: OpenCode,
			wantErr:  true,
			setup: func(t *testing.T, directory, outside string) {
				if err := os.Symlink(
					outside,
					directory,
				); err != nil {
					t.Skipf(
						"symlinks unavailable: %v",
						err,
					)
				}
			},
		},
		{
			name:     "credential is symlink",
			provider: OpenCode,
			wantErr:  true,
			setup: func(t *testing.T, directory, outside string) {
				if err := os.Mkdir(
					directory,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(
					filepath.Join(
						outside,
						credentialFile,
					),
					filepath.Join(
						directory,
						credentialFile,
					),
				); err != nil {
					t.Skipf(
						"symlinks unavailable: %v",
						err,
					)
				}
			},
		},
		{
			name:     "credential is directory",
			provider: OpenCode,
			wantErr:  true,
			setup: func(t *testing.T, directory, _ string) {
				if err := os.MkdirAll(
					filepath.Join(
						directory,
						credentialFile,
					),
					0o700,
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:     "existing readable configuration directory",
			provider: OpenCode,
			setup: func(t *testing.T, directory, _ string) {
				if runtime.GOOS == "windows" {
					t.Skip("Unix permissions")
				}
				if err := os.Mkdir(
					directory,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(
					directory,
					0o755,
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:     "configuration directory writable by others",
			provider: OpenCode,
			wantErr:  true,
			setup: func(t *testing.T, directory, _ string) {
				if runtime.GOOS == "windows" {
					t.Skip("Unix permissions")
				}
				if err := os.Mkdir(
					directory,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(
					directory,
					0o777,
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:     "readable credential file",
			provider: OpenCode,
			original: testAuthDocument,
			wantErr:  true,
			setup: func(t *testing.T, directory, _ string) {
				if runtime.GOOS == "windows" {
					t.Skip("Unix permissions")
				}
				if err := os.Chmod(
					filepath.Join(
						directory,
						credentialFile,
					),
					0o644,
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:     "lock is symlink",
			provider: OpenCode,
			wantErr:  true,
			setup: func(t *testing.T, directory, outside string) {
				if err := os.Mkdir(
					directory,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(
					filepath.Join(
						outside,
						credentialFile,
					),
					filepath.Join(
						directory,
						credentialLock,
					),
				); err != nil {
					t.Skipf(
						"symlinks unavailable: %v",
						err,
					)
				}
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				directory := filepath.Join(
					t.TempDir(),
					"config",
				)
				outside := t.TempDir()
				outsidePath := filepath.Join(
					outside,
					credentialFile,
				)
				if err := os.WriteFile(
					outsidePath,
					[]byte("keep"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(
					directory,
					credentialFile,
				)
				if test.original != "" {
					if err := os.Mkdir(
						directory,
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(
						path,
						[]byte(test.original),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				if test.setup != nil {
					test.setup(
						t,
						directory,
						outside,
					)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				if test.missingPath {
					path = ""
				}
				store := NewStore(afero.NewOsFs())
				if test.missingFS {
					store = NewStore(nil)
				}
				if test.zeroStore {
					store = &Store{}
				}
				if test.nilStore {
					store = nil
				}
				credentials := Credentials{
					OpenCode: &APIKeyCredentials{
						APIKey: "new",
					},
				}
				if test.badOAuth != "" {
					if err := json.Unmarshal(
						[]byte(testAuthDocument),
						&credentials,
					); err != nil {
						t.Fatal(err)
					}
					switch test.badOAuth {
					case "access":
						credentials.OpenAI.AccessToken = "\xff"
					case "refresh":
						credentials.OpenAI.RefreshToken = "\xff"
					case "id":
						credentials.OpenAI.IDToken = "\xff"
					case "account":
						credentials.OpenAI.AccountID = "\xff"
					}
				}
				err := store.Save(
					ctx,
					path,
					test.provider,
					credentials,
				)
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"unexpected error: %v",
						err,
					)
				}
				data, err := os.ReadFile(outsidePath)
				if err != nil || string(data) != "keep" {
					t.Error("external file changed")
				}
				if test.wantErr {
					if test.original != "" {
						data, err := os.ReadFile(path)
						if err != nil || string(data) != test.original {
							t.Error("failed update changed original credentials")
						}
					}
					if test.cancel {
						if _, err := os.Stat(directory); !errors.Is(
							err,
							fs.ErrNotExist,
						) {
							t.Error("canceled save created a directory")
						}
					}
					return
				}
				data, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var document Credentials
				if err := json.Unmarshal(
					data,
					&document,
				); err != nil {
					t.Fatal(err)
				}
				if document.OpenCode == nil || document.OpenCode.APIKey != "new" {
					t.Error("new credentials not published")
				}
				if test.original == testAuthDocument && (document.OpenAI == nil || document.OpenAI.AccessToken != "access") {
					t.Error("other provider was changed")
				}
				entries, err := os.ReadDir(directory)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 2 {
					t.Error("unexpected files; expected auth.json and its lock")
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
					t.Error("credential file not private")
				}
			},
		)
	}
}

func TestStoreRemove(t *testing.T) {
	for _, test := range []struct {
		name             string
		provider         Provider
		original         string
		cancel           bool
		missingDirectory bool
		wantErr          bool
		wantOpenAI       bool
		wantOpenCode     bool
	}{
		{
			name:         "remove OpenAI preserving OpenCode",
			provider:     OpenAI,
			original:     testAuthDocument,
			wantOpenCode: true,
		},
		{
			name:       "remove OpenCode preserving OpenAI",
			provider:   OpenCode,
			original:   testAuthDocument,
			wantOpenAI: true,
		},
		{
			name:     "remove last credential",
			provider: OpenCode,
			original: `{"opencode":{"api_key":"old"}}`,
		},
		{
			name:         "absent provider",
			provider:     OpenAI,
			original:     `{"opencode":{"api_key":"old"}}`,
			wantOpenCode: true,
		},
		{
			name:     "empty document",
			provider: OpenAI,
			original: `{}`,
		},
		{
			name:     "absent file",
			provider: OpenCode,
		},
		{
			name:             "absent directory",
			provider:         OpenCode,
			missingDirectory: true,
		},
		{
			name:     "invalid provider",
			provider: 0,
			original: testAuthDocument,
			wantErr:  true,
		},
		{
			name:     "canceled",
			provider: OpenCode,
			original: testAuthDocument,
			cancel:   true,
			wantErr:  true,
		},
		{
			name:     "malformed document",
			provider: OpenCode,
			original: "secret-not-json",
			wantErr:  true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				directory := filepath.Join(
					t.TempDir(),
					"config",
				)
				if !test.missingDirectory {
					if err := os.Mkdir(
						directory,
						0o700,
					); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(
					directory,
					credentialFile,
				)
				if test.original != "" {
					if err := os.WriteFile(
						path,
						[]byte(test.original),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				store := NewStore(afero.NewOsFs())
				err := store.Save(
					ctx,
					path,
					test.provider,
					Credentials{},
				)
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"unexpected error: %v",
						err,
					)
				}
				if test.original == "" {
					if _, err := os.Stat(path); !errors.Is(
						err,
						fs.ErrNotExist,
					) {
						t.Error("logout created auth.json")
					}
					if !test.missingDirectory {
						entries, err := os.ReadDir(directory)
						if err != nil || len(entries) != 0 {
							t.Error("absent file logout created state")
						}
					}
					return
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if test.wantErr {
					if string(data) != test.original {
						t.Error("failed logout changed credentials")
					}
					return
				}
				var document Credentials
				if err := json.Unmarshal(
					data,
					&document,
				); err != nil {
					t.Fatal(err)
				}
				if (document.OpenAI != nil) != test.wantOpenAI || (document.OpenCode != nil) != test.wantOpenCode {
					t.Error("incorrect remaining providers")
				}
				if document.OpenAI != nil && document.OpenAI.AccessToken != "access" {
					t.Error("remaining OpenAI credential changed")
				}
				if document.OpenCode != nil && document.OpenCode.APIKey != "old" {
					t.Error("remaining OpenCode credential changed")
				}
				if err := store.Save(
					t.Context(),
					path,
					test.provider,
					Credentials{},
				); err != nil {
					t.Error("logout not idempotent")
				}
			},
		)
	}
}

func TestStoreConcurrency(t *testing.T) {
	for _, test := range []struct {
		name   string
		remove bool
	}{
		{
			name: "concurrent logins preserve both providers",
		},
		{
			name:   "login and logout preserve the login",
			remove: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				directory := t.TempDir()
				path := filepath.Join(
					directory,
					credentialFile,
				)
				if err := os.WriteFile(
					path,
					[]byte(testAuthDocument),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				var credentials Credentials
				if err := json.Unmarshal(
					[]byte(testAuthDocument),
					&credentials,
				); err != nil {
					t.Fatal(err)
				}
				credentials.OpenCode.APIKey = "new"
				start := make(chan struct{})
				errors := make(chan error, 2)
				var wait sync.WaitGroup
				wait.Go(func() {
					<-start
					store := NewStore(afero.NewOsFs())
					errors <- store.Save(
						t.Context(),
						path,
						OpenCode,
						credentials,
					)
				})
				wait.Go(func() {
					<-start
					store := NewStore(afero.NewOsFs())
					if test.remove {
						errors <- store.Save(
							t.Context(),
							path,
							OpenAI,
							Credentials{},
						)
					} else {
						errors <- store.Save(
							t.Context(),
							path,
							OpenAI,
							credentials,
						)
					}
				})
				close(start)
				wait.Wait()
				close(errors)
				for err := range errors {
					if err != nil {
						t.Fatal(err)
					}
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var document Credentials
				if err := json.Unmarshal(
					data,
					&document,
				); err != nil {
					t.Fatal(err)
				}
				if document.OpenCode == nil || document.OpenCode.APIKey != "new" || (document.OpenAI == nil) != test.remove {
					t.Error("concurrent update was lost")
				}
			},
		)
	}
}

func TestStoreLockCancellation(t *testing.T) {
	if os.Getenv("CPE_TEST_HOLD_AUTH_LOCK") == "1" {
		lock, err := lockCredentials(
			t.Context(),
			afero.NewOsFs(),
			filepath.Join(
				os.Getenv("CPE_TEST_AUTH_DIRECTORY"),
				credentialFile,
			),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer closeCredentialLock(lock)
		if _, err := os.Stdout.WriteString("locked\n"); err != nil {
			t.Fatal(err)
		}
		// The parent terminates this subprocess after testing cancellation.
		select {}
	}
	for _, test := range []struct {
		name   string
		remove bool
	}{
		{
			name: "save",
		},
		{
			name:   "remove",
			remove: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				directory := t.TempDir()
				path := filepath.Join(
					directory,
					credentialFile,
				)
				if err := os.WriteFile(
					path,
					[]byte(testAuthDocument),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				command := exec.CommandContext(
					t.Context(),
					os.Args[0],
					"-test.run=^TestStoreLockCancellation$",
					"-test.timeout=1m",
				)
				command.Env = append(
					os.Environ(),
					"CPE_TEST_HOLD_AUTH_LOCK=1",
					"CPE_TEST_AUTH_DIRECTORY="+directory,
				)
				pipe, err := command.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := command.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() {
					_ = command.Process.Kill()
					_ = command.Wait()
				}()
				ready := make([]byte, len("locked\n"))
				if _, err := io.ReadFull(
					pipe,
					ready,
				); err != nil || string(ready) != "locked\n" {
					t.Fatalf(
						"lock subprocess failed: %v",
						err,
					)
				}
				ctx, cancel := context.WithTimeout(
					t.Context(),
					30*time.Millisecond,
				)
				defer cancel()
				store := NewStore(afero.NewOsFs())
				if test.remove {
					err = store.Save(
						ctx,
						path,
						OpenCode,
						Credentials{},
					)
				} else {
					err = store.Save(
						ctx,
						path,
						OpenCode,
						Credentials{
							OpenCode: &APIKeyCredentials{
								APIKey: "new",
							},
						},
					)
				}
				if !errors.Is(
					err,
					context.DeadlineExceeded,
				) {
					t.Fatalf(
						"expected lock wait cancellation, got %v",
						err,
					)
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != testAuthDocument {
					t.Error("canceled update changed credentials")
				}
			},
		)
	}
}

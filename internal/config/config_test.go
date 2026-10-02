package config_test

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"

	"github.com/spachava753/cpe/internal/config"
)

func TestLoadConfig(t *testing.T) {
	for _, test := range []struct {
		name            string
		content         string
		openErr         error
		readErr         error
		wantErr         error
		wantSyntaxError bool
	}{
		{name: "empty configuration", content: `{}`},
		{name: "surrounding whitespace", content: " \n\t{}\r\n"},
		{name: "empty file", wantErr: io.EOF},
		{name: "malformed JSON", content: `{"broken":]}`, wantSyntaxError: true},
		{name: "open failure", openErr: fs.ErrPermission, wantErr: fs.ErrPermission},
		{name: "read failure", content: `{}`, readErr: io.ErrClosedPipe, wantErr: io.ErrClosedPipe},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The supplied filesystem represents the configuration directory;
			// loading must not depend on the machine's actual home directory.
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			root := &configFS{
				ReadDirFS: fstest.MapFS{"settings.json": {Data: []byte(test.content)}},
				openErr:   test.openErr,
				readErr:   test.readErr,
			}

			var fsys fs.FS = root // Matches the interface returned by os.DirFS.
			got, err := config.LoadConfig(fsys)
			switch {
			case test.wantSyntaxError:
				if _, ok := errors.AsType[*json.SyntaxError](err); !ok {
					t.Errorf("LoadConfig error = %v; want a JSON syntax error", err)
				}
			case test.wantErr != nil:
				if !errors.Is(err, test.wantErr) {
					t.Errorf("LoadConfig error = %v; want %v", err, test.wantErr)
				}
			default:
				if err != nil {
					t.Errorf("LoadConfig error = %v; want nil", err)
				}
			}
			if diff := cmp.Diff(config.Config{}, got); diff != "" {
				t.Errorf("LoadConfig configuration mismatch (-want +got):\n%s", diff)
			}
			if !slices.Equal(root.opened, []string{"settings.json"}) {
				t.Errorf("opened paths = %q; want [settings.json] relative to the supplied filesystem", root.opened)
			}
			wantCloses := 1
			if test.openErr != nil {
				wantCloses = 0
			}
			if root.closes != wantCloses {
				t.Errorf("file closes = %d; want %d", root.closes, wantCloses)
			}
		})
	}
}

type configFS struct {
	fs.ReadDirFS
	openErr error
	readErr error
	opened  []string
	closes  int
}

func (root *configFS) Open(name string) (fs.File, error) {
	root.opened = append(root.opened, name)
	if root.openErr != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: root.openErr}
	}
	// The requested path is asserted separately. Return the fixture even for
	// a wrong path so JSON, I/O, and cleanup assertions can expose independent bugs.
	file, err := root.ReadDirFS.Open("settings.json")
	if err != nil {
		return nil, err
	}
	return &configFile{File: file, root: root}, nil
}

type configFile struct {
	fs.File
	root *configFS
}

func (file *configFile) Read(data []byte) (int, error) {
	if file.root.readErr != nil {
		return 0, file.root.readErr
	}
	return file.File.Read(data)
}

func (file *configFile) Close() error {
	file.root.closes++
	return file.File.Close()
}

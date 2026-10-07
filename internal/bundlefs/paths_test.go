package bundlefs_test

import (
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

type linkFS struct {
	afero.Fs
	link string
}

type linkInfo struct{ fs.FileInfo }

func (info linkInfo) Mode() fs.FileMode { return fs.ModeSymlink | 0o777 }
func (info linkInfo) IsDir() bool       { return false }

func (filesystem linkFS) LstatIfPossible(name string) (fs.FileInfo, bool, error) {
	info, err := filesystem.Stat(name)
	if err == nil && filepath.Clean(name) == filesystem.link {
		info = linkInfo{
			FileInfo: info,
		}
	}
	return info, true, err
}

func TestCheckDestination(t *testing.T) {
	root := filepath.FromSlash("/bundles")
	link := filepath.Join(
		root,
		"link",
	)
	for _, test := range []struct {
		name     string
		path     string
		noFS     bool
		statOnly bool
		errText  string
	}{
		{
			name: "existing root",
			path: root,
		},
		{
			name:     "stat-only filesystem",
			path:     root,
			statOnly: true,
		},
		{
			name: "new root",
			path: filepath.Join(
				root,
				"new",
			),
		},
		{
			name:    "empty root",
			errText: "required",
		},
		{
			name:    "blank root",
			path:    " \t",
			errText: "required",
		},
		{
			name:    "unavailable filesystem",
			path:    root,
			noFS:    true,
			errText: "unavailable",
		},
		{
			name:    "native symlink",
			path:    link,
			errText: "symlink",
		},
		{
			name:    "forward separators",
			path:    filepath.ToSlash(link),
			errText: "symlink",
		},
		{
			name:    "mixed separators",
			path:    filepath.ToSlash(root) + "/link" + string(filepath.Separator) + "child",
			errText: "symlink",
		},
		{
			name:    "native canceled link",
			path:    link + string(filepath.Separator) + "..",
			errText: "symlink",
		},
		{
			name:    "forward canceled link",
			path:    filepath.ToSlash(link) + "/..",
			errText: "symlink",
		},
		{
			name:    "mixed canceled link",
			path:    filepath.ToSlash(link) + string(filepath.Separator) + "..",
			errText: "symlink",
		},
		{
			name:    "missing parent traversal",
			path:    root + "/missing/../new",
			errText: "missing directory",
		},
		{
			name: "file component",
			path: filepath.Join(
				root,
				"file",
				"child",
			),
			errText: "not a directory",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				base := afero.NewMemMapFs()
				if err := base.MkdirAll(
					link,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := afero.WriteFile(
					base,
					filepath.Join(
						root,
						"file",
					),
					nil,
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				var filesystem afero.Fs = linkFS{
					Fs:   base,
					link: link,
				}
				if test.statOnly {
					filesystem = struct{ afero.Fs }{
						Fs: base,
					}
				}
				if test.noFS {
					filesystem = nil
				}
				err := bundlefs.CheckDestination(
					t.Context(),
					filesystem,
					test.path,
				)
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"error = %v, want %q",
							err,
							test.errText,
						)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			},
		)
	}
	if runtime.GOOS == "windows" {
		t.Run(
			"drive-relative root is rejected",
			func(t *testing.T) {
				err := bundlefs.CheckDestination(
					t.Context(),
					afero.NewMemMapFs(),
					"C:bundles",
				)
				if err == nil || !strings.Contains(
					err.Error(),
					"drive-relative",
				) {
					t.Fatalf(
						"error = %v",
						err,
					)
				}
			},
		)
	}
}

func TestCheckSourceDirectory(t *testing.T) {
	files := fstest.MapFS{
		"valid/nested/file": &fstest.MapFile{
			Data: []byte("resource"),
		},
		"file": &fstest.MapFile{
			Data: []byte("file"),
		},
		"link": &fstest.MapFile{
			Mode: fs.ModeSymlink,
		},
	}
	for _, test := range []struct {
		name    string
		path    string
		errText string
	}{
		{
			name: "source root",
			path: ".",
		},
		{
			name: "nested directory",
			path: "valid/nested",
		},
		{
			name:    "empty path",
			errText: "invalid source directory",
		},
		{
			name:    "escaping path",
			path:    "../outside",
			errText: "invalid source directory",
		},
		{
			name:    "Windows separator",
			path:    "valid\\nested",
			errText: "invalid source directory",
		},
		{
			name:    "null byte",
			path:    "valid\x00nested",
			errText: "invalid source directory",
		},
		{
			name:    "missing directory",
			path:    "missing",
			errText: "does not exist",
		},
		{
			name:    "file is not a directory",
			path:    "file",
			errText: "regular directory",
		},
		{
			name:    "link is not a directory",
			path:    "link",
			errText: "symlink",
		},
		{
			name:    "linked parent",
			path:    "link/child",
			errText: "symlink",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				err := bundlefs.CheckSourceDirectory(
					files,
					test.path,
				)
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"error = %v, want %q",
							err,
							test.errText,
						)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			},
		)
	}
}

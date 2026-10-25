package plugins_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/plugins"
)

func TestManagerContainment(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		link         string
		target       string
		wantFile     string
		errText      string
		absolute     bool
		manifestLink bool
		rootLink     bool
	}{
		{
			name:     "contained file link materialized",
			link:     "assets/alias.txt",
			target:   "value.txt",
			wantFile: "assets/alias.txt",
		},
		{
			name:     "contained directory link materialized",
			link:     "resource-copy",
			target:   "assets",
			wantFile: "resource-copy/value.txt",
		},
		{
			name:         "contained manifest link materialized",
			link:         "plugin.json",
			target:       "manifest.json",
			manifestLink: true,
			wantFile:     "plugin.json",
		},
		{
			name:    "escaping relative link rejected even in unused asset",
			link:    "escape.txt",
			target:  "../private.txt",
			errText: "permission denied",
		},
		{
			name:     "absolute link rejected",
			link:     "escape.txt",
			target:   "private.txt",
			absolute: true,
			errText:  "permission denied",
		},
		{
			name:    "directory cycle rejected",
			link:    "cycle",
			target:  ".",
			errText: "link cycle",
		},
		{
			name:     "package root link rejected",
			link:     "alias",
			target:   "package",
			rootLink: true,
			errText:  "regular directory",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				source := t.TempDir()
				packageDirectory := filepath.Join(
					source,
					"package",
				)
				for name, data := range map[string]string{
					"package/plugin.json":      alphaPlugin,
					"package/assets/value.txt": "support",
					"private.txt":              "must remain outside the package",
				} {
					filename := filepath.Join(
						source,
						filepath.FromSlash(name),
					)
					if err := os.MkdirAll(
						filepath.Dir(filename),
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(
						filename,
						[]byte(data),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				if test.manifestLink {
					if err := os.Rename(
						filepath.Join(
							packageDirectory,
							"plugin.json",
						),
						filepath.Join(
							packageDirectory,
							"manifest.json",
						),
					); err != nil {
						t.Fatal(err)
					}
				}
				link := filepath.Join(
					packageDirectory,
					filepath.FromSlash(test.link),
				)
				selectedPath := "package"
				if test.rootLink {
					link = filepath.Join(
						source,
						test.link,
					)
					selectedPath = test.link
				}
				target := test.target
				if test.absolute {
					target = filepath.Join(
						source,
						target,
					)
				}
				if err := os.Symlink(
					target,
					link,
				); err != nil {
					t.Skipf(
						"symlinks unavailable: %v",
						err,
					)
				}
				destination := filepath.Join(
					t.TempDir(),
					"plugins",
				)
				manager := plugins.NewManager(
					managerSourceClient{
						files: os.DirFS(source),
					},
					afero.NewOsFs(),
				)
				result, installErr := manager.Install(
					t.Context(),
					plugins.InstallRequest{
						Source:    managerSource,
						Directory: destination,
						Path:      &selectedPath,
					},
				)
				if test.errText != "" {
					if installErr == nil || !strings.Contains(
						installErr.Error(),
						test.errText,
					) || result != nil {
						t.Fatalf(
							"Install = %#v, %v; want %q",
							result,
							installErr,
							test.errText,
						)
					}
					if _, err := os.Stat(destination); !errors.Is(
						err,
						fs.ErrNotExist,
					) {
						t.Fatal("unsafe payload caused destination writes")
					}
					return
				}
				if installErr != nil || len(result) != 1 {
					t.Fatalf(
						"Install = %#v, %v",
						result,
						installErr,
					)
				}
				filename := filepath.Join(
					destination,
					"alpha",
					filepath.FromSlash(test.wantFile),
				)
				info, installErr := os.Lstat(filename)
				if installErr != nil || !info.Mode().IsRegular() {
					t.Fatal("contained link was not materialized into a regular file")
				}
				data, installErr := os.ReadFile(filename)
				want := "support"
				if test.manifestLink {
					want = alphaPlugin
				}
				if installErr != nil || string(data) != want {
					t.Fatalf(
						"materialized file = %q, %v",
						data,
						installErr,
					)
				}
			},
		)
	}
}

func TestManagerDestinationLinks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		operation func(*plugins.Manager, *testing.T) error
		name      string
	}{
		{
			name: "override does not replace a linked package",
			operation: func(manager *plugins.Manager, t *testing.T) error {
				_, err := manager.Install(
					t.Context(),
					plugins.InstallRequest{
						Source:    managerSource,
						Directory: "/plugins",
						Override:  true,
					},
				)
				return err
			},
		},
		{
			name: "list rejects package root links",
			operation: func(manager *plugins.Manager, t *testing.T) error {
				_, err := manager.ListInstalled(
					t.Context(),
					"/plugins",
				)
				return err
			},
		},
		{
			name: "remove does not delete linked packages",
			operation: func(manager *plugins.Manager, t *testing.T) error {
				_, err := manager.Remove(
					t.Context(),
					plugins.RemoveRequest{
						Directory: "/plugins",
						Plugins: []string{
							"alpha",
						},
					},
				)
				return err
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				filesystem := afero.NewMemMapFs()
				writeManagerFiles(
					t,
					filesystem,
					fstest.MapFS{
						"alpha/plugin.json": {
							Data: []byte(alphaPlugin),
						},
					},
				)
				manager := plugins.NewManager(
					managerSourceClient{
						files: fstest.MapFS{
							"plugin.json": {
								Data: []byte(alphaPlugin),
							},
						},
					},
					managerLinkFS{
						Fs: filesystem,
						links: map[string]bool{
							filepath.FromSlash("/plugins/alpha"): true,
						},
					},
				)
				if err := test.operation(
					manager,
					t,
				); err == nil {
					t.Fatal("destination symlink accepted")
				}
				data, err := afero.ReadFile(
					filesystem,
					"/plugins/alpha/plugin.json",
				)
				if err != nil || string(data) != alphaPlugin {
					t.Fatal("linked target changed")
				}
			},
		)
	}
	for _, test := range []struct {
		path func(string) string
		name string
	}{
		{
			name: "intermediate link with native separators",
			path: func(link string) string { return link + string(filepath.Separator) + "plugins" },
		},
		{
			name: "intermediate link with forward slashes",
			path: func(link string) string { return filepath.ToSlash(link) + "/plugins" },
		},
		{
			name: "intermediate link with mixed separators",
			path: func(link string) string { return link + "/plugins" },
		},
		{
			name: "mixed separators do not clean away link parent traversal",
			path: func(link string) string { return filepath.ToSlash(link) + string(filepath.Separator) + "../plugins" },
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				link := filepath.Join(
					t.TempDir(),
					"link",
				)
				filesystem := afero.NewMemMapFs()
				if err := filesystem.MkdirAll(
					filepath.Join(
						link,
						"plugins",
					),
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				manager := plugins.NewManager(
					nil,
					managerLinkFS{
						Fs: filesystem,
						links: map[string]bool{
							link: true,
						},
					},
				)
				if _, err := manager.ListInstalled(
					t.Context(),
					test.path(link),
				); err == nil || !strings.Contains(
					err.Error(),
					"symlink",
				) {
					t.Fatalf(
						"intermediate symlink error = %v",
						err,
					)
				}
			},
		)
	}
}

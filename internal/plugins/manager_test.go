package plugins_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/source"
)

const (
	managerSource = "https://example.com/plugins.git"
	alphaPlugin   = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"alpha","description":"Alpha plugin"}`
	betaPlugin    = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"beta","description":"Beta plugin"}`
	stripePlugin  = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"stripe","description":"Stripe integrations"}`
	stripeMCP     = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"stripe":{"type":"streamable-http","url":"https://mcp.stripe.com"}}}`
)

type managerSourceClient struct {
	files   fs.FS
	err     error
	cleanup func() error
}

func (client managerSourceClient) Fetch(context.Context, string) (source.Checkout, error) {
	if client.err != nil {
		return source.Checkout{}, client.err
	}
	return source.Checkout{
		FS:      client.files,
		Cleanup: client.cleanup,
	}, nil
}

func TestManagerListSource(t *testing.T) {
	t.Parallel()
	fetchErr := errors.New("fetch failed")
	cleanupErr := errors.New("cleanup failed")
	for _, test := range []struct {
		name            string
		files           fstest.MapFS
		options         plugins.DiscoveryOptions
		fetchErr        error
		cleanupErr      error
		cancel          bool
		nilClient       bool
		nilFS           bool
		want            []plugins.Plugin
		wantDiagnostics int
		wantErr         error
		errText         string
	}{
		{
			name:  "empty source",
			files: fstest.MapFS{},
		},
		{
			name: "source root package",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"nested/plugin.json": {
					Data: []byte(betaPlugin),
				},
			},
			want: []plugins.Plugin{
				{
					Name:        "alpha",
					Description: "Alpha plugin",
					Path:        ".",
				},
			},
		},
		{
			name: "nested packages in deterministic directory order",
			files: fstest.MapFS{
				"b/plugin.json": {
					Data: []byte(betaPlugin),
				},
				"a/nested/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			want: []plugins.Plugin{
				{
					Name:        "alpha",
					Description: "Alpha plugin",
					Path:        "a/nested",
				},
				{
					Name:        "beta",
					Description: "Beta plugin",
					Path:        "b",
				},
			},
		},
		{
			name: "legacy manifests are not native packages",
			files: fstest.MapFS{
				".codex-plugin/plugin.json": {
					Data: []byte(`{"name":"legacy"}`),
				},
				".claude-plugin/plugin.json": {
					Data: []byte(`{"name":"legacy"}`),
				},
				"other/plugin.json": {
					Data: []byte(`{"$schema":"https://example.com/plugin.schema.json","name":"other"}`),
				},
			},
		},
		{
			name: "depth boundary included",
			files: fstest.MapFS{
				"a/b/c/d/e/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			want: []plugins.Plugin{
				{
					Name:        "alpha",
					Description: "Alpha plugin",
					Path:        "a/b/c/d/e",
				},
			},
		},
		{
			name: "depth limit",
			files: fstest.MapFS{
				"a/b/c/d/e/f/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
		},
		{
			name: "full depth",
			files: fstest.MapFS{
				"a/b/c/d/e/f/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			options: plugins.DiscoveryOptions{
				FullDepth: true,
			},
			want: []plugins.Plugin{
				{
					Name:        "alpha",
					Description: "Alpha plugin",
					Path:        "a/b/c/d/e/f",
				},
			},
		},
		{
			name: "ignores Git and dependency directories",
			files: fstest.MapFS{
				".git/plugin.json": {
					Data: []byte("invalid"),
				},
				"node_modules/plugin.json": {
					Data: []byte("invalid"),
				},
			},
		},
		{
			name: "invalid native manifest rejects whole discovery",
			files: fstest.MapFS{
				"a/plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"z/plugin.json": {
					Data: []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"Bad"}`),
				},
			},
			errText: "invalid plugin name",
		},
		{
			name: "malformed JSON",
			files: fstest.MapFS{
				"broken/plugin.json": {
					Data: []byte("invalid"),
				},
			},
			errText: "broken/plugin.json",
		},
		{
			name: "component failures remain diagnostics",
			files: fstest.MapFS{
				"a/plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"a/mcp.json": {
					Data: []byte("invalid"),
				},
			},
			want: []plugins.Plugin{
				{
					Name:        "alpha",
					Description: "Alpha plugin",
					Path:        "a",
				},
			},
			wantDiagnostics: 1,
		},
		{
			name:     "fetch failure",
			fetchErr: fetchErr,
			wantErr:  fetchErr,
		},
		{
			name: "cleanup failure discards discovery",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			cleanupErr: cleanupErr,
			wantErr:    cleanupErr,
		},
		{
			name:    "cancelled",
			cancel:  true,
			wantErr: context.Canceled,
		},
		{
			name:      "missing client",
			nilClient: true,
			errText:   "source client",
		},
		{
			name:    "missing source filesystem",
			nilFS:   true,
			errText: "source filesystem",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				cleanups := 0
				var client source.Client = managerSourceClient{
					files: test.files,
					err:   test.fetchErr,
					cleanup: func() error {
						cleanups++
						return test.cleanupErr
					},
				}
				if test.nilClient {
					client = nil
				} else if test.nilFS {
					client = managerSourceClient{
						cleanup: func() error { cleanups++; return nil },
					}
				}
				got, err := plugins.NewManager(
					client,
					afero.NewMemMapFs(),
				).ListSource(
					ctx,
					managerSource,
					test.options,
				)
				if test.wantErr != nil || test.errText != "" {
					if err == nil || (test.wantErr != nil && !errors.Is(
						err,
						test.wantErr,
					)) || !strings.Contains(
						err.Error(),
						test.errText,
					) || got != nil {
						t.Fatalf(
							"ListSource = %#v, %v; want no partial result and error %v / %q",
							got,
							err,
							test.wantErr,
							test.errText,
						)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if diff := cmp.Diff(
						test.want,
						got,
						cmpopts.IgnoreFields(
							plugins.Plugin{},
							"Diagnostics",
						),
						cmpopts.EquateEmpty(),
					); diff != "" {
						t.Fatal(diff)
					}
					diagnostics := 0
					for _, plugin := range got {
						diagnostics += len(plugin.Diagnostics)
					}
					if diagnostics != test.wantDiagnostics {
						t.Fatalf(
							"diagnostics = %d, want %d",
							diagnostics,
							test.wantDiagnostics,
						)
					}
				}
				wantCleanups := 1
				if test.cancel || test.nilClient || test.fetchErr != nil {
					wantCleanups = 0
				}
				if cleanups != wantCleanups {
					t.Fatalf(
						"cleanup calls = %d, want %d",
						cleanups,
						wantCleanups,
					)
				}
			},
		)
	}
}

func TestManagerInstall(t *testing.T) {
	t.Parallel()
	cleanupErr := errors.New("checkout cleanup failed")
	for _, test := range []struct {
		name            string
		files           fstest.MapFS
		initial         fstest.MapFS
		names           []string
		selectedPath    *string
		override        bool
		fullDepth       bool
		fault           managerFSFault
		cleanupErr      error
		cancel          bool
		wantFiles       map[string]string
		wantNames       []string
		wantDiagnostics int
		committed       bool
		errText         string
	}{
		{
			name: "install source root bundle",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"bin/run.sh": {
					Data: []byte("echo alpha\n"),
					Mode: 0o755,
				},
				"node_modules/support.js": {
					Data: []byte("bundled dependency"),
				},
				".git/config": {
					Data: []byte("Git metadata"),
				},
			},
			wantFiles: map[string]string{
				"alpha/plugin.json":             alphaPlugin,
				"alpha/bin/run.sh":              "echo alpha\n",
				"alpha/node_modules/support.js": "bundled dependency",
			},
			wantNames: []string{
				"alpha",
			},
		},
		{
			name: "select by manifest name not directory",
			files: fstest.MapFS{
				"collection/a/plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"other/plugin.json": {
					Data: []byte(betaPlugin),
				},
			},
			names: []string{
				"alpha",
				"alpha",
			},
			wantFiles: map[string]string{
				"alpha/plugin.json": alphaPlugin,
			},
			wantNames: []string{
				"alpha",
			},
		},
		{
			name: "install all in manifest name order",
			files: fstest.MapFS{
				"a/plugin.json": {
					Data: []byte(betaPlugin),
				},
				"b/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			wantFiles: map[string]string{
				"alpha/plugin.json": alphaPlugin,
				"beta/plugin.json":  betaPlugin,
			},
			wantNames: []string{
				"alpha",
				"beta",
			},
		},
		{
			name: "ambiguous name",
			files: fstest.MapFS{
				"a/plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"b/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			errText: `ambiguous plugin name "alpha" at source directories ["a" "b"]; use --path`,
		},
		{
			name: "exact path resolves ambiguity",
			files: fstest.MapFS{
				"a/plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"b/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			selectedPath: new("b"),
			wantFiles: map[string]string{
				"alpha/plugin.json": alphaPlugin,
			},
			wantNames: []string{
				"alpha",
			},
		},
		{
			name: "exact path bypasses depth and unrelated malformed manifest",
			files: fstest.MapFS{
				"a/plugin.json": {
					Data: []byte("invalid"),
				},
				"a/b/c/d/e/f/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			selectedPath: new("a/b/c/d/e/f"),
			wantFiles: map[string]string{
				"alpha/plugin.json": alphaPlugin,
			},
			wantNames: []string{
				"alpha",
			},
		},
		{
			name: "legacy exact path rejected",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(`{"name":"legacy"}`),
				},
			},
			selectedPath: new("."),
			errText:      "$schema",
		},
		{
			name:  "selected name missing",
			files: fstest.MapFS{},
			names: []string{
				"missing",
			},
			errText: "not found",
		},
		{
			name: "invalid name",
			names: []string{
				"../outside",
			},
			errText: "invalid plugin directory name",
		},
		{
			name: "path and names are exclusive",
			names: []string{
				"alpha",
			},
			selectedPath: new("."),
			errText:      "mutually exclusive",
		},
		{
			name:         "parent path rejected",
			selectedPath: new("../outside"),
			errText:      "invalid source directory",
		},
		{
			name:         "empty path rejected",
			selectedPath: new(""),
			errText:      "invalid source directory",
		},
		{
			name: "manifest filename is not a package directory",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			selectedPath: new("plugin.json"),
			errText:      "regular directory",
		},
		{
			name: "existing package preserved",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			initial: fstest.MapFS{
				"alpha/plugin.json": {
					Data: []byte("old manifest"),
				},
				"alpha/stale.txt": {
					Data: []byte("old resource"),
				},
			},
			errText: "use --override",
		},
		{
			name: "override replaces whole damaged package",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			initial: fstest.MapFS{
				"alpha/plugin.json": {
					Data: []byte("damaged manifest"),
				},
				"alpha/stale.txt": {
					Data: []byte("old resource"),
				},
			},
			override: true,
			wantFiles: map[string]string{
				"alpha/plugin.json": alphaPlugin,
			},
			wantNames: []string{
				"alpha",
			},
		},
		{
			name: "override refuses unrelated directory",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			initial: fstest.MapFS{
				"alpha/private.txt": {
					Data: []byte("keep"),
				},
			},
			override: true,
			errText:  "plugin.json",
		},
		{
			name: "component failures do not discard payload",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"mcp.json": {
					Data: []byte("invalid"),
				},
			},
			wantFiles: map[string]string{
				"alpha/plugin.json": alphaPlugin,
				"alpha/mcp.json":    "invalid",
			},
			wantNames: []string{
				"alpha",
			},
			wantDiagnostics: 1,
		},
		{
			name: "special payload rejected before writes",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"socket": {
					Mode: fs.ModeSocket,
				},
			},
			errText: "not a regular file",
		},
		{
			name: "cleanup failure prevents publication",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			cleanupErr: cleanupErr,
			errText:    "checkout cleanup failed",
		},
		{
			name: "late publish failure rolls back every package",
			files: fstest.MapFS{
				"alpha/plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"beta/plugin.json": {
					Data: []byte(betaPlugin),
				},
			},
			initial: fstest.MapFS{
				"alpha/plugin.json": {
					Data: []byte("old alpha"),
				},
			},
			override: true,
			fault:    managerLatePublishFailure,
			errText:  "permission denied",
		},
		{
			name: "committed results survive stage cleanup failure",
			files: fstest.MapFS{
				"plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			fault:     managerStageCleanupFailure,
			committed: true,
			errText:   "changes committed",
			wantNames: []string{
				"alpha",
			},
			wantFiles: map[string]string{
				"alpha/plugin.json": alphaPlugin,
			},
		},
		{
			name:    "cancelled before acquisition",
			cancel:  true,
			errText: "context canceled",
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
					test.initial,
				)
				cleanups := 0
				client := managerSourceClient{
					files: test.files,
					cleanup: func() error {
						cleanups++
						return test.cleanupErr
					},
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				result, err := plugins.NewManager(
					client,
					managerFaultFS{
						Fs:    filesystem,
						fault: test.fault,
					},
				).Install(
					ctx,
					plugins.InstallRequest{
						Source:    managerSource,
						Directory: "/plugins",
						Plugins:   test.names,
						Path:      test.selectedPath,
						Override:  test.override,
						Discovery: plugins.DiscoveryOptions{
							FullDepth: test.fullDepth,
						},
					},
				)
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"Install error = %v, want %q",
							err,
							test.errText,
						)
					}
					if !test.committed && result != nil {
						t.Fatal("failed installation returned uncommitted results")
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if test.cleanupErr != nil && !errors.Is(
					err,
					test.cleanupErr,
				) {
					t.Fatal("cleanup cause was lost")
				}
				var names []string
				diagnostics := 0
				for _, entry := range result {
					names = append(
						names,
						entry.Plugin.Name,
					)
					diagnostics += len(entry.Plugin.Diagnostics)
					wantDirectory := filepath.Join(
						"/plugins",
						entry.Plugin.Name,
					)
					if entry.Directory != wantDirectory || entry.Plugin.Path != wantDirectory {
						t.Fatalf(
							"installed paths = %#v",
							entry,
						)
					}
				}
				if diff := cmp.Diff(
					test.wantNames,
					names,
					cmpopts.EquateEmpty(),
				); diff != "" || diagnostics != test.wantDiagnostics {
					t.Fatalf(
						"results differ: %s; diagnostics = %d",
						diff,
						diagnostics,
					)
				}
				wantFiles := test.wantFiles
				if test.errText != "" && !test.committed {
					wantFiles = map[string]string{}
					for name, file := range test.initial {
						wantFiles[name] = string(file.Data)
					}
				}
				actualFiles := map[string]string{}
				if _, err := filesystem.Stat("/plugins"); err == nil {
					if err := afero.Walk(
						filesystem,
						"/plugins",
						func(filename string, info fs.FileInfo, err error) error {
							if err != nil {
								return err
							}
							if info.IsDir() {
								if strings.HasPrefix(
									info.Name(),
									".cpe-stage-",
								) {
									if !test.committed {
										t.Error("staging directory leaked")
									}
									return filepath.SkipDir
								}
								return nil
							}
							data, err := afero.ReadFile(
								filesystem,
								filename,
							)
							if err != nil {
								return err
							}
							name, err := filepath.Rel(
								"/plugins",
								filename,
							)
							actualFiles[filepath.ToSlash(name)] = string(data)
							if strings.HasSuffix(
								filename,
								"run.sh",
							) && info.Mode().Perm() != 0o755 {
								t.Error("executable bit lost")
							}
							return err
						},
					); err != nil {
						t.Fatal(err)
					}
				}
				if diff := cmp.Diff(
					wantFiles,
					actualFiles,
					cmpopts.EquateEmpty(),
				); diff != "" {
					t.Fatal(diff)
				}
				if cleanups > 1 {
					t.Fatal("checkout released more than once")
				}
			},
		)
	}
}

func writeManagerFiles(t *testing.T, filesystem afero.Fs, files fstest.MapFS) {
	t.Helper()
	for name, file := range files {
		filename := filepath.Join(
			"/plugins",
			filepath.FromSlash(name),
		)
		if err := filesystem.MkdirAll(
			filepath.Dir(filename),
			0o700,
		); err != nil {
			t.Fatal(err)
		}
		if err := afero.WriteFile(
			filesystem,
			filename,
			file.Data,
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagerListInstalled(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		files     fstest.MapFS
		wantNames []string
		errText   string
	}{
		{
			name: "missing root",
		},
		{
			name: "manual directory name mismatch",
			files: fstest.MapFS{
				"manual/plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"unrelated/README.md": {
					Data: []byte("not a plugin"),
				},
				".cpe-stage-fixture/old/alpha/plugin.json": {
					Data: []byte("damaged"),
				},
			},
			wantNames: []string{
				"alpha",
			},
		},
		{
			name: "invalid manifest rejects whole inventory",
			files: fstest.MapFS{
				"a/plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"z/plugin.json": {
					Data: []byte("damaged"),
				},
			},
			errText: "z/plugin.json",
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
					test.files,
				)
				result, err := plugins.NewManager(
					nil,
					filesystem,
				).ListInstalled(
					t.Context(),
					"/plugins",
				)
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) || result != nil {
						t.Fatalf(
							"ListInstalled = %#v, %v",
							result,
							err,
						)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var names []string
				for _, entry := range result {
					names = append(
						names,
						entry.Plugin.Name,
					)
					if entry.Plugin.Path != entry.Directory || !filepath.IsAbs(entry.Directory) {
						t.Fatal("installed paths are not native destination paths")
					}
				}
				if diff := cmp.Diff(
					test.wantNames,
					names,
					cmpopts.EquateEmpty(),
				); diff != "" {
					t.Fatal(diff)
				}
			},
		)
	}
}

func TestManagerRemove(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		files   fstest.MapFS
		names   []string
		fault   managerFSFault
		want    []plugins.RemovedPlugin
		errText string
	}{
		{
			name: "named manual package",
			files: fstest.MapFS{
				"manual/plugin.json": {
					Data: []byte(alphaPlugin),
				},
				"manual/resource.txt": {
					Data: []byte("resource"),
				},
			},
			names: []string{
				"manual",
			},
			want: []plugins.RemovedPlugin{
				{
					Name:        "alpha",
					Description: "Alpha plugin",
					Path:        filepath.FromSlash("/plugins/manual"),
				},
			},
		},
		{
			name: "damaged manifest can be removed",
			files: fstest.MapFS{
				"alpha/plugin.json": {
					Data: []byte("damaged"),
				},
			},
			names: []string{
				"alpha",
			},
			want: []plugins.RemovedPlugin{
				{
					Name: "alpha",
					Path: filepath.FromSlash("/plugins/alpha"),
				},
			},
		},
		{
			name: "every selection checked before mutation",
			files: fstest.MapFS{
				"alpha/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			names: []string{
				"alpha",
				"missing",
			},
			errText: "missing",
		},
		{
			name: "unrelated directory cannot be removed",
			files: fstest.MapFS{
				"private/secret.txt": {
					Data: []byte("keep"),
				},
			},
			names: []string{
				"private",
			},
			errText: "not a plugin directory",
		},
		{
			name:    "empty selection",
			errText: "at least one plugin",
		},
		{
			name: "invalid name",
			names: []string{
				"../outside",
			},
			errText: "invalid plugin directory name",
		},
		{
			name: "cleanup failure preserves committed removal result",
			files: fstest.MapFS{
				"alpha/plugin.json": {
					Data: []byte(alphaPlugin),
				},
			},
			names: []string{
				"alpha",
			},
			fault:   managerStageCleanupFailure,
			errText: "changes committed",
			want: []plugins.RemovedPlugin{
				{
					Name:        "alpha",
					Description: "Alpha plugin",
					Path:        filepath.FromSlash("/plugins/alpha"),
				},
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
					test.files,
				)
				result, err := plugins.NewManager(
					nil,
					managerFaultFS{
						Fs:    filesystem,
						fault: test.fault,
					},
				).Remove(
					t.Context(),
					plugins.RemoveRequest{
						Directory: "/plugins",
						Plugins:   test.names,
					},
				)
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"Remove error = %v, want %q",
							err,
							test.errText,
						)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(
					test.want,
					result,
					cmpopts.EquateEmpty(),
				); diff != "" {
					t.Fatal(diff)
				}
				for name, file := range test.files {
					data, readErr := afero.ReadFile(
						filesystem,
						filepath.Join(
							"/plugins",
							filepath.FromSlash(name),
						),
					)
					if len(test.want) > 0 {
						if !errors.Is(
							readErr,
							fs.ErrNotExist,
						) {
							t.Fatal("removed payload remains")
						}
					} else if readErr != nil || string(data) != string(file.Data) {
						t.Fatal("failed removal changed a payload")
					}
				}
			},
		)
	}
}

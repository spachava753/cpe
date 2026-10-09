package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/plugins"
)

func TestPluginsCommands(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		args    []string
		install *plugins.InstallOptions
		list    *plugins.ListOptions
		remove  *plugins.RemoveOptions
		err     string
	}{
		{
			name: "install global",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
				"--scope",
				"global",
			},
			install: &plugins.InstallOptions{
				Scope: commandio.ScopeOptions{
					Global: true,
				},
				Source: "owner/repo",
			},
		},
		{
			name: "install alias with repeated plugins and full depth",
			args: []string{
				"plugins",
				"i",
				"owner/repo",
				"--scope",
				"project",
				"--plugin",
				"one",
				"--plugin",
				"two",
				"--full-depth",
			},
			install: &plugins.InstallOptions{
				Scope: commandio.ScopeOptions{
					Project: true,
				},
				Source: "owner/repo",
				Plugins: []string{
					"one",
					"two",
				},
				FullDepth: true,
			},
		},
		{
			name: "install JSON flag is forwarded",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
				"--scope",
				"project",
				"--json",
			},
			install: &plugins.InstallOptions{
				Scope: commandio.ScopeOptions{
					Project: true,
				},
				Source: "owner/repo",
				JSON:   true,
			},
		},
		{
			name: "install override is forwarded",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
				"--scope",
				"project",
				"--override",
			},
			install: &plugins.InstallOptions{
				Scope: commandio.ScopeOptions{
					Project: true,
				},
				Source:   "owner/repo",
				Override: true,
			},
		},
		{
			name: "install exact source directory",
			args: []string{
				"plugins",
				"add",
				"owner/repo",
				"--scope",
				"project",
				"--path",
				"variants/react/alpha",
			},
			install: &plugins.InstallOptions{
				Scope: commandio.ScopeOptions{
					Project: true,
				},
				Source: "owner/repo",
				Path:   new("variants/react/alpha"),
			},
		},
		{
			name: "install path and plugin flags are mutually exclusive",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
				"--scope",
				"project",
				"--path",
				"variants/react/alpha",
				"--plugin",
				"alpha",
			},
			err: "none of the others can be",
		},
		{
			name: "install preserves explicitly empty path for invocation validation",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
				"--scope",
				"global",
				"--path=",
			},
			install: &plugins.InstallOptions{
				Scope: commandio.ScopeOptions{
					Global: true,
				},
				Source: "owner/repo",
				Path:   new(""),
			},
		},
		{
			name: "even empty selector flags are mutually exclusive",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
				"--scope",
				"project",
				"--plugin=",
				"--path=",
			},
			err: "none of the others can be",
		},
		{
			name: "install custom directory with inherited scope before command",
			args: []string{
				"plugins",
				"--scope",
				"project",
				"add",
				"owner/repo",
				"--dir",
				"./team-plugins",
			},
			install: &plugins.InstallOptions{
				Scope: commandio.ScopeOptions{
					Directory: new("./team-plugins"),
					Project:   true,
				},
				Source: "owner/repo",
			},
		},
		{
			name: "list defaults",
			args: []string{
				"plugins",
				"list",
			},
			list: &plugins.ListOptions{},
		},
		{
			name: "list JSON flag is forwarded",
			args: []string{
				"plugins",
				"list",
				"--json",
			},
			list: &plugins.ListOptions{
				JSON: true,
			},
		},
		{
			name: "list source",
			args: []string{
				"plugins",
				"list",
				"owner/repo",
			},
			list: &plugins.ListOptions{
				Source: new("owner/repo"),
			},
		},
		{
			name: "list preserves explicitly empty source",
			args: []string{
				"plugins",
				"list",
				"",
			},
			list: &plugins.ListOptions{
				Source: new(""),
			},
		},
		{
			name: "list scoped alias",
			args: []string{
				"plugins",
				"ls",
				"--scope",
				"global",
			},
			list: &plugins.ListOptions{
				Scope: commandio.ScopeOptions{
					Global: true,
				},
			},
		},
		{
			name: "list custom directory without scope",
			args: []string{
				"plugins",
				"list",
				"--dir",
				"./team-plugins",
			},
			list: &plugins.ListOptions{
				Scope: commandio.ScopeOptions{
					Directory: new("./team-plugins"),
				},
			},
		},
		{
			name: "remove JSON flag is forwarded",
			args: []string{
				"plugins",
				"remove",
				"alpha",
				"--json",
			},
			remove: &plugins.RemoveOptions{
				Plugins: []string{
					"alpha",
				},
				JSON: true,
			},
		},
		{
			name: "remove names without scope",
			args: []string{
				"plugins",
				"remove",
				"one",
				"two",
			},
			remove: &plugins.RemoveOptions{
				Plugins: []string{
					"one",
					"two",
				},
			},
		},
		{
			name: "remove custom directory without scope",
			args: []string{
				"plugins",
				"rm",
				"one",
				"--dir",
				"./team-plugins",
			},
			remove: &plugins.RemoveOptions{
				Scope: commandio.ScopeOptions{
					Directory: new("./team-plugins"),
				},
				Plugins: []string{
					"one",
				},
			},
		},
		{
			name: "remove global",
			args: []string{
				"plugins",
				"remove",
				"one",
				"--scope",
				"global",
			},
			remove: &plugins.RemoveOptions{
				Scope: commandio.ScopeOptions{
					Global: true,
				},
				Plugins: []string{
					"one",
				},
			},
		},
		{
			name: "install requires scope",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
			},
			err: "--scope is required",
		},
		{
			name: "install directory does not bypass required scope",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
				"--dir",
				"./team-plugins",
			},
			err: "--scope is required",
		},
		{
			name: "install requires source",
			args: []string{
				"plugins",
				"install",
				"--scope",
				"project",
			},
			err: "accepts 1 arg",
		},
		{
			name: "install rejects extra sources",
			args: []string{
				"plugins",
				"install",
				"one/repo",
				"two/repo",
				"--scope",
				"project",
			},
			err: "accepts 1 arg",
		},
		{
			name: "list rejects extra sources",
			args: []string{
				"plugins",
				"list",
				"one/repo",
				"two/repo",
			},
			err: "accepts at most 1 arg",
		},
		{
			name: "remove requires names",
			args: []string{
				"plugins",
				"remove",
			},
			err: "requires at least 1 arg",
		},
		{
			name: "invalid install scope",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
				"--scope",
				"globla",
			},
			err: "--scope must be project or global",
		},
		{
			name: "invalid list scope",
			args: []string{
				"plugins",
				"list",
				"--scope",
				"globla",
			},
			err: "--scope must be project or global",
		},
		{
			name: "invalid remove scope",
			args: []string{
				"plugins",
				"remove",
				"one",
				"--scope",
				"globla",
			},
			err: "--scope must be project or global",
		},
		{
			name: "explicitly empty scope",
			args: []string{
				"plugins",
				"list",
				"--scope=",
			},
			err: "--scope must be project or global",
		},
		{
			name: "empty directory",
			args: []string{
				"plugins",
				"list",
				"--dir=",
			},
			err: "--dir must not be empty",
		},
		{
			name: "global scope conflicts with directory",
			args: []string{
				"plugins",
				"list",
				"--scope",
				"global",
				"--dir",
				"./team-plugins",
			},
			err: "--dir cannot be combined",
		},
		{
			name: "old misspelled depth flag is rejected",
			args: []string{
				"plugins",
				"install",
				"owner/repo",
				"--scope",
				"project",
				"--fuill-depth",
			},
			err: "unknown flag",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				root := &cobra.Command{
					Use:           "cpe",
					SilenceErrors: true,
					SilenceUsage:  true,
				}
				input := strings.NewReader("input")
				var output, diagnostics bytes.Buffer
				root.SetIn(input)
				root.SetOut(&output)
				root.SetErr(&diagnostics)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				operationErr := errors.New("operation failed")
				manager := plugins.NewManager(
					nil,
					afero.NewMemMapFs(),
				)
				calls := 0
				checkBoundary := func(actualCtx context.Context, actualManager *plugins.Manager, streams commandio.IO) {
					t.Helper()
					calls++
					if actualCtx != ctx || actualManager != manager || streams.In != input || streams.Out != &output || streams.Err != &diagnostics {
						t.Fatal("adapter did not forward command context, manager, and streams")
					}
				}
				pluginsCmdWithActions(
					root,
					manager,
					pluginActions{
						install: func(ctx context.Context, manager *plugins.Manager, opts plugins.InstallOptions, streams commandio.IO) error {
							checkBoundary(
								ctx,
								manager,
								streams,
							)
							if diff := cmp.Diff(
								test.install,
								&opts,
							); diff != "" {
								t.Fatalf(
									"install options (-want +got):\n%s",
									diff,
								)
							}
							return operationErr
						},
						list: func(ctx context.Context, manager *plugins.Manager, opts plugins.ListOptions, streams commandio.IO) error {
							checkBoundary(
								ctx,
								manager,
								streams,
							)
							if diff := cmp.Diff(
								test.list,
								&opts,
							); diff != "" {
								t.Fatalf(
									"list options (-want +got):\n%s",
									diff,
								)
							}
							return operationErr
						},
						remove: func(ctx context.Context, manager *plugins.Manager, opts plugins.RemoveOptions, streams commandio.IO) error {
							checkBoundary(
								ctx,
								manager,
								streams,
							)
							if diff := cmp.Diff(
								test.remove,
								&opts,
							); diff != "" {
								t.Fatalf(
									"remove options (-want +got):\n%s",
									diff,
								)
							}
							return operationErr
						},
					},
				)
				root.SetArgs(test.args)
				err := root.ExecuteContext(ctx)
				if test.err != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.err,
					) {
						t.Fatalf(
							"ExecuteContext returned %v, want error containing %q",
							err,
							test.err,
						)
					}
					if calls != 0 {
						t.Fatal("invalid invocation reached a plugin operation")
					}
					return
				}
				if !errors.Is(
					err,
					operationErr,
				) || calls != 1 {
					t.Fatalf(
						"ExecuteContext returned %v after %d operation calls, want operation error after one call",
						err,
						calls,
					)
				}
			},
		)
	}
}

func TestPluginsHelp(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "destinations",
			args: []string{
				"plugins",
				"--help",
			},
			want: []string{
				"~/.agents/plugins",
				"./.agents/plugins",
				"Only that directory is selected",
			},
		},
		{
			name: "install flags and examples",
			args: []string{
				"plugins",
				"install",
				"--help",
			},
			want: []string{
				"install SOURCE",
				"--full-depth",
				"--override",
				"replace the complete selected plugin directory",
				"--path",
				"source-relative plugin directory",
				"--path and --plugin are mutually exclusive",
				"--path .",
				"regardless of discovery depth",
				"--scope is required",
				"--json",
				"Output is a JSON array when stdout is not a terminal",
				"https://github.enterprise.com/my-company/custom-plugins --scope project",
			},
		},
		{
			name: "optional list source",
			args: []string{
				"plugins",
				"list",
				"--help",
			},
			want: []string{
				"list [SOURCE]",
				"including plugins copied manually or by another tool",
				"Component diagnostics",
				"--json",
			},
		},
		{
			name: "removal without requiring a valid manifest",
			args: []string{
				"plugins",
				"remove",
				"--help",
			},
			want: []string{
				"remove PLUGIN...",
				"including manually copied plugins",
				"without requiring a valid manifest",
				"--json",
				"Output is a JSON array when stdout is not a terminal",
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				root := Root(
					"test",
					nil,
					plugins.NewManager(
						nil,
						afero.NewMemMapFs(),
					),
					nil,
					auth.LoginOptions{},
				)
				var output bytes.Buffer
				root.SetOut(&output)
				root.SetErr(&output)
				root.SetArgs(test.args)
				if err := root.ExecuteContext(t.Context()); err != nil {
					t.Fatal(err)
				}
				for _, want := range test.want {
					if !strings.Contains(
						output.String(),
						want,
					) {
						t.Errorf(
							"help does not contain %q:\n%s",
							want,
							&output,
						)
					}
				}
			},
		)
	}
}

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

	"github.com/spachava753/cpe/internal/skills"
)

func TestSkillsCommands(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		args    []string
		install *skills.InstallOptions
		list    *skills.ListOptions
		update  *skills.UpdateOptions
		remove  *skills.RemoveOptions
		err     string
	}{
		{
			name: "install global",
			args: []string{"skills", "install", "owner/repo", "--scope", "global"},
			install: &skills.InstallOptions{
				Scope: skills.ScopeOptions{Global: true}, Source: "owner/repo",
			},
		},
		{
			name: "install alias with repeated skills and full depth",
			args: []string{"skills", "i", "owner/repo", "--scope", "project", "--skill", "one", "--skill", "two", "--full-depth"},
			install: &skills.InstallOptions{
				Scope: skills.ScopeOptions{Project: true}, Source: "owner/repo",
				Skills: []string{"one", "two"}, FullDepth: true,
			},
		},
		{
			name: "install custom directory with inherited scope before command",
			args: []string{"skills", "--scope", "project", "add", "owner/repo", "--dir", "./team-skills"},
			install: &skills.InstallOptions{
				Scope: skills.ScopeOptions{Directory: new("./team-skills"), Project: true}, Source: "owner/repo",
			},
		},
		{
			name: "list defaults",
			args: []string{"skills", "list"},
			list: &skills.ListOptions{},
		},
		{
			name: "list source",
			args: []string{"skills", "list", "owner/repo"},
			list: &skills.ListOptions{Source: new("owner/repo")},
		},
		{
			name: "list preserves explicitly empty source",
			args: []string{"skills", "list", ""},
			list: &skills.ListOptions{Source: new("")},
		},
		{
			name: "list scoped alias",
			args: []string{"skills", "ls", "--scope", "global"},
			list: &skills.ListOptions{Scope: skills.ScopeOptions{Global: true}},
		},
		{
			name: "list custom directory without scope",
			args: []string{"skills", "list", "--dir", "./team-skills"},
			list: &skills.ListOptions{Scope: skills.ScopeOptions{Directory: new("./team-skills")}},
		},
		{
			name:   "update defaults",
			args:   []string{"skills", "update"},
			update: &skills.UpdateOptions{Skills: []string{}},
		},
		{
			name: "update custom directory without scope",
			args: []string{"skills", "u", "one", "two", "--dir", "./team-skills"},
			update: &skills.UpdateOptions{
				Scope: skills.ScopeOptions{Directory: new("./team-skills")}, Skills: []string{"one", "two"},
			},
		},
		{
			name: "update explicit project",
			args: []string{"skills", "upgrade", "one", "--scope", "project"},
			update: &skills.UpdateOptions{
				Scope: skills.ScopeOptions{Project: true}, Skills: []string{"one"},
			},
		},
		{
			name:   "remove names without scope or records",
			args:   []string{"skills", "remove", "one", "two"},
			remove: &skills.RemoveOptions{Skills: []string{"one", "two"}},
		},
		{
			name: "remove custom directory without scope",
			args: []string{"skills", "rm", "one", "--dir", "./team-skills"},
			remove: &skills.RemoveOptions{
				Scope: skills.ScopeOptions{Directory: new("./team-skills")}, Skills: []string{"one"},
			},
		},
		{
			name: "remove global",
			args: []string{"skills", "remove", "one", "--scope", "global"},
			remove: &skills.RemoveOptions{
				Scope: skills.ScopeOptions{Global: true}, Skills: []string{"one"},
			},
		},
		{name: "install requires scope", args: []string{"skills", "install", "owner/repo"}, err: "--scope is required"},
		{name: "install directory does not bypass required scope", args: []string{"skills", "install", "owner/repo", "--dir", "./team-skills"}, err: "--scope is required"},
		{name: "install requires source", args: []string{"skills", "install", "--scope", "project"}, err: "accepts 1 arg"},
		{name: "install rejects extra sources", args: []string{"skills", "install", "one/repo", "two/repo", "--scope", "project"}, err: "accepts 1 arg"},
		{name: "list rejects extra sources", args: []string{"skills", "list", "one/repo", "two/repo"}, err: "accepts at most 1 arg"},
		{name: "remove requires names", args: []string{"skills", "remove"}, err: "requires at least 1 arg"},
		{name: "invalid install scope", args: []string{"skills", "install", "owner/repo", "--scope", "globla"}, err: "--scope must be project or global"},
		{name: "invalid list scope", args: []string{"skills", "list", "--scope", "globla"}, err: "--scope must be project or global"},
		{name: "invalid update scope", args: []string{"skills", "update", "--scope", "globla"}, err: "--scope must be project or global"},
		{name: "invalid remove scope", args: []string{"skills", "remove", "one", "--scope", "globla"}, err: "--scope must be project or global"},
		{name: "explicitly empty scope", args: []string{"skills", "update", "--scope="}, err: "--scope must be project or global"},
		{name: "empty directory", args: []string{"skills", "update", "--dir="}, err: "--dir must not be empty"},
		{name: "global scope conflicts with directory", args: []string{"skills", "update", "--scope", "global", "--dir", "./team-skills"}, err: "--dir cannot be combined"},
		{name: "old misspelled depth flag is rejected", args: []string{"skills", "install", "owner/repo", "--scope", "project", "--fuill-depth"}, err: "unknown flag"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := &cobra.Command{Use: "cpe", SilenceErrors: true, SilenceUsage: true}
			input := strings.NewReader("input")
			var output, diagnostics bytes.Buffer
			root.SetIn(input)
			root.SetOut(&output)
			root.SetErr(&diagnostics)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			operationErr := errors.New("operation failed")
			manager := skills.NewManager(nil, afero.NewMemMapFs())
			calls := 0
			checkBoundary := func(actualCtx context.Context, actualManager *skills.Manager, streams skills.IO) {
				t.Helper()
				calls++
				if actualCtx != ctx || actualManager != manager || streams.In != input || streams.Out != &output || streams.Err != &diagnostics {
					t.Fatal("adapter did not forward command context, manager, and streams")
				}
			}
			skillsCmdWithActions(root, manager, skillActions{
				install: func(ctx context.Context, manager *skills.Manager, opts skills.InstallOptions, streams skills.IO) error {
					checkBoundary(ctx, manager, streams)
					if diff := cmp.Diff(test.install, &opts); diff != "" {
						t.Fatalf("install options (-want +got):\n%s", diff)
					}
					return operationErr
				},
				list: func(ctx context.Context, manager *skills.Manager, opts skills.ListOptions, streams skills.IO) error {
					checkBoundary(ctx, manager, streams)
					if diff := cmp.Diff(test.list, &opts); diff != "" {
						t.Fatalf("list options (-want +got):\n%s", diff)
					}
					return operationErr
				},
				update: func(ctx context.Context, manager *skills.Manager, opts skills.UpdateOptions, streams skills.IO) error {
					checkBoundary(ctx, manager, streams)
					if diff := cmp.Diff(test.update, &opts); diff != "" {
						t.Fatalf("update options (-want +got):\n%s", diff)
					}
					return operationErr
				},
				remove: func(ctx context.Context, manager *skills.Manager, opts skills.RemoveOptions, streams skills.IO) error {
					checkBoundary(ctx, manager, streams)
					if diff := cmp.Diff(test.remove, &opts); diff != "" {
						t.Fatalf("remove options (-want +got):\n%s", diff)
					}
					return operationErr
				},
			})
			root.SetArgs(test.args)
			err := root.ExecuteContext(ctx)
			if test.err != "" {
				if err == nil || !strings.Contains(err.Error(), test.err) {
					t.Fatalf("ExecuteContext returned %v, want error containing %q", err, test.err)
				}
				if calls != 0 {
					t.Fatal("invalid invocation reached a skill operation")
				}
				return
			}
			if !errors.Is(err, operationErr) || calls != 1 {
				t.Fatalf("ExecuteContext returned %v after %d operation calls, want operation error after one call", err, calls)
			}
		})
	}
}

func TestSkillsHelp(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "destinations and metadata",
			args: []string{"skills", "--help"},
			want: []string{"~/.agents/skills/skills.json", "./.agents/skills/skills.json", "<directory>/skills.json", "Only that directory is selected"},
		},
		{
			name: "install flags and examples",
			args: []string{"skills", "install", "--help"},
			want: []string{"install SOURCE", "--full-depth", "--scope is required", "https://github.enterprise.com/my-company/custom-skills --scope project"},
		},
		{
			name: "optional list source",
			args: []string{"skills", "list", "--help"},
			want: []string{"list [SOURCE]", "including skills not installed or managed by CPE"},
		},
		{
			name: "directory-only update",
			args: []string{"skills", "update", "--help"},
			want: []string{"update [SKILL...]", "global skills are not included"},
		},
		{
			name: "unmanaged removal",
			args: []string{"skills", "remove", "--help"},
			want: []string{"remove SKILL...", "including unmanaged, manually copied skills", "a record is not required"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := Root("test", skills.NewManager(nil, afero.NewMemMapFs()))
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(test.args)
			if err := root.ExecuteContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(output.String(), want) {
					t.Errorf("help does not contain %q:\n%s", want, &output)
				}
			}
		})
	}
}

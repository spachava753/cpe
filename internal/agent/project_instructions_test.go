package agent

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/models"
)

type projectInstructionsFS struct {
	afero.Fs
	denied map[string]bool
	onOpen func(string)
}

func (f projectInstructionsFS) Open(path string) (afero.File, error) {
	if f.onOpen != nil {
		f.onOpen(path)
	}
	if f.denied[path] {
		return nil, &fs.PathError{
			Op:   "open",
			Path: path,
			Err:  fs.ErrPermission,
		}
	}
	return f.Fs.Open(path)
}

func writeProjectFile(t *testing.T, filesystem afero.Fs, path, content string) {
	t.Helper()
	if err := filesystem.MkdirAll(
		filepath.Dir(path),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(
		filesystem,
		path,
		[]byte(content),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectInstructions(t *testing.T) {
	for _, test := range []struct {
		name      string
		files     map[string]string
		denied    map[string]bool
		cancelAt  string
		cancelled bool
		want      []string
		wantEmpty bool
		wantError error
	}{
		{
			name: "no instruction files",
			files: map[string]string{
				"/AGENTS.md":          "Ancestor instructions must not be read.",
				"/project/readme.txt": "ordinary project file",
			},
			denied: map[string]bool{
				"/AGENTS.md":          true,
				"/project/readme.txt": true,
			},
			wantEmpty: true,
		},
		{
			name: "root contents are verbatim template data",
			files: map[string]string{
				"/project/AGENTS.md": "Keep this spacing.\n\n{{.Nested}} is literal text.\n",
			},
			want: []string{
				`"/project/AGENTS.md"`,
				"Keep this spacing.\n\n{{.Nested}} is literal text.\n",
			},
		},
		{
			name: "nested paths without a root file",
			files: map[string]string{
				"/project/z/AGENTS.md":       "nested body must not load",
				"/project/a/deep/AGENTS.md":  "nested body must not load",
				"/project/.hidden/AGENTS.md": "nested body must not load",
				"/project/.git/AGENTS.md":    "Git metadata is not project instructions",
				"/project/.gitignore":        "*",
			},
			denied: map[string]bool{
				"/project/z/AGENTS.md":       true,
				"/project/a/deep/AGENTS.md":  true,
				"/project/.hidden/AGENTS.md": true,
				"/project/.git":              true,
			},
			want: []string{
				`- "/project/.hidden/AGENTS.md"`,
				`- "/project/a/deep/AGENTS.md"`,
				`- "/project/z/AGENTS.md"`,
			},
		},
		{
			name: "root and nested instructions",
			files: map[string]string{
				"/project/AGENTS.md":          "Root guidance.",
				"/project/sub/AGENTS.md":      "nested body must not load",
				"/project/sub/deep/AGENTS.md": "nested body must not load",
			},
			want: []string{
				"Root guidance.",
				`- "/project/sub/AGENTS.md"`,
				`- "/project/sub/deep/AGENTS.md"`,
			},
		},
		{
			name: "empty root instructions",
			files: map[string]string{
				"/project/AGENTS.md": "",
			},
			want: []string{
				`"/project/AGENTS.md"`,
			},
		},
		{
			name: "root read failure",
			files: map[string]string{
				"/project/AGENTS.md": "Root guidance.",
			},
			denied: map[string]bool{
				"/project/AGENTS.md": true,
			},
			wantError: fs.ErrPermission,
		},
		{
			name: "directory scan failure",
			files: map[string]string{
				"/project/sub/AGENTS.md": "nested body must not load",
			},
			denied: map[string]bool{
				"/project/sub": true,
			},
			wantError: fs.ErrPermission,
		},
		{
			name:      "cancelled before discovery",
			cancelled: true,
			wantError: context.Canceled,
		},
		{
			name: "cancelled while reading root instructions",
			files: map[string]string{
				"/project/AGENTS.md": "Root guidance.",
			},
			cancelAt:  "/project/AGENTS.md",
			wantError: context.Canceled,
		},
		{
			name: "cancelled during discovery",
			files: map[string]string{
				"/project/sub/AGENTS.md": "nested body must not load",
			},
			cancelAt:  "/project/sub",
			wantError: context.Canceled,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				if err := filesystem.MkdirAll(
					"/project",
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				for path, content := range test.files {
					writeProjectFile(
						t,
						filesystem,
						path,
						content,
					)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancelled {
					cancel()
				}
				instructions, err := loadProjectInstructions(
					ctx,
					projectInstructionsFS{
						Fs:     filesystem,
						denied: test.denied,
						onOpen: func(path string) {
							if path == test.cancelAt {
								cancel()
							}
						},
					},
					"/project",
				)
				if !errors.Is(
					err,
					test.wantError,
				) {
					t.Fatalf(
						"load: %v",
						err,
					)
				}
				if err != nil {
					if instructions != "" {
						t.Fatal("failed discovery returned a partial instruction set")
					}
					return
				}
				if test.wantEmpty && instructions != "" {
					t.Fatal(instructions)
				}
				previous := -1
				for _, want := range test.want {
					index := strings.Index(
						instructions,
						want,
					)
					if index <= previous {
						t.Fatalf(
							"missing or out-of-order %q in %q",
							want,
							instructions,
						)
					}
					previous = index
				}
				if strings.Contains(
					instructions,
					"nested body must not load",
				) {
					t.Fatal("nested instruction body was included")
				}
			},
		)
	}
}

func TestProjectInstructionLinks(t *testing.T) {
	for _, mode := range []string{
		"file links",
		"directory cycle",
		"linked session directory",
	} {
		t.Run(
			mode,
			func(t *testing.T) {
				directory := t.TempDir()
				root := filepath.Join(
					directory,
					"project",
				)
				filesystem := afero.NewOsFs()
				nested := filepath.Join(
					root,
					"directory with spaces",
					"AGENTS.md",
				)
				writeProjectFile(
					t,
					filesystem,
					nested,
					"nested body must not load",
				)
				rootFile := filepath.Join(
					root,
					"AGENTS.md",
				)
				writeProjectFile(
					t,
					filesystem,
					rootFile,
					"Root guidance.",
				)
				var target, link string
				switch mode {
				case "file links":
					target = rootFile
					link = nested
					if err := filesystem.Remove(nested); err != nil {
						t.Fatal(err)
					}
				case "directory cycle":
					target = root
					link = filepath.Join(
						root,
						"cycle",
					)
				case "linked session directory":
					target = root
					link = filepath.Join(
						directory,
						"linked",
					)
					root = link
					nested = filepath.Join(
						root,
						"directory with spaces",
						"AGENTS.md",
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
				instructions, err := loadProjectInstructions(
					t.Context(),
					filesystem,
					root,
				)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(
					instructions,
					"Root guidance.",
				) || !strings.Contains(
					instructions,
					"- "+strconv.Quote(nested),
				) || strings.Count(
					instructions,
					"- ",
				) != 1 {
					t.Fatal(instructions)
				}
			},
		)
	}
}

func TestSessionProjectInstructions(t *testing.T) {
	for _, operation := range []string{
		"live session",
		"load",
		"resume",
		"fork",
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				prompts := make(
					chan gai.GenerationRequest,
					2,
				)
				var filesystem afero.Fs
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generatorFunc(func(_ context.Context, request gai.GenerationRequest) (gai.Response, error) {
							prompts <- request
							return gai.Response{
								Candidates: []gai.Message{
									{
										Role: gai.Assistant,
										Blocks: []gai.Block{
											gai.TextBlock("done"),
										},
									},
								},
								FinishReason: gai.EndTurn,
							}, nil
						}), gai.GenerationOptions{}, nil
					},
					func(config *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						config.Instructions = "Custom base instructions."
						filesystem = deps.Filesystem
						writeProjectFile(
							t,
							filesystem,
							"/work/AGENTS.md",
							"Initial root guidance.",
						)
						writeProjectFile(
							t,
							filesystem,
							"/work/sub/AGENTS.md",
							"nested body must not load",
						)
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				promptFixture(
					t,
					fixture.client,
					id,
					"first turn",
				)
				first := <-prompts
				if first.Instructions.Role != gai.System || !strings.Contains(
					first.Instructions.Blocks[0].Content.String(),
					"Initial root guidance.",
				) || len(first.Dialog) != 1 || first.Dialog[0].Blocks[0].Content.String() != "first turn" {
					t.Fatal("project instructions were not supplied as system context")
				}
				writeProjectFile(
					t,
					filesystem,
					"/work/AGENTS.md",
					"Updated root guidance.",
				)
				writeProjectFile(
					t,
					filesystem,
					"/work/new/AGENTS.md",
					"nested body must not load",
				)
				want := "Updated root guidance."
				target := id
				switch operation {
				case "live session":
					want = "Initial root guidance."
				case "fork":
					fork, err := fixture.client.ForkSession(
						t.Context(),
						&acp.ForkSessionRequest{
							SessionID: id,
							Cwd:       "/work",
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					target = fork.SessionID
				case "load", "resume":
					_, err := fixture.client.CloseSession(
						t.Context(),
						&acp.CloseSessionRequest{
							SessionID: id,
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					if operation == "load" {
						_, err = fixture.client.LoadSession(
							t.Context(),
							&acp.LoadSessionRequest{
								SessionID: id,
								Cwd:       "/work",
							},
						)
					} else {
						_, err = fixture.client.ResumeSession(
							t.Context(),
							&acp.ResumeSessionRequest{
								SessionID: id,
								Cwd:       "/work",
							},
						)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				promptFixture(
					t,
					fixture.client,
					target,
					"second turn",
				)
				second := <-prompts
				text := second.Instructions.Blocks[0].Content.String()
				for _, part := range []string{
					"Custom base instructions.",
					want,
					`- "/work/sub/AGENTS.md"`,
				} {
					if !strings.Contains(
						text,
						part,
					) {
						t.Fatal(text)
					}
				}
				if strings.Contains(
					text,
					`- "/work/new/AGENTS.md"`,
				) != (operation != "live session") || strings.Contains(
					text,
					"nested body must not load",
				) {
					t.Fatal(text)
				}
				record, err := fixture.store.Load(
					t.Context(),
					SessionID(target),
				)
				if err != nil || len(record.Dialog) != 4 {
					t.Fatalf(
						"project instructions changed saved dialog: %#v, %v",
						record,
						err,
					)
				}
			},
		)
	}
}

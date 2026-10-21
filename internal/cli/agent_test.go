package cli

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/auth"
)

type unreadablePromptFS struct {
	afero.Fs
	path string
}

func (f unreadablePromptFS) Open(name string) (afero.File, error) {
	if name == f.path {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fs.ErrPermission,
		}
	}
	return f.Fs.Open(name)
}

func TestACPCommand(t *testing.T) {
	const model = `{"id":"fixture","base_url":"http://localhost:1234/v1/","api":"Openai","input_modalities":["text"],"output_modalities":["text"],"limit":8192,"max_tokens":256}`
	for _, test := range []struct {
		name             string
		args             []string
		config           string
		wantError        string
		wantRetries      uint
		wantSetupTimeout time.Duration
		promptFiles      map[string]string
		unreadablePrompt string
		wantInstructions string
	}{
		{
			name:             "default roots",
			config:           "[" + model + "]",
			wantRetries:      2,
			wantInstructions: defaultSystemPrompt + "\n\n" + defaultCompactionPrompt,
		},
		{
			name: "first configured model supplies the bootstrap default",
			config: "[" + model + "," + strings.ReplaceAll(
				model,
				`"fixture"`,
				`"aaa"`,
			) + "]",
			wantRetries:      2,
			wantInstructions: defaultSystemPrompt + "\n\n" + defaultCompactionPrompt,
		},
		{
			name: "custom setup timeout",
			args: []string{
				"--setup-timeout",
				"45s",
			},
			config:           "[" + model + "]",
			wantRetries:      2,
			wantSetupTimeout: 45 * time.Second,
			wantInstructions: defaultSystemPrompt + "\n\n" + defaultCompactionPrompt,
		},
		{
			name: "retries disabled",
			args: []string{
				"--response-retries",
				"0",
			},
			config:           "[" + model + "]",
			wantRetries:      0,
			wantInstructions: defaultSystemPrompt + "\n\n" + defaultCompactionPrompt,
		},
		{
			name: "relative additional roots",
			args: []string{
				"--skills",
				"extra-skills",
				"--plugins",
				"extra-plugins",
			},
			config:           "[" + model + "]",
			wantRetries:      2,
			wantInstructions: defaultSystemPrompt + "\n\n" + defaultCompactionPrompt,
		},
		{
			name:        "custom system prompt",
			config:      "[" + model + "]",
			wantRetries: 2,
			promptFiles: map[string]string{
				"SYSTEM.md": "Custom instructions.\n",
			},
			wantInstructions: "Custom instructions.\n" + "\n\n" + defaultCompactionPrompt,
		},
		{
			name:        "append to built-in prompt",
			config:      "[" + model + "]",
			wantRetries: 2,
			promptFiles: map[string]string{
				"APPEND-SYSTEM.md": "Additional instructions.\n",
			},
			wantInstructions: defaultSystemPrompt + "\n\nAdditional instructions.\n" + "\n\n" + defaultCompactionPrompt,
		},
		{
			name:        "append to custom prompt",
			config:      "[" + model + "]",
			wantRetries: 2,
			promptFiles: map[string]string{
				"SYSTEM.md":        "Custom instructions.",
				"APPEND-SYSTEM.md": "Additional instructions.",
			},
			wantInstructions: "Custom instructions.\n\nAdditional instructions." + "\n\n" + defaultCompactionPrompt,
		},
		{
			name:        "empty custom prompt replaces default",
			config:      "[" + model + "]",
			wantRetries: 2,
			promptFiles: map[string]string{
				"SYSTEM.md": "",
			},
			wantInstructions: defaultCompactionPrompt,
		},
		{
			name:        "append to empty custom prompt",
			config:      "[" + model + "]",
			wantRetries: 2,
			promptFiles: map[string]string{
				"SYSTEM.md":        "",
				"APPEND-SYSTEM.md": "Additional instructions.",
			},
			wantInstructions: "Additional instructions." + "\n\n" + defaultCompactionPrompt,
		},
		{
			name:        "empty append preserves default",
			config:      "[" + model + "]",
			wantRetries: 2,
			promptFiles: map[string]string{
				"APPEND-SYSTEM.md": "",
			},
			wantInstructions: defaultSystemPrompt + "\n\n" + defaultCompactionPrompt,
		},
		{
			name:             "system prompt read failure",
			config:           "[" + model + "]",
			unreadablePrompt: "SYSTEM.md",
			wantError:        `read system prompt "/config/SYSTEM.md"`,
		},
		{
			name:             "append prompt read failure",
			config:           "[" + model + "]",
			unreadablePrompt: "APPEND-SYSTEM.md",
			wantError:        `read system prompt "/config/APPEND-SYSTEM.md"`,
		},
		{
			name:        "custom compaction guidance",
			config:      "[" + model + "]",
			wantRetries: 2,
			promptFiles: map[string]string{
				"COMPACTION.md": "Preserve the current goal and blockers.",
			},
			wantInstructions: defaultSystemPrompt + "\n\nPreserve the current goal and blockers.",
		},
		{
			name:        "custom compaction guidance after custom system and append",
			config:      "[" + model + "]",
			wantRetries: 2,
			promptFiles: map[string]string{
				"SYSTEM.md":        "Custom base.",
				"APPEND-SYSTEM.md": "Extra instructions.",
				"COMPACTION.md":    "Custom summary guidance.",
			},
			wantInstructions: "Custom base.\n\nExtra instructions.\n\nCustom summary guidance.",
		},
		{
			name:        "empty compaction file replaces default guidance",
			config:      "[" + model + "]",
			wantRetries: 2,
			promptFiles: map[string]string{
				"COMPACTION.md": "",
			},
			wantInstructions: defaultSystemPrompt,
		},
		{
			name:             "compaction guidance read failure",
			config:           "[" + model + "]",
			unreadablePrompt: "COMPACTION.md",
			wantError:        `read compaction instructions "/config/COMPACTION.md"`,
		},
		{
			name:      "empty model catalog",
			config:    "[]",
			wantError: "at least one model must be configured",
		},
		{
			name:      "invalid models",
			config:    "{bad",
			wantError: "decode models",
		},
		{
			name:      "duplicate model IDs",
			config:    "[" + model + "," + model + "]",
			wantError: "duplicate model ID",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				home := t.TempDir()
				t.Setenv(
					"HOME",
					home,
				)
				t.Setenv(
					"USERPROFILE",
					home,
				)
				filesystem := afero.NewMemMapFs()
				if err := filesystem.MkdirAll(
					"/config",
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := afero.WriteFile(
					filesystem,
					"/config/models.json",
					[]byte(test.config),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				for name, content := range test.promptFiles {
					if err := afero.WriteFile(
						filesystem,
						filepath.Join(
							"/config",
							name,
						),
						[]byte(content),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				if test.unreadablePrompt != "" {
					filesystem = unreadablePromptFS{
						Fs: filesystem,
						path: filepath.Join(
							"/config",
							test.unreadablePrompt,
						),
					}
				}
				root := &cobra.Command{
					Use:           "cpe",
					SilenceErrors: true,
					SilenceUsage:  true,
				}
				root.SetOut(io.Discard)
				var stderr bytes.Buffer
				root.SetErr(&stderr)
				called := false
				acpCmdWithServe(
					root,
					filesystem,
					auth.NewStore(filesystem),
					auth.LoginOptions{
						Path: "/config/auth.json",
					},
					func(_ context.Context, _ acp.Transport, config agent.Config, deps agent.Dependencies) error {
						called = true
						if config.Instructions != test.wantInstructions {
							t.Fatalf(
								"instructions: got %q, want %q",
								config.Instructions,
								test.wantInstructions,
							)
						}
						if deps.Stderr != &stderr {
							t.Fatal("diagnostics are not wired to command stderr")
						}
						wantSetupTimeout := test.wantSetupTimeout
						if wantSetupTimeout == 0 {
							wantSetupTimeout = 10 * time.Minute
						}
						if config.SetupTimeout != wantSetupTimeout {
							t.Fatalf(
								"setup timeout: got %s, want %s",
								config.SetupTimeout,
								wantSetupTimeout,
							)
						}
						if config.MaxResponseRetries != test.wantRetries {
							t.Fatalf(
								"response retries: %d",
								config.MaxResponseRetries,
							)
						}
						if config.Initial.Model != "fixture" || deps.Generators == nil || deps.Hosts == nil || deps.MCP == nil || deps.History == nil || deps.LastModel == nil {
							t.Fatal("incomplete runtime wiring")
						}
						if config.SkillPaths[0] != filepath.Join(
							home,
							".agents",
							"skills",
						) || config.PluginPaths[0] != filepath.Join(
							home,
							".agents",
							"plugins",
						) {
							t.Fatal("global install roots missing")
						}
						if test.name == "relative additional roots" {
							want, err := filepath.Abs("extra-plugins")
							if err != nil {
								t.Fatal(err)
							}
							if len(config.PluginPaths) != 2 || config.PluginPaths[1] != want || !filepath.IsAbs(config.SkillPaths[1]) {
								t.Fatalf(
									"relative roots not normalized: %v %v",
									config.SkillPaths,
									config.PluginPaths,
								)
							}
						}
						return nil
					},
				)
				root.SetArgs(append(
					[]string{
						"acp",
					},
					test.args...,
				))
				err := root.ExecuteContext(t.Context())
				if test.wantError == "" {
					if err != nil || !called {
						t.Fatalf(
							"execute: %v, served=%v",
							err,
							called,
						)
					}
				} else if err == nil || !strings.Contains(
					err.Error(),
					test.wantError,
				) || called {
					t.Fatalf(
						"execute: %v, served=%v",
						err,
						called,
					)
				}
			},
		)
	}
}

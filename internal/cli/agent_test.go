package cli

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/auth"
)

func TestACPCommand(t *testing.T) {
	const model = `{"id":"fixture","base_url":"http://localhost:1234/v1/","api":"Openai","input_modalities":["text"],"output_modalities":["text"],"limit":8192,"max_tokens":256}`
	for _, test := range []struct {
		name      string
		args      []string
		config    string
		wantError string
	}{
		{
			name: "default roots",
			args: []string{
				"--model",
				"fixture",
			},
			config: "[" + model + "]",
		},
		{
			name: "relative additional roots",
			args: []string{
				"--model",
				"fixture",
				"--skills",
				"extra-skills",
				"--plugins",
				"extra-plugins",
			},
			config: "[" + model + "]",
		},
		{
			name:      "missing model",
			wantError: "--model is required",
		},
		{
			name: "invalid models",
			args: []string{
				"--model",
				"fixture",
			},
			config:    "{bad",
			wantError: "decode models",
		},
		{
			name: "duplicate model IDs",
			args: []string{
				"--model",
				"fixture",
			},
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
				root := &cobra.Command{
					Use:           "cpe",
					SilenceErrors: true,
					SilenceUsage:  true,
				}
				root.SetOut(io.Discard)
				root.SetErr(io.Discard)
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
						if config.Initial.Model != "fixture" || deps.Generators == nil || deps.Hosts == nil || deps.MCP == nil || deps.History == nil {
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

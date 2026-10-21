package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/providers"
	"github.com/spachava753/cpe/internal/toolruntime"
)

type serveAgent func(context.Context, acp.Transport, agent.Config, agent.Dependencies) error

func acpCmd(root *cobra.Command, filesystem afero.Fs, store *auth.Store, authOptions auth.LoginOptions) {
	acpCmdWithServe(
		root,
		filesystem,
		store,
		authOptions,
		agent.Serve,
	)
}

func acpCmdWithServe(root *cobra.Command, filesystem afero.Fs, store *auth.Store, authOptions auth.LoginOptions, serve serveAgent) {
	var modelFile, historyPath, effort string
	var timeout time.Duration
	var responseRetries uint
	var skillPaths, pluginPaths []string
	command := &cobra.Command{
		Use:   "acp",
		Short: "Serve one ACP client over stdin/stdout",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if authOptions.Path == "" {
				return errors.New("CPE configuration directory is unavailable")
			}
			instructions, err := loadSystemPrompt(
				filesystem,
				filepath.Dir(authOptions.Path),
			)
			if err != nil {
				return err
			}
			if modelFile == "" {
				modelFile = filepath.Join(
					filepath.Dir(authOptions.Path),
					"models.json",
				)
			}
			if historyPath == "" {
				historyPath = filepath.Join(
					filepath.Dir(authOptions.Path),
					"sessions",
				)
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf(
					"resolve global discovery roots: %w",
					err,
				)
			}
			skillPaths = append(
				[]string{
					filepath.Join(
						home,
						".agents",
						"skills",
					),
				},
				skillPaths...,
			)
			pluginPaths = append(
				[]string{
					filepath.Join(
						home,
						".agents",
						"plugins",
					),
				},
				pluginPaths...,
			)
			for _, roots := range [][]string{
				skillPaths,
				pluginPaths,
			} {
				for i, root := range roots {
					roots[i], err = filepath.Abs(root)
					if err != nil {
						return err
					}
				}
			}
			file, err := filesystem.Open(modelFile)
			if err != nil {
				return err
			}
			configured, loadErr := models.LoadModels(file)
			if err := errors.Join(
				loadErr,
				file.Close(),
			); err != nil {
				return err
			}
			if len(configured) == 0 {
				return errors.New("at least one model must be configured")
			}
			catalog := make(map[models.ID]models.Model)
			for _, model := range configured {
				id := models.ID(model.ID)
				if _, exists := catalog[id]; exists {
					return fmt.Errorf(
						"duplicate model ID %q",
						id,
					)
				}
				catalog[id] = model
			}
			lastModel, err := models.NewLastUsed(filepath.Join(
				filepath.Dir(authOptions.Path),
				"last-model.json",
			))
			if err != nil {
				return err
			}
			history, err := conversation.NewStore(historyPath)
			if err != nil {
				return err
			}
			providerSet, err := providers.New(
				store,
				authOptions.Path,
				&http.Client{
					CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
				},
			)
			if err != nil {
				return err
			}
			defer providerSet.Close()
			return serve(
				command.Context(),
				&acp.StdioTransport{},
				agent.Config{
					Models: catalog,
					Initial: models.Selection{
						Model:     models.ID(configured[0].ID),
						Reasoning: models.ReasoningLevel(effort),
					},
					SkillPaths:         skillPaths,
					PluginPaths:        pluginPaths,
					SetupTimeout:       timeout,
					MaxResponseRetries: responseRetries,
					Instructions:       instructions,
				},
				agent.Dependencies{
					Filesystem:     filesystem,
					Authentication: providerSet,
					Generators:     providerSet.Generator,
					Hosts:          toolruntime.Local,
					MCP:            mcpclient.Connect,
					History:        history,
					LastModel:      lastModel,
					Traces:         otel.GetTracerProvider(),
					Stderr:         command.ErrOrStderr(),
				},
			)
		},
	}
	command.Flags().StringVar(
		&effort,
		"reasoning",
		"",
		"Initial reasoning effort (empty uses provider default)",
	)
	command.Flags().StringVar(
		&modelFile,
		"models",
		"",
		"Model configuration file (default ~/.cpe/models.json)",
	)
	command.Flags().StringVar(
		&historyPath,
		"sessions",
		"",
		"Absolute checkpoint directory (default ~/.cpe/sessions)",
	)
	command.Flags().DurationVar(
		&timeout,
		"setup-timeout",
		10*time.Minute,
		"Maximum duration of session setup",
	)
	command.Flags().UintVar(
		&responseRetries,
		"response-retries",
		2,
		"Maximum retries of one interrupted model response (zero disables)",
	)
	command.Flags().StringSliceVar(
		&skillPaths,
		"skills",
		nil,
		"Additional skill roots; project .agents/skills is always loaded",
	)
	command.Flags().StringSliceVar(
		&pluginPaths,
		"plugins",
		nil,
		"Additional plugin roots; project .agents/plugins is always loaded",
	)
	root.AddCommand(command)
}

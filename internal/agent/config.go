package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"time"

	"github.com/spachava753/gai"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel/trace"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/toolruntime"
)

// Config supplies models, discovery paths, and execution limits.
// Callers must not mutate referenced values until Serve returns.
type Config struct {
	Models map[models.ID]models.Model
	// Initial supplies the fallback model and the reasoning effort for new sessions.
	Initial      models.Selection
	SkillPaths   []string
	PluginPaths  []string
	Instructions string
	SetupTimeout time.Duration
	// MaxResponseRetries limits retries of one interrupted model response; zero disables retries.
	MaxResponseRetries uint
}

// Dependencies supplies concurrency-safe services borrowed until Serve returns.
// All fields are required; an explicit no-op tracer provider is supported.
type Dependencies struct {
	Filesystem     afero.Fs
	Authentication Authenticator
	Generators     GeneratorFactory
	Hosts          toolruntime.Factory
	MCP            mcpclient.Connector
	History        conversation.Store
	LastModel      *models.LastUsed
	Traces         trace.TracerProvider
	// Stderr receives nonfatal diagnostics, separate from ACP output.
	Stderr io.Writer
}

// Authenticator verifies credentials established by the separate terminal login flow.
type Authenticator interface {
	Authenticate(context.Context, auth.Provider) error
	Logout(context.Context) error
}

// GeneratorFactory constructs a generator and options using current credentials.
type GeneratorFactory func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error)

func checkConfig(config Config, deps Dependencies) error {
	if len(config.Models) == 0 || config.SetupTimeout <= 0 {
		return errors.New("models and a positive session setup timeout are required")
	}
	if deps.Filesystem == nil || deps.Authentication == nil || deps.Generators == nil || deps.Hosts == nil || deps.MCP == nil || deps.History == nil || deps.LastModel == nil || deps.Traces == nil || deps.Stderr == nil {
		return errors.New("all agent dependencies must be configured")
	}
	for _, roots := range [][]string{
		config.SkillPaths,
		config.PluginPaths,
	} {
		for _, root := range roots {
			if !filepath.IsAbs(root) {
				return fmt.Errorf(
					"discovery root must be absolute: %q",
					root,
				)
			}
		}
	}
	for id, model := range config.Models {
		if id == "" || model.ID == "" || len(model.InputModalities) == 0 {
			return errors.New("model catalog contains an incomplete model")
		}
	}
	return checkOptions(
		config.Models,
		models.Selection{
			Model: config.Initial.Model,
		},
	)
}

func checkOptions(catalog map[models.ID]models.Model, options models.Selection) error {
	model, ok := catalog[options.Model]
	if !ok {
		return fmt.Errorf(
			"unknown model %q",
			options.Model,
		)
	}
	if options.Reasoning != "" && !slices.Contains(
		model.ReasoningLevels,
		string(options.Reasoning),
	) {
		return fmt.Errorf(
			"model %q does not support reasoning effort %q",
			options.Model,
			options.Reasoning,
		)
	}
	return nil
}

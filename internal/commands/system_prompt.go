package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
)

// LoadSystemPromptOptions contains parameters for loading a system prompt
type LoadSystemPromptOptions struct {
	// SystemPromptPath is the path to the system prompt file
	SystemPromptPath string
	// Config is the effective configuration for template rendering
	Config *config.Config
	// Stderr is where template warnings are written
	Stderr io.Writer
}

// LoadSystemPrompt loads and renders a system prompt template.
// Returns empty string if path is empty.
func LoadSystemPrompt(ctx context.Context, opts LoadSystemPromptOptions) (string, error) {
	if opts.SystemPromptPath == "" {
		return "", nil
	}

	f, err := os.Open(opts.SystemPromptPath)
	if err != nil {
		return "", fmt.Errorf("could not open system prompt file %q: %w", opts.SystemPromptPath, err)
	}
	defer f.Close()

	contents, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("failed to read system prompt file %q: %w", opts.SystemPromptPath, err)
	}

	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	rendered, err := agent.SystemPromptTemplate(ctx, string(contents), agent.TemplateData{
		Config: opts.Config,
	}, stderr)
	if err != nil {
		return "", fmt.Errorf("failed to render system prompt: %w", err)
	}

	return rendered, nil
}

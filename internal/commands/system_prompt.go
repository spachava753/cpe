package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/prompt"
	"github.com/spachava753/cpe/internal/skills"
)

// LoadSystemPromptOptions contains parameters for loading a system prompt
type LoadSystemPromptOptions struct {
	// SystemPromptPath is the path to the system prompt file
	SystemPromptPath string
	// Config is the effective configuration for template rendering
	Config config.Config
	// Skills is the already-filtered model-visible skill metadata exposed to the
	// prompt template through TemplateData.Skills. Callers should omit skills with
	// disable-model-invocation: true unless they intentionally want those skills
	// visible in the rendered system prompt.
	Skills []skills.Skill
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

	rendered, err := prompt.SystemPromptTemplate(ctx, string(contents), prompt.TemplateData{
		Config: opts.Config,
		Skills: opts.Skills,
	})
	if err != nil {
		return "", fmt.Errorf("failed to render system prompt: %w", err)
	}

	return rendered, nil
}

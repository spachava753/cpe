package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	oopts "github.com/openai/openai-go/v3/option"
	"github.com/spachava753/gai"
	"google.golang.org/genai"

	"github.com/spachava753/cpe/internal/codex"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/responses"
)

const codexProvider = "codex"

// Provider builds a gai generator with explicit credentials and no SDK retries.
func Provider(ctx context.Context, m config.Model) (gai.Generator, error) {
	if m.Provider == codexProvider {
		dir, err := config.Directory()
		if err != nil {
			return nil, err
		}
		return codex.New(filepath.Join(dir, "auth.json")), nil
	}
	key := os.Getenv(m.APIKeyEnv)
	if key == "" {
		return nil, fmt.Errorf("set %s for model %s", m.APIKeyEnv, m.ID)
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	switch m.Provider {
	case "openai":
		return gai.NewOpenAiGenerator(client, m.BaseURL, key)
	case "responses":
		opts := []oopts.RequestOption{oopts.WithAPIKey(key), oopts.WithHTTPClient(client), oopts.WithMaxRetries(0)}
		if m.BaseURL != "" {
			opts = append(opts, oopts.WithBaseURL(m.BaseURL))
		}
		c := openai.NewClient(opts...)
		return responses.New(&c.Responses), nil
	case "anthropic":
		opts := []aopts.RequestOption{aopts.WithAPIKey(key), aopts.WithHTTPClient(client), aopts.WithMaxRetries(0)}
		if m.BaseURL != "" {
			opts = append(opts, aopts.WithBaseURL(m.BaseURL))
		}
		c := anthropic.NewClient(opts...)
		return gai.NewAnthropicGenerator(&c.Messages), nil
	case "gemini":
		c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI, HTTPClient: client, HTTPOptions: genai.HTTPOptions{BaseURL: m.BaseURL}})
		if err != nil {
			return nil, err
		}
		return gai.NewGeminiGenerator(c), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", m.Provider)
	}
}

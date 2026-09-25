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
	"github.com/spachava753/cpe/internal/gemini"
	"github.com/spachava753/cpe/internal/opencodego"
	"github.com/spachava753/cpe/internal/responses"
)

const codexProvider = "codex"

// Provider builds a gai generator with explicit credentials and no SDK retries.
// dir owns CPE credentials; sessionID is the durable root entry ID, shared by
// generation and compaction and retained when resuming or branching a session.
func Provider(ctx context.Context, m config.Model, dir, sessionID string) (gai.Generator, error) {
	if m.Provider == codexProvider {
		return codex.New(filepath.Join(dir, "auth.json")), nil
	}
	key := os.Getenv(m.APIKeyEnv)
	client := &http.Client{Timeout: 10 * time.Minute}
	if m.Credential == "opencode-go" {
		var err error
		m.BaseURL, err = opencodego.BaseURL(m.Provider)
		if err != nil {
			return nil, err
		}
		client, err = opencodego.Client(dir, sessionID)
		if err != nil {
			return nil, err
		}
		key = "cpe-key-loaded-by-transport" // SDK placeholder; never sent on the wire.
	} else if m.Credential != "" {
		return nil, fmt.Errorf("unsupported credential %q", m.Credential)
	} else if key == "" {
		return nil, fmt.Errorf("set %s for model %s", m.APIKeyEnv, m.ID)
	}
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
		return gemini.New(ctx, genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI, HTTPClient: client, HTTPOptions: genai.HTTPOptions{BaseURL: m.BaseURL}})
	default:
		return nil, fmt.Errorf("unsupported provider %q", m.Provider)
	}
}

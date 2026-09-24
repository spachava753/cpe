package opencodego

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/version"
)

const (
	credentialName    = "opencode-go"
	anthropicProtocol = "anthropic"
	apiURL            = "https://opencode.ai/zen/go/v1"
	modelsURL         = apiURL + "/models"
	catalogURL        = "https://models.dev/api.json"
)

type catalogProvider struct {
	NPM    string                  `json:"npm"`
	Models map[string]catalogModel `json:"models"`
}

type catalogModel struct {
	Status   string `json:"status"`
	Provider struct {
		NPM string `json:"npm"`
	} `json:"provider"`
	ToolCall bool `json:"tool_call"`
	Limit    struct {
		Context int `json:"context"`
		Input   int `json:"input"`
		Output  int `json:"output"`
	} `json:"limit"`
}

// Go's endpoint contract takes precedence over stale adapter metadata. These
// exact IDs use Messages according to https://opencode.ai/docs/go/#endpoints
// (checked 2026-09-24); do not infer routes from vendor or model-name prefixes.
var goProtocolOverrides = map[string]string{
	"qwen3.6-plus": anthropicProtocol,
	"qwen3.7-plus": anthropicProtocol,
	"qwen3.7-max":  anthropicProtocol,
	"qwen3.8-max":  anthropicProtocol,
}

// Login saves an API key locally and imports currently available, supported Go
// profiles. The public catalogs cannot verify the key or subscription; the first
// generation does that. Catalog/import failure leaves credentials unchanged.
// Existing profile customizations and the default model are never overwritten.
// If credential saving fails after import, retrying is safe: imported profiles
// remain signed out. The key must come from UI-only input, never a conversation.
func Login(ctx context.Context, dir, key string) (map[string]config.Model, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	return login(ctx, dir, key, client, modelsURL, catalogURL)
}

func login(ctx context.Context, dir, key string, client *http.Client, modelsEndpoint, catalogEndpoint string) (map[string]config.Model, error) {
	key = strings.TrimSpace(key)
	if !validKey(key) {
		return nil, errors.New("enter an OpenCode Go API key without whitespace")
	}
	profiles, err := discover(ctx, client, modelsEndpoint, catalogEndpoint)
	if err != nil {
		return nil, err
	}
	profiles, err = config.AddModels(ctx, dir, profiles)
	if err != nil {
		return nil, fmt.Errorf("import OpenCode Go profiles: %w", err)
	}
	if err := saveKey(ctx, dir, key); err != nil {
		return nil, fmt.Errorf("save OpenCode Go key (profiles imported; retry /login): %w", err)
	}
	return profiles, nil
}

func discover(ctx context.Context, client *http.Client, modelsEndpoint, catalogEndpoint string) (map[string]config.Model, error) {
	var available struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := fetch(ctx, client, modelsEndpoint, &available); err != nil {
		return nil, fmt.Errorf("load OpenCode Go models: %w", err)
	}
	// The document covers many providers. Decode only Go's provider namespace;
	// a matching model ID elsewhere conveys neither Go availability nor routing.
	var catalog struct {
		Go catalogProvider `json:"opencode-go"`
	}
	if err := fetch(ctx, client, catalogEndpoint, &catalog); err != nil {
		return nil, fmt.Errorf("load models.dev routing: %w", err)
	}
	goCatalog := catalog.Go
	profiles := make(map[string]config.Model)
	for _, model := range available.Data {
		metadata, ok := goCatalog.Models[model.ID]
		if !ok || model.ID == "" || metadata.Status == "deprecated" || !metadata.ToolCall || metadata.Limit.Context <= 0 || metadata.Limit.Output <= 0 {
			continue
		}
		npm := metadata.Provider.NPM
		if npm == "" {
			npm = goCatalog.NPM
		}
		provider := goProtocolOverrides[model.ID]
		if provider == "" {
			switch npm {
			case "@ai-sdk/openai-compatible":
				provider = "openai"
			case "@ai-sdk/openai":
				provider = "responses"
			case "@ai-sdk/anthropic":
				provider = anthropicProtocol
			default:
				continue // Unknown protocol: never guess from a model's name.
			}
		}
		// These are editable working budgets, not the model's maximums. Leave
		// output room and start below common long-context pricing thresholds.
		output := min(16384, metadata.Limit.Output, metadata.Limit.Context/2)
		input := min(128000, metadata.Limit.Context-output)
		if metadata.Limit.Input > 0 {
			input = min(input, metadata.Limit.Input)
		}
		if input <= 0 {
			continue
		}
		profiles[credentialName+"/"+model.ID] = config.Model{
			Provider: provider, ID: model.ID, Credential: credentialName,
			ContextWindow: input, MaxOutputTokens: output,
		}
	}
	if len(profiles) == 0 {
		return nil, errors.New("no active OpenCode Go models have supported protocol and context metadata; existing configuration is unchanged")
	}
	return profiles, nil
}

func fetch(ctx context.Context, client *http.Client, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "cpe/"+version.Get())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("catalog returned HTTP %d", resp.StatusCode)
	}
	const maxCatalogBytes = 32 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxCatalogBytes {
		return errors.New("model catalog exceeds 32 MiB")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("invalid model catalog JSON")
	}
	return nil
}

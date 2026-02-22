package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ModelsDevAPI is the URL for the models.dev registry
const ModelsDevAPI = "https://models.dev/api.json"

// ModelsDevProvider represents a provider in the models.dev registry
type ModelsDevProvider struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Env    []string                  `json:"env"`
	API    string                    `json:"api,omitempty"`
	Models map[string]ModelsDevModel `json:"models"`
}

// ModelsDevModel represents a model in the models.dev registry
type ModelsDevModel struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Limit    *ModelsDevLimit `json:"limit,omitempty"`
	Cost     *ModelsDevCost  `json:"cost,omitempty"`
	ToolCall bool            `json:"tool_call"`
}

// ModelsDevLimit represents token limits for a model
type ModelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

// ModelsDevCost represents cost per million tokens.
// Fields are pointers to distinguish "not provided" from an explicit zero cost.
type ModelsDevCost struct {
	Input      *float64 `json:"input,omitempty"`
	Output     *float64 `json:"output,omitempty"`
	CacheRead  *float64 `json:"cache_read,omitempty"`
	CacheWrite *float64 `json:"cache_write,omitempty"`
}

// providerTypeMap maps models.dev provider IDs to CPE types
var providerTypeMap = map[string]string{
	"anthropic":           "anthropic",
	"openai":              "openai",
	"google":              "gemini",
	"groq":                "groq",
	"cerebras":            "cerebras",
	"openrouter":          "openrouter",
	"zai":                 "zai",
	"zai-coding-plan":     "anthropic",
	"minimax-coding-plan": "anthropic",
}

// SupportedRegistryProviders returns the list of supported provider IDs from models.dev
func SupportedRegistryProviders() []string {
	providers := make([]string, 0, len(providerTypeMap))
	for p := range providerTypeMap {
		providers = append(providers, p)
	}
	return providers
}

// FetchModelsDevRegistry fetches the models.dev registry
func FetchModelsDevRegistry(ctx context.Context) (map[string]ModelsDevProvider, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", ModelsDevAPI, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var registry map[string]ModelsDevProvider
	if err := json.NewDecoder(resp.Body).Decode(&registry); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return registry, nil
}

// LookupRegistryModel looks up a provider and model in the registry
func LookupRegistryModel(registry map[string]ModelsDevProvider, providerID, modelID string) (*ModelsDevProvider, *ModelsDevModel, error) {
	provider, ok := registry[providerID]
	if !ok {
		return nil, nil, fmt.Errorf("provider %q not found in models.dev registry", providerID)
	}

	model, ok := provider.Models[modelID]
	if !ok {
		var available []string
		for id := range provider.Models {
			available = append(available, id)
		}
		return nil, nil, fmt.Errorf("model %q not found for provider %q. Available models: %v", modelID, providerID, available)
	}

	return &provider, &model, nil
}

// BuildModelFromRegistry creates a ModelConfig from registry data
func BuildModelFromRegistry(provider *ModelsDevProvider, model *ModelsDevModel, ref string) (*ModelConfig, error) {
	cpeType, ok := providerTypeMap[provider.ID]
	if !ok {
		supported := SupportedRegistryProviders()
		return nil, fmt.Errorf("provider %q is not supported by CPE. Supported providers: %v", provider.ID, supported)
	}

	// Determine ref name if not provided
	if ref == "" {
		ref = sanitizeRef(model.ID)
	}

	// Determine API key env var
	apiKeyEnv := ""
	if len(provider.Env) > 0 {
		apiKeyEnv = provider.Env[0]
	}

	cfg := &ModelConfig{
		Model: Model{
			Ref:         ref,
			DisplayName: model.Name,
			ID:          model.ID,
			Type:        cpeType,
			ApiKeyEnv:   apiKeyEnv,
			BaseUrl:     provider.API,
		},
	}

	if model.Limit == nil || model.Limit.Context <= 0 || model.Limit.Output <= 0 {
		return nil, fmt.Errorf("model %q from provider %q does not include required context/output limits in models.dev", model.ID, provider.ID)
	}
	cfg.ContextWindow = uint32(model.Limit.Context)
	cfg.MaxOutput = uint32(model.Limit.Output)
	if model.Cost != nil {
		cfg.InputCostPerMillion = cloneFloatPtr(model.Cost.Input)
		cfg.OutputCostPerMillion = cloneFloatPtr(model.Cost.Output)
		cfg.CacheReadCostPerMillion = cloneFloatPtr(model.Cost.CacheRead)
		cfg.CacheWriteCostPerMillion = cloneFloatPtr(model.Cost.CacheWrite)
	}

	return cfg, nil
}

func cloneFloatPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	copied := *v
	return &copied
}

// sanitizeRef creates a reasonable ref from a model ID
func sanitizeRef(modelID string) string {
	ref := modelID
	ref = strings.ReplaceAll(ref, "/", "-")
	ref = strings.ReplaceAll(ref, ":", "-")
	ref = strings.ReplaceAll(ref, "_", "-")

	for strings.Contains(ref, "--") {
		ref = strings.ReplaceAll(ref, "--", "-")
	}

	return strings.Trim(ref, "-")
}

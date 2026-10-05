package models

import (
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/negrel/secrecy"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/gai"
)

// Model represents a single configured model. It contains all of the information necessary to configure a [gai.Generator]
type Model struct {
	// BaseUrl is the base url of the model provider
	BaseUrl url.URL
	// API is the model provider type
	API API
	// APIKey is the API key secret, if applicable. Some models, like subscription supported models or local models, may not have nor need an API key
	APIKey *secrecy.SecretString
	// ID of the model, which will actually be used the API request
	ID string
	// Supported input modalities of the model
	InputModalities []gai.Modality
	// Supported output modalities of the model
	OutputModalities []gai.Modality
	// Input token cost of $ per million tokens
	Input float64
	// Output token cost of $ per million tokens
	Output float64
	// CacheRead is the cost of $ per million input tokens read from model provider cache. May not be applicable if the model provider does not support caching.
	CacheRead float64
	// CacheWrite is the cost of $ per million input tokens written to the model provider cache. May not be applicable if the model provider does not support caching, or simply does not charge for cache writes.
	CacheWrite float64
	// Limit is the context window limit of the model
	Limit uint
	// MaxTokens is the generation limit of the model, that is, max amount of the tokens the model can generate in a single generation call.
	MaxTokens uint
	// ReasoningLevels is the different thinking/reasoning levels supported by the model
	ReasoningLevels []string

	// Openai-compatible specific settings

	// ReasoningField decides what field to use for reading reasoning from the model. This field is specific to OpenAI Chat Completions compatible endpoints, as different model providers use different fields for preserving reasoning traces of the model between turns. See [gai.OpenAIExtraFieldReasoningSource]. Some provider specific generators like [gai.ZaiGenerator], [gai.MoonshotGenerator], or [gai.CerebrasGenerator] set this field automatically.
	ReasoningField ReasoningField
	// UseMaxTokens decides what field to use for limiting the max tokens the model can generate. This field is specific to OpenAI Chat Completions compatible endpoints, as different model providers use either `max_tokens` or `max_completion_tokens`. See [gai.OpenAIGenerationOptionTokenLimitField]. Some provider specific generators like [gai.ZaiGenerator], [gai.MoonshotGenerator], or [gai.CerebrasGenerator] set this field automatically.
	UseMaxTokens bool
	// ExtraFields adds extra fields to the request. This field is specific to OpenAI Chat Completions compatible endpoints, as different model providers use either `max_tokens` or `max_completion_tokens`. See [gai.OpenAIGenerationOptionExtraBody].  Some provider specific generators like [gai.ZaiGenerator], [gai.MoonshotGenerator], or [gai.CerebrasGenerator] set this field automatically.
	ExtraFields map[string]json.RawMessage
}

const modelConfigFile = "models.json"

func ConfigFile() (fs.File, error) {
	p, err := config.Path()
	if err != nil {
		return nil, err
	}
	p = filepath.Join(p, modelConfigFile)
	return os.Open(p)
}

type modelEntry struct {
	BaseUrl          string   `json:"base_url"`
	API              string   `json:"api"`
	APIKey           string   `json:"api_key"`
	ID               string   `json:"id"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Input            float64  `json:"input"`
	CacheRead        float64  `json:"cache_read"`
	CacheWrite       float64  `json:"cache_write"`
	Output           float64  `json:"output"`
	Limit            uint     `json:"limit"`
	MaxTokens        uint     `json:"max_tokens"`
	ReasoningLevels  []string `json:"reasoning_levels"`
	// Openai-compatible specific settings
	ReasoningField string `json:"reasoning_field"`
	UseMaxTokens   bool   `json:"use_max_tokens"`
}

// LoadModels loads models from a json file and returns a slice of parsed, valid [Model].
// We don't make a distinction of where the models JSON file is located, but we do
// expect a certain schema.
func LoadModels(f fs.File) []Model {
	return nil
}

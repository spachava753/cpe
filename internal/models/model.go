package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/negrel/secrecy"
	"github.com/spachava753/gai"
)

// Model represents a single configured model. It contains all of the information necessary to configure a [gai.Generator]
type Model struct {
	// ExtraFields adds provider-specific JSON fields to OpenAI Chat Completions compatible requests. See [gai.OpenAIGenerationOptionExtraBody].
	ExtraFields map[string]json.RawMessage
	// APIKey is the API key secret, if applicable. Some models, like subscription supported models or local models, may not have nor need an API key
	APIKey *secrecy.SecretString
	// ID of the model, which will actually be used the API request
	ID string
	// BaseURL is the base url of the model provider
	BaseURL url.URL
	// ReasoningLevels is the different thinking/reasoning levels supported by the model
	ReasoningLevels []string
	// Supported output modalities of the model
	OutputModalities []gai.Modality
	// Supported input modalities of the model
	InputModalities []gai.Modality
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
	// Openai-compatible specific settings

	// ReasoningField decides what field to use for reading reasoning from the model. This field is specific to OpenAI Chat Completions compatible endpoints, as different model providers use different fields for preserving reasoning traces of the model between turns. See [gai.OpenAIExtraFieldReasoningSource]. Some provider specific generators like [gai.ZaiGenerator], [gai.MoonshotGenerator], or [gai.CerebrasGenerator] set this field automatically.
	ReasoningField ReasoningField
	// API is the model provider type
	API API
	// UseMaxTokens decides what field to use for limiting the max tokens the model can generate. This field is specific to OpenAI Chat Completions compatible endpoints, as different model providers use either `max_tokens` or `max_completion_tokens`. See [gai.OpenAIGenerationOptionTokenLimitField]. Some provider specific generators like [gai.ZaiGenerator], [gai.MoonshotGenerator], or [gai.CerebrasGenerator] set this field automatically.
	UseMaxTokens bool
}

type modelEntry struct {
	ExtraFields map[string]json.RawMessage `json:"extra_fields"`
	BaseURL     string                     `json:"base_url"`
	API         string                     `json:"api"`
	APIKey      string                     `json:"api_key"`
	ID          string                     `json:"id"`
	// Openai-compatible specific settings
	ReasoningField   string   `json:"reasoning_field"`
	ReasoningLevels  []string `json:"reasoning_levels"`
	OutputModalities []string `json:"output_modalities"`
	InputModalities  []string `json:"input_modalities"`
	CacheRead        float64  `json:"cache_read"`
	CacheWrite       float64  `json:"cache_write"`
	Output           float64  `json:"output"`
	Limit            uint     `json:"limit"`
	MaxTokens        uint     `json:"max_tokens"`
	Input            float64  `json:"input"`
	UseMaxTokens     bool     `json:"use_max_tokens"`
}

// LoadModels reads a JSON array of model configurations from f. It returns nil
// models and an error if the file or any entry is invalid. Entry errors identify
// the zero-based array index and, for invalid domain values, the field.
// LoadModels does not close f; the caller owns the file.
func LoadModels(f fs.File) ([]Model, error) {
	decoder := json.NewDecoder(f)
	var entries []json.RawMessage
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	if entries == nil {
		return nil, errors.New("models: expected a JSON array, not null")
	}

	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == nil {
		return nil, errors.New("models: trailing data after JSON array")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("models: trailing data: %w", err)
	}

	parsed := make([]Model, 0, len(entries))
	for i, raw := range entries {
		// encoding/json otherwise replaces malformed UTF-8 inside strings.
		if !utf8.Valid(raw) {
			return nil, fmt.Errorf("models[%d]: invalid UTF-8", i)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("models[%d]: %w", i, err)
		}
		if fields == nil {
			return nil, fmt.Errorf("models[%d]: expected a model object, not null", i)
		}
		// Scalar unmarshalling treats null as absent. Configuration fields must
		// have their declared types; only values inside extra_fields may be null.
		for field, value := range fields {
			if bytes.Equal(value, []byte("null")) {
				return nil, fmt.Errorf("models[%d].%s: must not be null", i, field)
			}
		}
		entryDecoder := json.NewDecoder(bytes.NewReader(raw))
		entryDecoder.DisallowUnknownFields()
		var entry modelEntry
		if err := entryDecoder.Decode(&entry); err != nil {
			return nil, fmt.Errorf("models[%d]: %w", i, err)
		}
		model, err := entry.parse()
		if err != nil {
			return nil, fmt.Errorf("models[%d].%w", i, err)
		}
		parsed = append(parsed, model)
	}
	return parsed, nil
}

func (entry modelEntry) parse() (Model, error) {
	baseURL, err := url.Parse(entry.BaseURL)
	if err != nil {
		return Model{}, fmt.Errorf("base_url: %w", err)
	}
	if (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Hostname() == "" {
		return Model{}, errors.New("base_url: must be an absolute HTTP(S) URL with a hostname")
	}
	api, err := parseAPI(entry.API)
	if err != nil {
		return Model{}, err
	}
	if strings.TrimSpace(entry.ID) == "" {
		return Model{}, errors.New("id: must not be blank")
	}
	inputModalities, err := parseModalities("input_modalities", entry.InputModalities)
	if err != nil {
		return Model{}, err
	}
	outputModalities, err := parseModalities("output_modalities", entry.OutputModalities)
	if err != nil {
		return Model{}, err
	}
	if entry.Limit == 0 {
		return Model{}, errors.New("limit: must be positive")
	}
	if entry.MaxTokens == 0 {
		return Model{}, errors.New("max_tokens: must be positive")
	}
	for _, cost := range []struct {
		name  string
		value float64
	}{
		{"input", entry.Input},
		{"output", entry.Output},
		{"cache_read", entry.CacheRead},
		{"cache_write", entry.CacheWrite},
	} {
		if cost.value < 0 {
			return Model{}, fmt.Errorf("%s: must not be negative", cost.name)
		}
	}
	for i, level := range entry.ReasoningLevels {
		if strings.TrimSpace(level) == "" {
			return Model{}, fmt.Errorf("reasoning_levels[%d]: must not be blank", i)
		}
	}
	reasoningField, err := parseReasoningField(entry.ReasoningField)
	if err != nil {
		return Model{}, err
	}

	var apiKey *secrecy.SecretString
	if entry.APIKey != "" {
		secret := secrecy.NewSecretString([]byte(entry.APIKey))
		apiKey = &secret
	}
	return Model{
		BaseURL:          *baseURL,
		API:              api,
		APIKey:           apiKey,
		ID:               entry.ID,
		InputModalities:  inputModalities,
		OutputModalities: outputModalities,
		Input:            entry.Input,
		Output:           entry.Output,
		CacheRead:        entry.CacheRead,
		CacheWrite:       entry.CacheWrite,
		Limit:            entry.Limit,
		MaxTokens:        entry.MaxTokens,
		ReasoningLevels:  entry.ReasoningLevels,
		ReasoningField:   reasoningField,
		UseMaxTokens:     entry.UseMaxTokens,
		ExtraFields:      entry.ExtraFields,
	}, nil
}

func parseAPI(value string) (API, error) {
	for _, api := range []API{Responses, Anthropic, Gemini, Openai, Zai, Moonshot, Deepseek, Cerebras, OpenRouter} {
		if value == api.String() {
			return api, nil
		}
	}
	return 0, fmt.Errorf("api: unknown API %q", value)
}

func parseModalities(field string, values []string) ([]gai.Modality, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%s: must contain at least one modality", field)
	}
	parsed := make([]gai.Modality, 0, len(values))
	for i, value := range values {
		switch value {
		case "text":
			parsed = append(parsed, gai.Text)
		case "image":
			parsed = append(parsed, gai.Image)
		case "audio":
			parsed = append(parsed, gai.Audio)
		case "video":
			parsed = append(parsed, gai.Video)
		default:
			return nil, fmt.Errorf("%s[%d]: unknown modality %q", field, i, value)
		}
	}
	return parsed, nil
}

func parseReasoningField(value string) (ReasoningField, error) {
	if value == "" {
		return ReasoningDetails, nil
	}
	for _, field := range []ReasoningField{ReasoningDetails, ReasoningContent, Reasoning} {
		if value == field.String() {
			return field, nil
		}
	}
	return 0, fmt.Errorf("reasoning_field: unknown reasoning field %q", value)
}

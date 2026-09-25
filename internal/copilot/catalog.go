package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/spachava753/cpe/internal/config"
)

type catalogModel struct {
	ID            string   `json:"id"`
	PickerEnabled bool     `json:"model_picker_enabled"`
	Endpoints     []string `json:"supported_endpoints"`
	Policy        struct {
		State string `json:"state"`
	} `json:"policy"`
	Capabilities struct {
		Limits struct {
			Context int `json:"max_context_window_tokens"`
			Input   int `json:"max_prompt_tokens"`
			Output  int `json:"max_output_tokens"`
		} `json:"limits"`
		Supports struct {
			Tools     bool `json:"tool_calls"`
			Streaming bool `json:"streaming"`
		} `json:"supports"`
	} `json:"capabilities"`
}

func discover(ctx context.Context, client *http.Client, saved credential) (map[string]config.Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, saved.APIURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	clientHeaders(req)
	req.Header.Set("Authorization", "Bearer "+saved.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := readJSON(resp, &catalog); err != nil {
		return nil, err
	}
	models := make(map[string]config.Model)
	for _, raw := range catalog.Data {
		var item catalogModel
		if json.Unmarshal(raw, &item) != nil || !validToken(item.ID) || !item.PickerEnabled || !item.Capabilities.Supports.Tools || (item.Policy.State != "" && item.Policy.State != "enabled") {
			continue
		}
		limits := item.Capabilities.Limits
		if limits.Input <= 0 || limits.Output <= 0 || limits.Context < 0 {
			continue
		}
		var protocol string
		switch {
		case slices.Contains(item.Endpoints, "/responses"):
			protocol = responsesProtocol
		case slices.Contains(item.Endpoints, "/v1/messages"):
			protocol = messagesProtocol
		case item.Endpoints == nil || slices.Contains(item.Endpoints, "/chat/completions"):
			// Older catalog entries omit endpoints and use Chat Completions.
			protocol = chatProtocol
		default:
			continue
		}
		input, output := min(128000, limits.Input), min(16384, limits.Output)
		if limits.Context > 0 {
			input = min(input, limits.Context-output)
		}
		if input <= 0 {
			continue
		}
		models[credentialName+"/"+item.ID] = config.Model{Provider: protocol, Credential: credentialName, ID: item.ID, ContextWindow: input, MaxOutputTokens: output, CopilotStreaming: item.Capabilities.Supports.Streaming}
	}
	if len(models) == 0 {
		return nil, errors.New("GitHub Copilot has no enabled, tool-capable models with supported endpoints and usable limits; check your subscription and model policies")
	}
	return models, nil
}

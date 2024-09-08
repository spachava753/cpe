package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type AnthropicProvider struct {
	apiKey       string
	conversation Conversation
}

type anthropicRequestBody struct {
	Model         string    `json:"model"`
	MaxTokens     int       `json:"max_tokens"`
	Messages      []Message `json:"messages"`
	SystemMessage string    `json:"system"`
	Temperature   float32   `json:"temperature,omitempty"`
	TopP          float32   `json:"top_p,omitempty"`
	TopK          int       `json:"top_k,omitempty"`
	Stop          []string  `json:"stop_sequences,omitempty"`
}

type anthropicResponseBody struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model        string      `json:"model"`
	StopReason   string      `json:"stop_reason"`
	StopSequence interface{} `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
	}
}

func (a *AnthropicProvider) SetConversation(conv Conversation) error {
	a.conversation = conv
	return nil
}

func (a *AnthropicProvider) AddMessage(message Message) error {
	a.conversation.Messages = append(a.conversation.Messages, message)
	return nil
}

func (a *AnthropicProvider) GenerateResponse(config ModelConfig) (string, error) {
	url := "https://api.anthropic.com/v1/messages"

	requestBody := anthropicRequestBody{
		Model:         config.Model,
		MaxTokens:     config.MaxTokens,
		Messages:      a.conversation.Messages,
		SystemMessage: a.conversation.SystemPrompt,
		Temperature:   config.Temperature,
		TopP:          config.TopP,
		TopK:          config.TopK,
		Stop:          config.Stop,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling request body: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "max-tokens-3-5-sonnet-2024-07-15")
	req.Header.Set("content-type", "application/json")

	client := &http.Client{
		Timeout: 2 * time.Minute,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error: status code %d, body: %s", resp.StatusCode, string(body))
	}

	var responseBody anthropicResponseBody
	err = json.Unmarshal(body, &responseBody)
	if err != nil {
		return "", fmt.Errorf("error parsing response JSON: %w", err)
	}

	if len(responseBody.Content) > 0 {
		generatedResponse := responseBody.Content[0].Text
		a.AddMessage(Message{Role: "assistant", Content: generatedResponse})
		return generatedResponse, nil
	}

	return "", fmt.Errorf("no content in response")
}

func (a *AnthropicProvider) GetConversation() Conversation {
	return a.conversation
}

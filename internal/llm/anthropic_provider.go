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
	apiKey  string
	baseURL string
	client  *http.Client
}

type anthropicRequestBody struct {
	Model       string               `json:"model"`
	MaxTokens   int                  `json:"max_tokens"`
	Messages    []anthropicMessage   `json:"messages"`
	System      string               `json:"system,omitempty"`
	Temperature float32              `json:"temperature,omitempty"`
	TopP        float32              `json:"top_p,omitempty"`
	TopK        int                  `json:"top_k,omitempty"`
	Stop        []string             `json:"stop_sequences,omitempty"`
	Metadata    interface{}          `json:"metadata,omitempty"`
	Stream      bool                 `json:"stream,omitempty"`
	ToolChoice  *anthropicToolChoice `json:"tool_choice,omitempty"`
	Tools       []anthropicTool      `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string                         `json:"role"`
	Content []anthropicMessageContentBlock `json:"content"`
}

type anthropicMessageContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Id        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseId string          `json:"tool_use_id,omitempty"`
	IsError   *bool           `json:"is_error,omitempty"`
	Content   interface{}     `json:"content,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  json.RawMessage        `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicCacheControl struct {
	TTL int `json:"ttl"`
}

type anthropicResponseBody struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Role         string                 `json:"role"`
	Content      []anthropicContentItem `json:"content"`
	Model        string                 `json:"model"`
	StopReason   string                 `json:"stop_reason"`
	StopSequence string                 `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicContentItem struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func NewAnthropicProvider(apiKey string, baseURL string) *AnthropicProvider {
	provider := &AnthropicProvider{
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
	if baseURL != "" {
		provider.baseURL = baseURL
	} else {
		provider.baseURL = "https://api.anthropic.com"
	}
	return provider
}

func (a *AnthropicProvider) convertToAnthropicTools(tools []Tool) []anthropicTool {
	anthropicTools := make([]anthropicTool, len(tools))
	for i, tool := range tools {
		anthropicTools[i] = anthropicTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}
	}
	return anthropicTools
}

func (a *AnthropicProvider) convertToAnthropicMessages(messages []Message) []anthropicMessage {
	anthropicMessages := make([]anthropicMessage, len(messages))
	for i, msg := range messages {
		anthropicMsg := anthropicMessage{
			Role:    msg.Role,
			Content: make([]anthropicMessageContentBlock, 0, len(msg.Content)),
		}

		for _, content := range msg.Content {
			switch content.Type {
			case "text":
				anthropicMsg.Content = append(anthropicMsg.Content, anthropicMessageContentBlock{
					Type: "text",
					Text: content.Text,
				})
			case "tool_use":
				if content.ToolUse != nil {
					anthropicMsg.Content = append(anthropicMsg.Content, anthropicMessageContentBlock{
						Type:  "tool_use",
						Id:    content.ToolUse.ID,
						Name:  content.ToolUse.Name,
						Input: content.ToolUse.Input,
					})
				}
			case "tool_result":
				if content.ToolResult != nil {
					isError := false
					anthropicMsg.Content = append(anthropicMsg.Content, anthropicMessageContentBlock{
						Type:      "tool_result",
						ToolUseId: content.ToolResult.ToolUseID,
						IsError:   &isError,
						Content:   content.ToolResult.Content,
					})
				}
			}
		}

		anthropicMessages[i] = anthropicMsg
	}
	return anthropicMessages
}

func (a *AnthropicProvider) GenerateResponse(config GenConfig, conversation Conversation) (Message, TokenUsage, error) {
	url := a.baseURL + "/v1/messages"

	requestBody := anthropicRequestBody{
		Model:       config.Model,
		MaxTokens:   config.MaxTokens,
		Messages:    a.convertToAnthropicMessages(conversation.Messages),
		System:      conversation.SystemPrompt,
		Temperature: config.Temperature,
		Stop:        config.Stop,
		Tools:       a.convertToAnthropicTools(conversation.Tools),
	}

	if config.TopP != nil {
		requestBody.TopP = *config.TopP
	}
	if config.TopK != nil {
		requestBody.TopK = *config.TopK
	}

	if config.ToolChoice != "" {
		requestBody.ToolChoice = &anthropicToolChoice{Type: config.ToolChoice}
		if config.ForcedTool != "" {
			requestBody.ToolChoice.Name = config.ForcedTool
		}
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return Message{}, TokenUsage{}, fmt.Errorf("error marshaling request body: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return Message{}, TokenUsage{}, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return Message{}, TokenUsage{}, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, TokenUsage{}, fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Message{}, TokenUsage{}, fmt.Errorf("error: status code %d, body: %s", resp.StatusCode, string(body))
	}

	var responseBody anthropicResponseBody
	err = json.Unmarshal(body, &responseBody)
	if err != nil {
		return Message{}, TokenUsage{}, fmt.Errorf("error parsing response JSON: %w", err)
	}

	tokenUsage := TokenUsage{
		InputTokens:  responseBody.Usage.InputTokens,
		OutputTokens: responseBody.Usage.OutputTokens,
	}

	var contentBlocks []ContentBlock
	for _, content := range responseBody.Content {
		switch content.Type {
		case "text":
			contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: content.Text})
		case "tool_use":
			toolUse := &ToolUse{
				ID:    content.ID,
				Name:  content.Name,
				Input: content.Input,
			}
			contentBlocks = append(contentBlocks, ContentBlock{Type: "tool_use", ToolUse: toolUse})
		}
	}

	if len(contentBlocks) > 0 {
		return Message{
			Role:    "assistant",
			Content: contentBlocks,
		}, tokenUsage, nil
	}

	return Message{}, TokenUsage{}, fmt.Errorf("no content in response")
}

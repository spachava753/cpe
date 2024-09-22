package llm

import "fmt"

// Message represents a single message in a conversation
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// String implements the Stringer interface for Message
func (m Message) String() string {
	var result string
	result += fmt.Sprintf("Role: %s\n", m.Role)
	for i, content := range m.Content {
		result += fmt.Sprintf("  Content[%d]: Type=%s", i, content.Type)
		if content.Text != "" {
			result += ", Text=" + content.Text
		}
		if content.ToolUse != nil {
			result += fmt.Sprintf(", ToolUse={Name: %s}", content.ToolUse.Name)
		}
		if content.ToolResult != nil {
			result += fmt.Sprintf(
				", ToolResult={ToolUse Id: %s, Content: %s}",
				content.ToolResult.ToolUseID,
				content.ToolResult.Content,
			)
		}
		result += "\n"
	}
	return result
}

// Conversation represents a full conversation context
type Conversation struct {
	SystemPrompt string
	Messages     []Message
	Tools        []Tool
}

// String implements the Stringer interface for Conversation
func (c Conversation) String() string {
	var result string
	result += "System Prompt: " + c.SystemPrompt + "\n"
	result += "Messages:\n"
	for i, msg := range c.Messages {
		result += fmt.Sprintf("  [%d] Role: %s\n", i, msg.Role)
		for j, content := range msg.Content {
			result += fmt.Sprintf("    Content[%d]: Type=%s", j, content.Type)
			if content.Text != "" {
				result += ", Text=" + content.Text
			}
			result += "\n"
		}
	}
	result += "Tools:\n"
	for i, tool := range c.Tools {
		result += fmt.Sprintf("  [%d] %s\n", i, tool.Name)
	}
	return result
}

// ContentBlock represents a single block of content in a message
type ContentBlock struct {
	Type       string      `json:"type"`
	Text       string      `json:"text,omitempty"`
	ToolUse    *ToolUse    `json:"tool_use,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
}

// GenConfig represents the configuration when invoking a model.
// This helps divorce what model is invoked vs. what provider is used,
// so the same provider can invoke different models.
type GenConfig struct {
	Model             string
	MaxTokens         int
	Temperature       float32  // Controls randomness: 0.0 - 1.0
	TopP              float32  // Controls diversity: 0.0 - 1.0
	TopK              int      // Controls token sampling:
	FrequencyPenalty  float32  // Penalizes frequent tokens: -2.0 - 2.0
	PresencePenalty   float32  // Penalizes repeated tokens: -2.0 - 2.0
	Stop              []string // List of sequences where the API will stop generating further tokens
	NumberOfResponses int      // Number of chat completion choices to generate
	ToolChoice        string   // Controls tool use: "auto", "any", or "tool"
	ForcedTool        string   // Name of the tool to force when ToolChoice is "tool"
}

// LLMProvider defines the interface for interacting with LLM providers
type LLMProvider interface {
	// GenerateResponse generates a response from the assistant based on the provided conversation
	GenerateResponse(config GenConfig, conversation Conversation) ([]ContentBlock, error)
}

package llm

// Message represents a single message in a conversation
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Conversation represents a full conversation context
type Conversation struct {
	SystemPrompt string
	Messages     []Message
}

// ModelConfig represents the configuration when invoking a model.
// This helps divorce what model is invoked vs. what provider is used,
// so the same provider can invoke different models.
type ModelConfig struct {
	Model     string
	MaxTokens int
	// Add other common configuration options here
}

// LLMProvider defines the interface for interacting with LLM providers
type LLMProvider interface {
	// Initialize sets up the provider with the given API key
	Initialize(apiKey string) error

	// SetConversation sets or updates the current conversation context
	SetConversation(conv Conversation) error

	// AddMessage adds a new message to the current conversation
	AddMessage(message Message) error

	// GenerateResponse generates a response from the assistant based on the current conversation
	GenerateResponse(config ModelConfig) (string, error)

	// GetConversation returns the current conversation context
	GetConversation() Conversation
}

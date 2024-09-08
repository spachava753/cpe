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
	Model             string
	MaxTokens         int
	Temperature       float32  // Controls randomness: 0.0 - 1.0
	TopP              float32  // Controls diversity: 0.0 - 1.0
	TopK              int      // Controls token sampling:
	FrequencyPenalty  float32  // Penalizes frequent tokens: -2.0 - 2.0
	PresencePenalty   float32  // Penalizes repeated tokens: -2.0 - 2.0
	Stop              []string // List of sequences where the API will stop generating further tokens
	NumberOfResponses int      // Number of chat completion choices to generate
}

// LLMProvider defines the interface for interacting with LLM providers
type LLMProvider interface {
	// GenerateResponse generates a response from the assistant based on the provided conversation
	GenerateResponse(config ModelConfig, conversation Conversation) (string, error)
}

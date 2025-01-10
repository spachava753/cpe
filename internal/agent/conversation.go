package agent

// Conversation represents a conversation history with a specific executor type
type Conversation[T any] struct {
	Type     string
	Messages T
}

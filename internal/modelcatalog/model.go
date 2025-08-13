package modelcatalog

type Model struct {
	Name                   string  `json:"name"`
	ID                     string  `json:"id"`
	Type                   string  `json:"type"`
	BaseUrl                string  `json:"base_url"`
	ApiKeyEnv              string  `json:"api_key_env"`
	ContextWindow          uint32  `json:"context_window"`
	MaxOutput              uint32  `json:"max_output"`
	InputCostPerMillion    float64 `json:"input_cost_per_million"`
	OutputCostPerMillion   float64 `json:"output_cost_per_million"`
	SupportsReasoning      bool    `json:"supports_reasoning"`
	DefaultReasoningEffort string  `json:"default_reasoning_effort"`
}

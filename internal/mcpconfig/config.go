package mcpconfig

// ServerConfig declares how CPE connects to one MCP server and filters its tools.
// When Type is empty, stdio is assumed. Type "builtin" selects a CPE-provided
// in-process server. Validation tags enforce basic transport constraints, with
// additional cross-field checks performed in internal/config.
type ServerConfig struct {
	Command       string            `json:"command" yaml:"command"`
	Args          []string          `json:"args" yaml:"args"`
	Type          string            `json:"type,omitempty" yaml:"type,omitempty" validate:"omitempty,oneof=stdio sse http builtin" jsonschema:"enum=stdio,enum=sse,enum=http,enum=builtin"`
	URL           string            `json:"url,omitempty" yaml:"url,omitempty" validate:"excluded_if=Type stdio,required_if=Type sse,required_if=Type http,omitempty,https_url|http_url"`
	Timeout       int               `json:"timeout,omitempty" yaml:"timeout,omitempty" validate:"gte=0"`
	Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty" validate:"excluded_if=Type http,excluded_if=Type sse"`
	Headers       map[string]string `json:"headers,omitempty" yaml:"headers,omitempty" validate:"excluded_if=Type stdio"`
	EnabledTools  []string          `json:"enabledTools,omitempty" yaml:"enabledTools,omitempty" validate:"omitempty,min=1,excluded_with=DisabledTools"`
	DisabledTools []string          `json:"disabledTools,omitempty" yaml:"disabledTools,omitempty" validate:"omitempty,min=1,excluded_with=EnabledTools"`
}

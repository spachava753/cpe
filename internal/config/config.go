package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/jsonconfig"
	"github.com/spachava753/cpe/internal/theme"
)

// Config contains application defaults and named provider profiles.
type Config struct {
	needsDefault bool
	DefaultModel string               `json:"default_model"`
	Models       map[string]Model     `json:"models"`
	Agent        Agent                `json:"agent"`
	TUI          tui                  `json:"tui"`
	Compaction   Compaction           `json:"compaction"`
	MCPServers   map[string]mcpServer `json:"mcp_servers,omitempty"`
	System       string               `json:"-"`
	Dir          string               `json:"-"`
}

// mcpServer selects exactly one transport: a stdio command (with optional args
// and environment overrides), or a Streamable HTTP URL. Commands run in the
// agent's working directory and inherit the process environment.
type mcpServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

// Model selects a wire provider, credentials, and generation settings. Codex uses
// ~/.cpe/auth.json. Credential="opencode-go" selects CPE's saved Go API key with
// an openai, responses, or anthropic provider. Both omit APIKeyEnv and BaseURL.
// ReasoningEffort is forwarded through every provider; Codex rejects output
// limits and temperature.
type Model struct {
	Provider        string   `json:"provider"`
	ID              string   `json:"id"`
	Credential      string   `json:"credential,omitempty"`
	APIKeyEnv       string   `json:"api_key_env,omitempty"`
	BaseURL         string   `json:"base_url,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	ContextWindow   int      `json:"context_window,omitempty"`
	Cost            *Pricing `json:"cost,omitempty"`
}

// Rates are USD per million tokens in four mutually exclusive billing buckets.
// All four values must be supplied; zero explicitly means no charge.
type Rates struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheWrite *float64 `json:"cache_write"`
	CacheRead  *float64 `json:"cache_read"`
}

// Pricing configures estimates, not provider billing. LongContext optionally
// replaces all rates when a request's total input exceeds its threshold.
type Pricing struct {
	Rates
	LongContext *PriceTier `json:"long_context,omitempty"`
}

// PriceTier applies to the entire request, including cached input and output.
type PriceTier struct {
	AboveInputTokens int64 `json:"above_input_tokens"`
	Rates
}

// ValidateBudget checks local context and pricing settings independently of
// provider credentials. ContextWindow is a preferred input-token budget; zero
// disables token-based compaction. It does not alter the provider's actual limit.
func (m Model) ValidateBudget() error {
	if m.ContextWindow < 0 {
		return errors.New("context_window must be nonnegative")
	}
	if m.Cost == nil {
		return nil
	}
	if err := m.Cost.validate(); err != nil {
		return err
	}
	if tier := m.Cost.LongContext; tier != nil {
		if tier.AboveInputTokens <= 0 {
			return errors.New("cost.long_context.above_input_tokens must be positive")
		}
		return tier.validate()
	}
	return nil
}

func (r Rates) validate() error {
	for _, rate := range []*float64{r.Input, r.Output, r.CacheWrite, r.CacheRead} {
		if rate == nil || *rate < 0 || math.IsNaN(*rate) || math.IsInf(*rate, 0) {
			return errors.New("cost requires finite, nonnegative input, output, cache_write, and cache_read rates")
		}
	}
	return nil
}

// ReasoningEfforts returns common effort labels followed by thinking modes.
// All providers accept this setting; their adapters/models determine which
// labels are supported. In particular, Anthropic supports adaptive and disabled.
func ReasoningEfforts() []string {
	return []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "adaptive", "disabled"}
}

// WithReasoningEffort returns a copy with validated reasoning settings. An empty
// effort omits the provider option; it is distinct from the explicit "none".
func (m Model) WithReasoningEffort(effort string) (Model, error) {
	if effort != "" {
		if !slices.Contains(ReasoningEfforts(), effort) {
			return m, fmt.Errorf("invalid reasoning_effort %q", effort)
		}
	}
	m.ReasoningEffort = effort
	return m, nil
}

// Agent bounds individual REPL executions and the number of model rounds.
type Agent struct {
	ToolTimeout string `json:"tool_timeout"`
	OutputLimit int    `json:"output_limit"`
	MaxRounds   int    `json:"max_rounds"`
}

// Compaction configures manual /compact and optional automatic context reduction.
// MaxCharacters is a serialized-dialog character threshold; zero disables auto.
type Compaction struct {
	Prompt        string `json:"prompt"`
	MaxCharacters int    `json:"max_characters"`
}

// Directory returns ~/.cpe using the operating system's user home directory.
func Directory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cpe"), nil
}

// Load reads and validates both files in the fixed user configuration directory.
func Load(ctx context.Context) (Config, error) {
	dir, err := Directory()
	if err != nil {
		return Config{}, err
	}
	return load(ctx, dir)
}
func load(ctx context.Context, dir string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return Config{}, fmt.Errorf("load config.json (run cpe --init for starter files): %w", err)
	}
	c, err := parse(dir, data)
	if err == nil && c.needsDefault {
		return editConfig(ctx, dir, nil)
	}
	return c, err
}

func parse(dir string, data []byte) (Config, error) {
	c := Config{Dir: dir, Agent: Agent{ToolTimeout: "1m", OutputLimit: 32000, MaxRounds: 50}, Compaction: Compaction{Prompt: "Summarize the conversation for continuation. Preserve the user's goals, decisions, files changed, unresolved work, and names/types of useful persistent Starlark variables. Do not claim external effects were undone."}}
	if err := jsonconfig.Decode(data, &c); err != nil {
		return c, fmt.Errorf("load config.json: %w", err)
	}
	if c.DefaultModel == "" && len(c.Models) > 0 {
		// Preserve declaration order: maps deliberately do not carry this boundary fact.
		var document map[string]json.RawMessage
		_ = json.Unmarshal(data, &document)
		decoder := json.NewDecoder(bytes.NewReader(document["models"]))
		_, _ = decoder.Token()
		first, _ := decoder.Token()
		c.DefaultModel, _ = first.(string)
		c.needsDefault = true
	}
	data, err := os.ReadFile(filepath.Join(dir, "system.md"))
	if err != nil {
		return c, err
	}
	c.System = string(data)
	if strings.TrimSpace(c.System) == "" {
		return c, errors.New("system.md must not be empty")
	}
	if _, ok := c.Models[c.DefaultModel]; !ok && (len(c.Models) != 0 || c.DefaultModel != "") {
		return c, errors.New("default_model must name a configured model")
	}
	for name, m := range c.Models {
		if strings.TrimSpace(name) == "" {
			return c, errors.New("model profile names must not be empty")
		}
		if m.Credential != "" {
			if m.Credential != "opencode-go" || (m.Provider != "openai" && m.Provider != "responses" && m.Provider != "anthropic") || m.APIKeyEnv != "" || m.BaseURL != "" {
				return c, fmt.Errorf("model %q: credential must be opencode-go with provider openai, responses, or anthropic; omit api_key_env and base_url", name)
			}
		}
		if err := m.ValidateBudget(); err != nil {
			return c, fmt.Errorf("model %q: %w", name, err)
		}
		if m.ID == "" || m.MaxOutputTokens < 0 {
			return c, fmt.Errorf("model %q requires id and nonnegative max_output_tokens", name)
		}
		switch m.Provider {
		case "openai", "responses", "anthropic", "gemini":
			if m.APIKeyEnv == "" && m.Credential == "" {
				return c, fmt.Errorf("model %q requires api_key_env", name)
			}
		case "codex":
			if m.APIKeyEnv != "" || m.BaseURL != "" || m.MaxOutputTokens != 0 || m.Temperature != nil {
				return c, fmt.Errorf("codex model %q uses CPE OAuth and a fixed endpoint; omit api_key_env, base_url, max_output_tokens, and temperature", name)
			}
		default:
			return c, fmt.Errorf("unsupported provider %q", m.Provider)
		}
		if _, err := m.WithReasoningEffort(m.ReasoningEffort); err != nil {
			return c, fmt.Errorf("model %q: %w", name, err)
		}
		if m.BaseURL != "" {
			u, err := url.Parse(m.BaseURL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return c, fmt.Errorf("invalid base_url for %q", name)
			}
		}
		if m.Temperature != nil && (*m.Temperature < 0 || *m.Temperature > 2) {
			return c, fmt.Errorf("temperature for %q must be between 0 and 2", name)
		}
	}
	timeout, err := time.ParseDuration(c.Agent.ToolTimeout)
	if err != nil || timeout <= 0 {
		return c, errors.New("agent.tool_timeout must be a positive duration")
	}
	if c.Agent.OutputLimit <= 0 || c.Agent.MaxRounds <= 0 {
		return c, errors.New("agent output_limit and max_rounds must be positive")
	}
	if c.Compaction.MaxCharacters < 0 || strings.TrimSpace(c.Compaction.Prompt) == "" {
		return c, errors.New("invalid compaction settings")
	}
	for name, server := range c.MCPServers {
		if (server.Command == "") == (server.URL == "") {
			return c, fmt.Errorf("MCP %q requires exactly one of command or url", name)
		}
		if server.URL != "" {
			u, err := url.Parse(server.URL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
				return c, fmt.Errorf("MCP %q requires an HTTP(S) URL without userinfo or fragment", name)
			}
			if len(server.Args) != 0 || len(server.Env) != 0 {
				return c, fmt.Errorf("MCP %q: args and env require command", name)
			}
		}
	}
	return c, nil
}

const themesFile = "themes.json"

// Init creates private starter files, leaving any existing files intact.
func Init() (string, error) {
	dir, err := Directory()
	if err != nil {
		return "", err
	}
	return initFiles(dir)
}

func initFiles(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	for _, v := range []struct{ name, body string }{{"config.json", defaultJSON}, {"system.md", defaultSystem}, {themesFile, theme.StarterJSON}} {
		f, err := os.OpenFile(filepath.Join(dir, v.name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, writeErr := f.WriteString(v.body)
		err = errors.Join(writeErr, f.Close())
		if err != nil {
			return "", err
		}
	}
	return dir, nil
}

const defaultJSON = `{
  "default_model": "default",
  "tui": {"submit_key": "enter"},
  "models": {
    "default": {
      "provider": "codex",
      "id": "gpt-6-astra",
      "context_window": 272000
    }
  },
  "agent": {
    "tool_timeout": "1m",
    "output_limit": 32000,
    "max_rounds": 50
  },
  "compaction": {
    "max_characters": 0,
    "prompt": "Summarize for continuation: preserve goals, decisions, file changes, unresolved work, and persistent Starlark variables."
  }
}
`
const defaultSystem = `You are a thoughtful programming assistant working in the user's current directory.
Inspect relevant files before changing them. Make focused changes and verify your work.
Use the persistent Starlark REPL to interact with files, HTTP services, and commands.
Explain outcomes clearly and concisely.
`

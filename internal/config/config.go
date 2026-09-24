package config

import (
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
	DefaultModel string           `json:"default_model"`
	Models       map[string]Model `json:"models"`
	Agent        Agent            `json:"agent"`
	Compaction   Compaction       `json:"compaction"`
	System       string           `json:"-"`
	Dir          string           `json:"-"`
}

// Model selects a provider, credentials, and generation settings. Codex uses
// credentials in ~/.cpe/auth.json, not APIKeyEnv or BaseURL. ReasoningEffort
// applies to Responses and Codex; Codex rejects output limits and temperature.
type Model struct {
	Provider        string   `json:"provider"`
	ID              string   `json:"id"`
	APIKeyEnv       string   `json:"api_key_env"`
	BaseURL         string   `json:"base_url"`
	ReasoningEffort string   `json:"reasoning_effort"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	Temperature     *float64 `json:"temperature"`
	ContextWindow   int      `json:"context_window"`
	Cost            *Pricing `json:"cost"`
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

// ReasoningEfforts returns the recognized effort labels in increasing order.
// Individual models may support only a subset of these labels.
func ReasoningEfforts() []string {
	return []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
}

// WithReasoningEffort returns a copy with validated reasoning settings. An empty
// effort omits the provider option; it is distinct from the explicit "none".
func (m Model) WithReasoningEffort(effort string) (Model, error) {
	if effort != "" {
		if m.Provider != "responses" && m.Provider != "codex" {
			return m, errors.New("reasoning_effort requires responses or codex")
		}
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
func Load() (Config, error) {
	dir, err := Directory()
	if err != nil {
		return Config{}, err
	}
	return load(dir)
}
func load(dir string) (Config, error) {
	c := Config{Dir: dir, Agent: Agent{ToolTimeout: "1m", OutputLimit: 32000, MaxRounds: 50}, Compaction: Compaction{Prompt: "Summarize the conversation for continuation. Preserve the user's goals, decisions, files changed, unresolved work, and names/types of useful persistent Starlark variables. Do not claim external effects were undone."}}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return c, fmt.Errorf("load config.json (run cpe --init for starter files): %w", err)
	}
	if err := jsonconfig.Decode(data, &c); err != nil {
		return c, fmt.Errorf("load config.json: %w", err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "system.md"))
	if err != nil {
		return c, err
	}
	c.System = string(data)
	if strings.TrimSpace(c.System) == "" {
		return c, errors.New("system.md must not be empty")
	}
	if _, ok := c.Models[c.DefaultModel]; !ok {
		return c, errors.New("default_model must name a configured model")
	}
	for name, m := range c.Models {
		if err := m.ValidateBudget(); err != nil {
			return c, fmt.Errorf("model %q: %w", name, err)
		}
		if m.ID == "" || m.MaxOutputTokens < 0 {
			return c, fmt.Errorf("model %q requires id and nonnegative max_output_tokens", name)
		}
		switch m.Provider {
		case "openai", "responses", "anthropic", "gemini":
			if m.APIKeyEnv == "" {
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
  "models": {
    "default": {
      "provider": "codex",
      "id": "gpt-6-astra",
      "reasoning_effort": "low",
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

package commands

import "github.com/spachava753/gai"

// GenParamValues holds the resolved flag values for generation parameters.
// The internal/cmd package binds and populates these values before calling
// BuildGenOpts.
type GenParamValues struct {
	MaxTokens        *int
	Temperature      *float64
	TopP             *float64
	TopK             *uint
	FrequencyPenalty *float64
	PresencePenalty  *float64
	N                *uint
	ThinkingBudget   string
}

// GenParamChanges records which generation parameters were explicitly set by the
// CLI layer. This keeps commands framework-agnostic: the internal/cmd package
// determines whether a flag changed, then passes those facts here as plain
// booleans.
type GenParamChanges struct {
	MaxTokens        bool
	Temperature      bool
	TopP             bool
	TopK             bool
	FrequencyPenalty bool
	PresencePenalty  bool
	N                bool
	ThinkingBudget   bool
}

// BuildGenOpts constructs a *gai.GenOpts containing only explicitly changed
// fields. It returns nil when no generation parameter overrides were supplied.
func BuildGenOpts(values GenParamValues, changed GenParamChanges) *gai.GenOpts {
	var opts gai.GenOpts
	anySet := false

	if changed.MaxTokens {
		opts.MaxGenerationTokens = values.MaxTokens
		anySet = true
	}
	if changed.Temperature {
		opts.Temperature = values.Temperature
		anySet = true
	}
	if changed.TopP {
		opts.TopP = values.TopP
		anySet = true
	}
	if changed.TopK {
		opts.TopK = values.TopK
		anySet = true
	}
	if changed.FrequencyPenalty {
		opts.FrequencyPenalty = values.FrequencyPenalty
		anySet = true
	}
	if changed.PresencePenalty {
		opts.PresencePenalty = values.PresencePenalty
		anySet = true
	}
	if changed.N {
		opts.N = values.N
		anySet = true
	}
	if changed.ThinkingBudget {
		opts.ThinkingBudget = values.ThinkingBudget
		anySet = true
	}

	if !anySet {
		return nil
	}
	return &opts
}

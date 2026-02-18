package commands

import (
	"github.com/spachava753/gai"
	"github.com/spf13/pflag"
)

// GenParamFlags holds pointers to the CLI flag variables for generation parameters.
// These are populated by cobra flag bindings in the cmd package.
type GenParamFlags struct {
	MaxTokens        *int
	Temperature      *float64
	TopP             *float64
	TopK             *uint
	FrequencyPenalty *float64
	PresencePenalty  *float64
	N                *uint
	ThinkingBudget   string
}

// BuildGenOptsFromFlags constructs a *gai.GenOpts containing only the fields
// whose corresponding CLI flags were explicitly provided by the user.
// Returns nil if no generation parameter flags were set.
func BuildGenOptsFromFlags(flags *pflag.FlagSet, params GenParamFlags) *gai.GenOpts {
	var opts gai.GenOpts
	anySet := false

	flags.Visit(func(f *pflag.Flag) {
		switch f.Name {
		case "max-tokens":
			opts.MaxGenerationTokens = params.MaxTokens
			anySet = true
		case "temperature":
			opts.Temperature = params.Temperature
			anySet = true
		case "top-p":
			opts.TopP = params.TopP
			anySet = true
		case "top-k":
			opts.TopK = params.TopK
			anySet = true
		case "frequency-penalty":
			opts.FrequencyPenalty = params.FrequencyPenalty
			anySet = true
		case "presence-penalty":
			opts.PresencePenalty = params.PresencePenalty
			anySet = true
		case "number-of-responses":
			opts.N = params.N
			anySet = true
		case "thinking-budget":
			opts.ThinkingBudget = params.ThinkingBudget
			anySet = true
		}
	})

	if !anySet {
		return nil
	}
	return &opts
}

package cliopts

import (
	"flag"
	"fmt"
	"github.com/spachava753/cpe/internal/agent"
	"maps"
	"slices"
	"strings"
)

type Options struct {
	Model             string
	CustomURL         string
	MaxTokens         int
	Temperature       float64
	TopP              float64
	TopK              int
	FrequencyPenalty  float64
	PresencePenalty   float64
	NumberOfResponses int
	Input             string
	Version           bool
	TokenCountPath    string
	Prompt            string
}

var Opts Options

func init() {
	flag.StringVar(&Opts.TokenCountPath, "token-count", "", "Print a tree of directories and files with their token counts for the given path")
	flag.BoolVar(&Opts.Version, "version", false, "Print the version number and exit")
	flag.StringVar(&Opts.Model, "model", agent.DefaultModel, fmt.Sprintf("Specify the model to use. Supported models: %s", strings.Join(slices.Collect(maps.Keys(agent.ModelConfigs)), ", ")))
	flag.StringVar(&Opts.CustomURL, "custom-url", "", "Specify a custom base URL for the model provider API")
	flag.IntVar(&Opts.MaxTokens, "max-tokens", 0, "Maximum number of tokens to generate")
	flag.Float64Var(&Opts.Temperature, "temperature", 0, "Sampling temperature (0.0 - 1.0)")
	flag.Float64Var(&Opts.TopP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	flag.IntVar(&Opts.TopK, "top-k", 0, "Top-k sampling parameter")
	flag.Float64Var(&Opts.FrequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	flag.Float64Var(&Opts.PresencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	flag.IntVar(&Opts.NumberOfResponses, "number-of-responses", 0, "Number of responses to generate")
	flag.StringVar(&Opts.Input, "input", "", "Specify the input file path. Use '-' for stdin. If omitted, only command line arguments are used as input")
}

func ParseFlags() {
	flag.Parse()

	// Any remaining arguments after flags are treated as the prompt
	if args := flag.Args(); len(args) > 0 {
		Opts.Prompt = strings.Join(args, " ")
	}
}

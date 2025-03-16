package cliopts

import (
	"flag"
	"log"
	"os"
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
	ThinkingBudget    string
	Input             bool
	Version           bool
	Prompt            string
	Continue          string
	DeleteCascade     bool
	New               bool
	Args              []string // Remaining arguments after flag parsing
}

var Opts Options

func init() {
	flag.StringVar(&Opts.Model, "model", "", "Specify the model to use")
	flag.BoolVar(&Opts.Version, "version", false, "Print the version number and exit")
	flag.StringVar(&Opts.CustomURL, "custom-url", "", "Specify a custom base URL for the model provider API")
	flag.IntVar(&Opts.MaxTokens, "max-tokens", 0, "Maximum number of tokens to generate")
	flag.Float64Var(&Opts.Temperature, "temperature", 0, "Sampling temperature (0.0 - 1.0)")
	flag.Float64Var(&Opts.TopP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	flag.IntVar(&Opts.TopK, "top-k", 0, "Top-k sampling parameter")
	flag.Float64Var(&Opts.FrequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	flag.Float64Var(&Opts.PresencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	flag.IntVar(&Opts.NumberOfResponses, "number-of-responses", 0, "Number of responses to generate")
	flag.StringVar(&Opts.ThinkingBudget, "thinking-budget", "", "Budget for reasoning/thinking capabilities (string or numerical value)")
	flag.BoolVar(&Opts.Input, "input", false, "When provided, all arguments except the last one are treated as input files that must exist. The last argument is either a file path or a prompt text")
	flag.StringVar(&Opts.Continue, "continue", "", "Continue from a specific conversation ID")
	flag.BoolVar(&Opts.DeleteCascade, "cascade", false, "When deleting a conversation, also delete its children")
	flag.BoolVar(&Opts.New, "new", false, "Start a new conversation instead of continuing from the last one")
}

func ParseFlags() {
	flag.Parse()

	// Store remaining arguments
	Opts.Args = flag.Args()

	if len(Opts.Args) > 0 {
		if Opts.Input {
			// If -input flag is provided, need at least one input file
			if len(Opts.Args) < 1 {
				log.Fatal("when using -input flag, need at least one input file")
			}
			// All arguments are treated as input files, except the last one if it's not a file
			lastIdx := len(Opts.Args)
			lastArg := Opts.Args[lastIdx-1]
			if _, err := os.Stat(lastArg); err != nil {
				// Last argument doesn't exist as a file, treat it as prompt text
				lastIdx--
				Opts.Prompt = lastArg
			}
			// Validate all other arguments are valid files
			for _, path := range Opts.Args[:lastIdx] {
				if _, err := os.Stat(path); err != nil {
					log.Fatalf("input file does not exist: %s", path)
				}
			}
		} else {
			// If -input flag is not provided, only one argument (the prompt) is allowed
			if len(Opts.Args) > 1 {
				log.Fatal("without -input flag, only one argument (the prompt) can be provided")
			}
			Opts.Prompt = Opts.Args[0]
		}
	}
}

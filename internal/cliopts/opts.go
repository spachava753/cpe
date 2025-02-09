package cliopts

import (
	"flag"
	"fmt"
	"github.com/spachava753/cpe/internal/agent"
	"log"
	"maps"
	"os"
	"slices"
	"strings"
)

type Options struct {
	Model              string
	CustomURL          string
	MaxTokens          int
	Temperature        float64
	TopP               float64
	TopK               int
	FrequencyPenalty   float64
	PresencePenalty    float64
	NumberOfResponses  int
	Input              string
	Version            bool
	TokenCountPath     string
	Prompt             string
	Continue           string
	ListConversations  bool
	DeleteConversation string
	DeleteCascade      bool
	PrintConversation  string
	ListFiles          bool
	Overview           bool
	RelatedFiles       string
	New                bool
}

var Opts Options

func init() {
	// Get default model from environment variable if available
	defaultModel := agent.DefaultModel
	if envModel := os.Getenv("CPE_MODEL"); envModel != "" {
		defaultModel = envModel
	}

	flag.StringVar(&Opts.Model, "model", defaultModel, fmt.Sprintf("Specify the model to use. Supported models: %s (default: %s)", strings.Join(slices.Collect(maps.Keys(agent.ModelConfigs)), ", "), defaultModel))
	flag.StringVar(&Opts.TokenCountPath, "token-count", "", "Print a tree of directories and files with their token counts for the given path")
	flag.BoolVar(&Opts.Version, "version", false, "Print the version number and exit")
	flag.BoolVar(&Opts.ListFiles, "list-files", false, "List all text files in the current directory recursively")
	flag.BoolVar(&Opts.Overview, "overview", false, "Get an overview of all files in the current directory with reduced content")
	flag.StringVar(&Opts.RelatedFiles, "related-files", "", "Get related files for the given comma-separated list of files")
	flag.StringVar(&Opts.CustomURL, "custom-url", "", "Specify a custom base URL for the model provider API")
	flag.IntVar(&Opts.MaxTokens, "max-tokens", 0, "Maximum number of tokens to generate")
	flag.Float64Var(&Opts.Temperature, "temperature", 0, "Sampling temperature (0.0 - 1.0)")
	flag.Float64Var(&Opts.TopP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	flag.IntVar(&Opts.TopK, "top-k", 0, "Top-k sampling parameter")
	flag.Float64Var(&Opts.FrequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	flag.Float64Var(&Opts.PresencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	flag.IntVar(&Opts.NumberOfResponses, "number-of-responses", 0, "Number of responses to generate")
	flag.StringVar(&Opts.Input, "input", "", "When specified, all arguments except the last one are treated as input files. The last argument is the prompt. Without this flag, only one argument (the prompt) is accepted")
	flag.StringVar(&Opts.Continue, "continue", "", "Continue from a specific conversation ID")
	flag.BoolVar(&Opts.ListConversations, "list-convo", false, "List all conversations")
	flag.StringVar(&Opts.DeleteConversation, "delete-convo", "", "Delete a specific conversation")
	flag.BoolVar(&Opts.DeleteCascade, "cascade", false, "When deleting a conversation, also delete its children")
	flag.StringVar(&Opts.PrintConversation, "print-convo", "", "Print a specific conversation")
	flag.BoolVar(&Opts.New, "new", false, "Start a new conversation instead of continuing from the last one")
}

func ParseFlags() {
	flag.Parse()

	// Any remaining arguments after flags
	args := flag.Args()
	if len(args) > 0 {
		if Opts.Input != "" {
			// If -input flag is specified, all arguments except the last one are input files
			// and the last argument is the prompt
			if len(args) < 2 {
				// Need at least one input file and the prompt
				log.Fatal("when using -input flag, at least one input file and a prompt must be provided")
			}
			Opts.Input = strings.Join(args[:len(args)-1], ",")
			Opts.Prompt = args[len(args)-1]
		} else {
			// If -input flag is not specified, the only argument is the prompt
			if len(args) > 1 {
				log.Fatal("without -input flag, only one argument (the prompt) can be provided")
			}
			Opts.Prompt = args[0]
		}
	}
}

package main

import (
	_ "embed"
	"fmt"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/cliopts"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/tokentree"
	"log"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

// getVersion returns the version of the application from build info
func getVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(unknown version)"
}

func main() {
	startTime := time.Now()
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
	defer func() {
		elapsed := time.Since(startTime)
		log.Printf("finished execution, elapsed: %s", elapsed)
	}()

	config, err := parseConfig()
	if err != nil {
		log.Fatalf("fatal error: %s", err)
	}

	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		log.Fatalf("fatal error: %s", err)
	}
	if ignorer == nil {
		log.Fatal("git ignorer was nil")
	}

	if config.TokenCountPath != "" {
		if err := tokentree.PrintTokenTree(os.DirFS("."), ignorer); err != nil {
			log.Fatalf("fatal error: %s", err)
		}
		return
	}

	if config.ListFiles {
		files, err := agent.ListTextFiles(ignorer)
		if err != nil {
			log.Fatalf("fatal error: %s", err)
		}
		for _, file := range files {
			if _, writeErr := fmt.Fprintf(os.Stdout, "File: %s\nContent:\n%s\n\n", file.Path, file.Content); writeErr != nil {
				log.Fatalf("fatal error: %s", writeErr)
			}
		}
		return
	}

	executor, err := agent.InitExecutor(log.Default(), agent.ModelOptions{
		Model:             config.Model,
		CustomURL:         config.CustomURL,
		MaxTokens:         config.MaxTokens,
		Temperature:       config.Temperature,
		TopP:              config.TopP,
		TopK:              config.TopK,
		FrequencyPenalty:  config.FrequencyPenalty,
		PresencePenalty:   config.PresencePenalty,
		NumberOfResponses: config.NumberOfResponses,
		Input:             config.Input,
		Version:           config.Version,
		Continue:          config.Continue,
	})
	if err != nil {
		slog.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}

	input, err := readInput(config.Input)
	if err != nil {
		slog.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}

	if err := executor.Execute(input); err != nil {
		slog.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}

	// Save messages to file
	f, err := os.Create(".cpeconvo")
	if err != nil {
		slog.Error("failed to create conversation file", slog.Any("err", err))
		os.Exit(1)
	}
	defer f.Close()

	if err := executor.SaveMessages(f); err != nil {
		slog.Error("failed to save messages", slog.Any("err", err))
		os.Exit(1)
	}
}

func parseConfig() (cliopts.Options, error) {
	cliopts.ParseFlags()

	if cliopts.Opts.Version {
		fmt.Printf("cpe version %s\n", getVersion())
		os.Exit(0)
	}

	if cliopts.Opts.Model != "" && cliopts.Opts.Model != agent.DefaultModel {
		_, ok := agent.ModelConfigs[cliopts.Opts.Model]
		if !ok && cliopts.Opts.CustomURL == "" {
			return cliopts.Options{}, fmt.Errorf("unknown model '%s' requires -custom-url flag", cliopts.Opts.Model)
		}
	}

	return cliopts.Opts, nil
}

func readInput(inputPath string) (string, error) {
	var inputs []string

	// Check if there is any input from stdin by checking if stdin is a pipe or redirection
	//stat, _ := os.Stdin.Stat()
	//if (stat.Mode() & os.ModeCharDevice) == 0 {
	//	// Stdin has data available
	//	content, err := io.ReadAll(os.Stdin)
	//	if err != nil {
	//		return "", fmt.Errorf("error reading from stdin: %w", err)
	//	}
	//	if len(content) > 0 {
	//		inputs = append(inputs, string(content))
	//	}
	//}

	// Check if there is input from the -input flag
	if inputPath != "" {
		content, err := os.ReadFile(inputPath)
		if err != nil {
			return "", fmt.Errorf("error opening input file %s: %w", inputPath, err)
		}
		if len(content) > 0 {
			inputs = append(inputs, string(content))
		}
	}

	// Check if there is input from command line arguments
	if cliopts.Opts.Prompt != "" {
		inputs = append(inputs, cliopts.Opts.Prompt)
	}

	// Combine all inputs with double newlines
	input := strings.Join(inputs, "\n\n")

	if input == "" {
		return "", fmt.Errorf("no input provided. Please provide input via stdin, input file, or as a command line argument")
	}

	return input, nil
}

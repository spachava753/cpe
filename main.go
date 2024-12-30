package main

import (
	_ "embed"
	"fmt"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/cliopts"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/tokentree"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
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
	logger := slog.Default()
	startTime := time.Now()
	defer func() {
		elapsed := time.Since(startTime)
		logger.Info("finished execution", "elapsed", elapsed)
	}()

	config, err := parseConfig()
	if err != nil {
		logger.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}

	if config.TokenCountPath != "" {
		ignorer, err := ignore.LoadIgnoreFiles(".")
		if err != nil {
			logger.Error("fatal error", slog.Any("err", err))
			os.Exit(1)
		}
		if ignorer == nil {
			logger.Error("git ignorer was nil")
			os.Exit(1)
		}
		if err := tokentree.PrintTokenTree(os.DirFS("."), ignorer); err != nil {
			slog.Error("fatal error", slog.Any("err", err))
			os.Exit(1)
		}
		return
	}

	executor, err := agent.InitExecutor(logger, agent.ModelOptions{
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
	var input string

	// Read from stdin or file if provided
	if inputPath != "" && inputPath != "-" {
		// Read from file
		content, err := os.ReadFile(inputPath)
		if err != nil {
			return "", fmt.Errorf("error opening input file %s: %w", inputPath, err)
		}
		input = string(content)
	} else if inputPath == "-" {
		// Read from stdin
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		input = string(content)
	}

	// If we have a prompt from command line arguments, append it to any existing input
	if cliopts.Opts.Prompt != "" {
		if input != "" {
			input = input + "\n\n" + cliopts.Opts.Prompt
		} else {
			input = cliopts.Opts.Prompt
		}
	}

	if input == "" {
		return "", fmt.Errorf("no input provided. Please provide input via stdin, input file, or as a command line argument")
	}

	return input, nil
}

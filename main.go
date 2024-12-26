package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/cliopts"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/llm"
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
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		logger.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}
	if ignorer == nil {
		logger.Error("git ignorer was nil")
		os.Exit(1)
	}

	if config.TokenCountPath != "" {
		if err := tokentree.PrintTokenTree(os.DirFS("."), ignorer); err != nil {
			slog.Error("fatal error", slog.Any("err", err))
			os.Exit(1)
		}
		return
	}

	// Check for custom URL in environment variable
	customURL := config.CustomURL
	if envURL := os.Getenv("CPE_CUSTOM_URL"); customURL == "" && envURL != "" {
		customURL = envURL
	}

	provider, genConfig, err := llm.GetProvider(logger, config.Model, llm.ModelOptions{
		Model:             config.Model,
		CustomURL:         customURL,
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

	if closer, ok := provider.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	input, err := readInput(config.Input)
	if err != nil {
		slog.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}

	executor, err := agent.NewExecutor(
		customURL,
		provider,
		genConfig,
		slog.Default(),
		ignorer,
	)
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

	if cliopts.Opts.Model != "" && cliopts.Opts.Model != llm.DefaultModel {
		_, ok := llm.ModelConfigs[cliopts.Opts.Model]
		if !ok && cliopts.Opts.CustomURL == "" {
			return cliopts.Options{}, fmt.Errorf("unknown model '%s' requires -custom-url flag", cliopts.Opts.Model)
		}
	}

	return cliopts.Opts, nil
}

func readInput(inputPath string) (string, error) {
	var r io.Reader
	if inputPath == "-" {
		r = bufio.NewReader(os.Stdin)
	} else {
		var err error
		r, err = os.Open(inputPath)
		if err != nil {
			return "", fmt.Errorf("error opening input file %s: %w", inputPath, err)
		}
	}
	content, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}

	if len(content) == 0 {
		return "", fmt.Errorf("no input provided. Please provide input via stdin or specify an input file")
	}

	return string(content), nil
}

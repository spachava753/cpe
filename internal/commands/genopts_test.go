package commands

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestBuildGenOptsFromFlags(t *testing.T) {
	t.Run("no flags set returns nil", func(t *testing.T) {
		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		var temp float64
		flags.Float64Var(&temp, "temperature", 0, "")

		result := BuildGenOptsFromFlags(flags, GenParamFlags{Temperature: &temp})
		if result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})

	t.Run("one flag set populates only that field", func(t *testing.T) {
		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		var temp float64
		var maxTokens int
		flags.Float64Var(&temp, "temperature", 0, "")
		flags.IntVar(&maxTokens, "max-tokens", 0, "")

		if err := flags.Parse([]string{"--temperature", "0.5"}); err != nil {
			t.Fatal(err)
		}

		result := BuildGenOptsFromFlags(flags, GenParamFlags{
			Temperature: &temp,
			MaxTokens:   &maxTokens,
		})
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Temperature == nil || *result.Temperature != 0.5 {
			t.Errorf("expected Temperature=0.5, got %v", result.Temperature)
		}
		if result.MaxGenerationTokens != nil {
			t.Errorf("expected MaxGenerationTokens=nil, got %v", *result.MaxGenerationTokens)
		}
	})

	t.Run("flag set to zero returns non-nil pointer to zero", func(t *testing.T) {
		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		var temp float64
		flags.Float64Var(&temp, "temperature", 0, "")

		if err := flags.Parse([]string{"--temperature", "0"}); err != nil {
			t.Fatal(err)
		}

		result := BuildGenOptsFromFlags(flags, GenParamFlags{Temperature: &temp})
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Temperature == nil {
			t.Fatal("expected non-nil Temperature pointer")
		}
		if *result.Temperature != 0.0 {
			t.Errorf("expected Temperature=0.0, got %f", *result.Temperature)
		}
	})

	t.Run("multiple flags set", func(t *testing.T) {
		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		var temp float64
		var maxTokens int
		var topK uint
		var thinkingBudget string
		flags.Float64Var(&temp, "temperature", 0, "")
		flags.IntVar(&maxTokens, "max-tokens", 0, "")
		flags.UintVar(&topK, "top-k", 0, "")
		flags.StringVar(&thinkingBudget, "thinking-budget", "", "")

		if err := flags.Parse([]string{"--temperature", "0.8", "--max-tokens", "4096", "--thinking-budget", "high"}); err != nil {
			t.Fatal(err)
		}

		result := BuildGenOptsFromFlags(flags, GenParamFlags{
			Temperature:    &temp,
			MaxTokens:      &maxTokens,
			TopK:           &topK,
			ThinkingBudget: thinkingBudget,
		})
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Temperature == nil || *result.Temperature != 0.8 {
			t.Errorf("expected Temperature=0.8, got %v", result.Temperature)
		}
		if result.MaxGenerationTokens == nil || *result.MaxGenerationTokens != 4096 {
			t.Errorf("expected MaxGenerationTokens=4096, got %v", result.MaxGenerationTokens)
		}
		if result.TopK != nil {
			t.Errorf("expected TopK=nil (not set), got %v", *result.TopK)
		}
		if result.ThinkingBudget != "high" {
			t.Errorf("expected ThinkingBudget=high, got %q", result.ThinkingBudget)
		}
	})
}

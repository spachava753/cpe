package commands

import "testing"

func TestBuildGenOpts(t *testing.T) {
	t.Run("no changes returns nil", func(t *testing.T) {
		temp := 0.0
		result := BuildGenOpts(GenParamValues{Temperature: &temp}, GenParamChanges{})
		if result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})

	t.Run("one changed field populates only that field", func(t *testing.T) {
		temp := 0.5
		maxTokens := 0
		result := BuildGenOpts(
			GenParamValues{
				Temperature: &temp,
				MaxTokens:   &maxTokens,
			},
			GenParamChanges{Temperature: true},
		)
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

	t.Run("changed field preserves explicit zero values", func(t *testing.T) {
		temp := 0.0
		result := BuildGenOpts(
			GenParamValues{Temperature: &temp},
			GenParamChanges{Temperature: true},
		)
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

	t.Run("multiple changed fields", func(t *testing.T) {
		temp := 0.8
		maxTokens := 4096
		topK := uint(0)
		result := BuildGenOpts(
			GenParamValues{
				Temperature:    &temp,
				MaxTokens:      &maxTokens,
				TopK:           &topK,
				ThinkingBudget: "high",
			},
			GenParamChanges{
				Temperature:    true,
				MaxTokens:      true,
				ThinkingBudget: true,
			},
		)
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
			t.Errorf("expected TopK=nil (not changed), got %v", *result.TopK)
		}
		if result.ThinkingBudget != "high" {
			t.Errorf("expected ThinkingBudget=high, got %q", result.ThinkingBudget)
		}
	})
}

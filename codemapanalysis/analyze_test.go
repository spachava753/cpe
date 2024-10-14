package codemapanalysis

import (
	"encoding/json"
	"github.com/spachava753/cpe/cliopts"
	"github.com/spachava753/cpe/llm"
	"github.com/stretchr/testify/assert"
	"testing"
)

// CustomMockLLMProvider is a custom mock implementation of the LLMProvider interface
type CustomMockLLMProvider struct {
	t           *testing.T
	calls       []func(llm.GenConfig, llm.Conversation) (llm.Message, llm.TokenUsage, error)
	currentCall int
}

func NewCustomMockLLMProvider(t *testing.T, calls []func(llm.GenConfig, llm.Conversation) (llm.Message, llm.TokenUsage, error), realProvider llm.LLMProvider) *CustomMockLLMProvider {
	return &CustomMockLLMProvider{
		t:     t,
		calls: calls,
	}
}

func (m *CustomMockLLMProvider) GenerateResponse(config llm.GenConfig, conversation llm.Conversation) (llm.Message, llm.TokenUsage, error) {
	if m.currentCall >= len(m.calls) {
		m.t.Error("Too many calls to GenerateResponse")
		return llm.Message{}, llm.TokenUsage{}, nil
	}

	response, usage, err := m.calls[m.currentCall](config, conversation)
	m.currentCall++

	return response, usage, err
}

func (m *CustomMockLLMProvider) AssertExpectations() {
	if m.currentCall < len(m.calls) {
		m.t.Errorf("Expected %d calls to GenerateResponse, but got %d", len(m.calls), m.currentCall)
	}
}

func TestPerformAnalysis_Retry(t *testing.T) {
	cliopts.ParseFlags()

	if cliopts.Opts.Model != "" && cliopts.Opts.Model != llm.DefaultModel {
		_, ok := llm.ModelConfigs[cliopts.Opts.Model]
		if !ok && cliopts.Opts.CustomURL == "" {
			t.Fatalf("Error: Unknown model '%s' requires -custom-url flag\n", cliopts.Opts.Model)
		}
	}

	provider, genConfig, err := llm.GetProvider(cliopts.Opts.Model, llm.ModelOptions{
		Model:             cliopts.Opts.Model,
		CustomURL:         cliopts.Opts.CustomURL,
		MaxTokens:         cliopts.Opts.MaxTokens,
		Temperature:       cliopts.Opts.Temperature,
		TopP:              cliopts.Opts.TopP,
		TopK:              cliopts.Opts.TopK,
		FrequencyPenalty:  cliopts.Opts.FrequencyPenalty,
		PresencePenalty:   cliopts.Opts.PresencePenalty,
		NumberOfResponses: cliopts.Opts.NumberOfResponses,
	})
	if err != nil {
		t.Fatalf("Error initializing provider: %v\n", err)
	}

	if closer, ok := provider.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	// Define the mock calls
	mockCalls := []func(llm.GenConfig, llm.Conversation) (llm.Message, llm.TokenUsage, error){
		// First call: return a malformed response
		func(config llm.GenConfig, conversation llm.Conversation) (llm.Message, llm.TokenUsage, error) {
			return llm.Message{
				Role: "assistant",
				Content: []llm.ContentBlock{
					{
						Type: "tool_use",
						ToolUse: &llm.ToolUse{
							ID:   "toolu_01CkfkQxg335fJ4yjUyaWStU",
							Name: "select_files_for_analysis",
							Input: json.RawMessage(`{
        "thinking": "To answer the user's query about what testing packages are being used, we need to analyze the import statements and function signatures related to testing. Based on the provided code map, there are two relevant files that contain testing-related code:\n\n1. main.go:\n   - This file imports the \"github.com/stretchr/testify/assert\" package, which is a popular testing utility for Go.\n   - It also imports the standard \"testing\" package.\n   - It contains a TestMain function, which is typically used for test setup and teardown.\n\n2. llm/types.go:\n   - This file imports the standard \"testing\" package.\n   - It contains a TestTokenUsage function, which appears to be a test function.\n\nBoth files are relevant to answering the query about testing packages. By analyzing these files, we can determine the testing packages used in the project.\n",
        "selected_files": "main.go, llm/types.go"
      }
`),
						},
					},
				},
			}, llm.TokenUsage{InputTokens: 100, OutputTokens: 50}, nil
		},
		// Second call: delegate to the real provider
		func(config llm.GenConfig, conversation llm.Conversation) (llm.Message, llm.TokenUsage, error) {
			return provider.GenerateResponse(config, conversation)
		},
	}

	mockProvider := NewCustomMockLLMProvider(t, mockCalls, provider)

	// Prepare test inputs
	codeMapOutput := `<code_map>
<file path="main.go">
package main

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMain(t *testing.T)
</file>
<file path="llm/types.go">
package llm

import "testing"

func TestTokenUsage(t *testing.T)
</file>
</code_map>`
	userQuery := "What testing packages am I using?"

	// Call the function under test
	selectedFiles, err := PerformAnalysis(mockProvider, genConfig, codeMapOutput, userQuery)

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, []string{"main.go", "llm/types.go"}, selectedFiles)

	// Verify that the mock's expectations were met
	mockProvider.AssertExpectations()
}

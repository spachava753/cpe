package commands

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/config"
)

func TestModelList(t *testing.T) {
	tests := []struct {
		name               string
		config             *config.Config
		defaultModel       string
		wantOutputContains []string
	}{
		{
			name: "list models without default",
			config: &config.Config{
				Models: []config.ModelConfig{
					{Model: config.Model{Ref: "model1"}},
					{Model: config.Model{Ref: "model2"}},
				},
			},
			defaultModel: "",
			wantOutputContains: []string{
				"model1",
				"model2",
			},
		},
		{
			name: "list models with default marked",
			config: &config.Config{
				Models: []config.ModelConfig{
					{Model: config.Model{Ref: "model1"}},
					{Model: config.Model{Ref: "model2"}},
				},
			},
			defaultModel: "model1",
			wantOutputContains: []string{
				"model1 (default)",
				"model2",
			},
		},
		{
			name: "empty model list",
			config: &config.Config{
				Models: []config.ModelConfig{},
			},
			defaultModel:       "",
			wantOutputContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := ModelListOptions{
				Config:       tt.config,
				DefaultModel: tt.defaultModel,
				Writer:       &buf,
			}

			err := ModelList(context.Background(), opts)
			if err != nil {
				t.Errorf("ModelList() error = %v", err)
				return
			}

			output := buf.String()
			for _, want := range tt.wantOutputContains {
				if !strings.Contains(output, want) {
					t.Errorf("ModelList() output does not contain %q\nOutput: %s", want, output)
				}
			}
		})
	}
}

func TestModelInfo(t *testing.T) {
	tests := []struct {
		name               string
		config             *config.Config
		modelName          string
		wantErr            bool
		errMsg             string
		wantOutputContains []string
	}{
		{
			name: "show model info",
			config: &config.Config{
				Models: []config.ModelConfig{
					{
						Model: config.Model{
							Ref:                  "test-model",
							DisplayName:          "Test Model",
							Type:                 "openai",
							ID:                   "gpt-4",
							ContextWindow:        8192,
							MaxOutput:            4096,
							InputCostPerMillion:  1.5,
							OutputCostPerMillion: 2.5,
						},
					},
				},
			},
			modelName: "test-model",
			wantErr:   false,
			wantOutputContains: []string{
				"Ref: test-model",
				"Display Name: Test Model",
				"Type: openai",
				"ID: gpt-4",
				"Context: 8192",
				"MaxOutput: 4096",
			},
		},
		{
			name: "model not found",
			config: &config.Config{
				Models: []config.ModelConfig{
					{Model: config.Model{Ref: "existing-model"}},
				},
			},
			modelName: "nonexistent-model",
			wantErr:   true,
			errMsg:    "model \"nonexistent-model\" not found",
		},
		{
			name: "no model name provided",
			config: &config.Config{
				Models: []config.ModelConfig{},
			},
			modelName: "",
			wantErr:   true,
			errMsg:    "no model name provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := ModelInfoOptions{
				Config:    tt.config,
				ModelName: tt.modelName,
				Writer:    &buf,
			}

			err := ModelInfo(context.Background(), opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ModelInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ModelInfo() error = %v, want error containing %q", err, tt.errMsg)
			}

			if !tt.wantErr {
				output := buf.String()
				for _, want := range tt.wantOutputContains {
					if !strings.Contains(output, want) {
						t.Errorf("ModelInfo() output does not contain %q\nOutput: %s", want, output)
					}
				}
			}
		})
	}
}

// mockSystemPromptRenderer is a test implementation of SystemPromptRenderer
type mockSystemPromptRenderer struct {
	renderFunc func(template string, model *config.Model) (string, error)
}

func (m *mockSystemPromptRenderer) Render(template string, model *config.Model) (string, error) {
	if m.renderFunc != nil {
		return m.renderFunc(template, model)
	}
	return "rendered prompt", nil
}

func TestModelSystemPrompt(t *testing.T) {
	tests := []struct {
		name               string
		config             *config.Config
		modelName          string
		template           string
		templatePath       string
		renderFunc         func(template string, model *config.Model) (string, error)
		wantErr            bool
		errMsg             string
		wantOutputContains []string
	}{
		{
			name: "show system prompt",
			config: &config.Config{
				Models: []config.ModelConfig{
					{
						Model: config.Model{
							Ref: "test-model",
						},
					},
				},
			},
			modelName:    "test-model",
			template:     "You are a helpful assistant",
			templatePath: "prompt.txt",
			renderFunc: func(template string, model *config.Model) (string, error) {
				return "Test prompt content", nil
			},
			wantErr: false,
			wantOutputContains: []string{
				"Model: test-model",
				"Path: prompt.txt",
				"Test prompt content",
			},
		},
		{
			name: "model without system prompt",
			config: &config.Config{
				Models: []config.ModelConfig{
					{
						Model: config.Model{
							Ref: "test-model",
						},
					},
				},
			},
			modelName: "test-model",
			template:  "",
			wantErr:   false,
			wantOutputContains: []string{
				"does not define a system prompt",
			},
		},
		{
			name: "model not found",
			config: &config.Config{
				Models: []config.ModelConfig{},
			},
			modelName: "nonexistent-model",
			wantErr:   true,
			errMsg:    "model \"nonexistent-model\" not found",
		},
		{
			name: "no model specified",
			config: &config.Config{
				Models: []config.ModelConfig{},
			},
			modelName: "",
			wantErr:   true,
			errMsg:    "no model specified",
		},
		{
			name: "render error",
			config: &config.Config{
				Models: []config.ModelConfig{
					{
						Model: config.Model{
							Ref: "test-model",
						},
					},
				},
			},
			modelName:    "test-model",
			template:     "test template",
			templatePath: "prompt.txt",
			renderFunc: func(template string, model *config.Model) (string, error) {
				return "", errors.New("render failed")
			},
			wantErr: true,
			errMsg:  "failed to render system prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := ModelSystemPromptOptions{
				Config:               tt.config,
				ModelName:            tt.modelName,
				SystemPromptTemplate: tt.template,
				SystemPromptPath:     tt.templatePath,
				Writer:               &buf,
				SystemPromptRenderer: &mockSystemPromptRenderer{renderFunc: tt.renderFunc},
			}

			err := ModelSystemPrompt(context.Background(), opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ModelSystemPrompt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ModelSystemPrompt() error = %v, want error containing %q", err, tt.errMsg)
			}

			if !tt.wantErr {
				output := buf.String()
				for _, want := range tt.wantOutputContains {
					if !strings.Contains(output, want) {
						t.Errorf("ModelSystemPrompt() output does not contain %q\nOutput: %s", want, output)
					}
				}
			}
		})
	}
}

package commands

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/cpe/internal/config"
)

func TestModelList(t *testing.T) {
	tests := []struct {
		name         string
		config       *config.RawConfig
		defaultModel string
	}{
		{
			name: "list models without default",
			config: &config.RawConfig{
				Models: []config.ModelConfig{
					{Model: config.Model{Ref: "model1"}},
					{Model: config.Model{Ref: "model2"}},
				},
			},
			defaultModel: "",
		},
		{
			name: "list models with default marked",
			config: &config.RawConfig{
				Models: []config.ModelConfig{
					{Model: config.Model{Ref: "model1"}},
					{Model: config.Model{Ref: "model2"}},
				},
			},
			defaultModel: "model1",
		},
		{
			name: "empty model list",
			config: &config.RawConfig{
				Models: []config.ModelConfig{},
			},
			defaultModel: "",
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

			cupaloy.SnapshotT(t, buf.String())
		})
	}
}

func TestModelInfo(t *testing.T) {
	tests := []struct {
		name      string
		config    *config.RawConfig
		modelName string
		wantErr   bool
		errMsg    string
	}{
		{
			name: "show model info",
			config: &config.RawConfig{
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
		},
		{
			name: "model not found",
			config: &config.RawConfig{
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
			config: &config.RawConfig{
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
				cupaloy.SnapshotT(t, buf.String())
			}
		})
	}
}

// mockFile implements fs.File for testing purposes
type mockFile struct {
	reader io.Reader
	name   string
}

func newMockFile(content, name string) *mockFile {
	return &mockFile{
		reader: strings.NewReader(content),
		name:   name,
	}
}

func (m *mockFile) Read(p []byte) (n int, err error) {
	return m.reader.Read(p)
}

func (m *mockFile) Close() error {
	return nil
}

func (m *mockFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{name: m.name}, nil
}

type mockFileInfo struct {
	name string
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return 0 }
func (m *mockFileInfo) Mode() fs.FileMode  { return 0 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() any           { return nil }

func TestModelSystemPrompt(t *testing.T) {
	tests := []struct {
		name         string
		config       *config.RawConfig
		modelName    string
		systemPrompt fs.File
		wantErr      bool
		errMsg       string
	}{
		{
			name: "show system prompt",
			config: &config.RawConfig{
				Models: []config.ModelConfig{
					{
						Model:            config.Model{Ref: "test-model"},
						SystemPromptPath: "prompt.txt",
					},
				},
			},
			modelName:    "test-model",
			systemPrompt: newMockFile("Test prompt content", "prompt.txt"),
			wantErr:      false,
		},
		{
			name: "model without system prompt",
			config: &config.RawConfig{
				Models: []config.ModelConfig{
					{
						Model: config.Model{
							Ref: "test-model",
						},
					},
				},
			},
			modelName:    "test-model",
			systemPrompt: nil,
			wantErr:      false,
		},
		{
			name: "model not found",
			config: &config.RawConfig{
				Models: []config.ModelConfig{},
			},
			modelName: "nonexistent-model",
			wantErr:   true,
			errMsg:    "model \"nonexistent-model\" not found",
		},
		{
			name: "no model specified",
			config: &config.RawConfig{
				Models: []config.ModelConfig{},
			},
			modelName: "",
			wantErr:   true,
			errMsg:    "no model specified",
		},
		{
			name: "render error",
			config: &config.RawConfig{
				Models: []config.ModelConfig{
					{
						Model:            config.Model{Ref: "test-model"},
						SystemPromptPath: "prompt.txt",
					},
				},
			},
			modelName:    "test-model",
			systemPrompt: newMockFile("{{ invalid template syntax", "prompt.txt"),
			wantErr:      true,
			errMsg:       "failed to parse template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := ModelSystemPromptOptions{
				Config:       tt.config,
				ModelName:    tt.modelName,
				SystemPrompt: tt.systemPrompt,
				Output:       &buf,
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
				cupaloy.SnapshotT(t, buf.String())
			}
		})
	}
}

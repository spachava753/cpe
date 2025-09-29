package config

import (
	"testing"
	"testing/fstest"
)

func TestLoadConfigFromFileFormats(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		filename   string
		expectName string
		expectType string
		wantErr    bool
	}{
		{
			name: "YAML config",
			content: `
version: "1.0"
models:
  - name: "test-yaml"
    id: "test-yaml-id"
    type: "openai"
    context_window: 8192
    max_output: 4096
`,
			filename:   "config.yaml",
			expectName: "test-yaml",
			expectType: "openai",
			wantErr:    false,
		},
		{
			name: "JSON config",
			content: `{
  "version": "1.0",
  "models": [
    {
      "name": "test-json",
      "id": "test-json-id", 
      "type": "anthropic",
      "context_window": 16384,
      "max_output": 8192
    }
  ]
}`,
			filename:   "config.json",
			expectName: "test-json",
			expectType: "anthropic",
			wantErr:    false,
		},
		{
			name: "YML extension",
			content: `
version: "1.0"
models:
  - name: "test-yml"
    id: "test-yml-id"
    type: "gemini"
`,
			filename:   "config.yml",
			expectName: "test-yml",
			expectType: "gemini",
			wantErr:    false,
		},
		{
			name: "No extension fallback to YAML",
			content: `
version: "1.0"
models:
  - name: "test-noext"
    id: "test-noext-id"
    type: "groq"
`,
			filename:   "config",
			expectName: "test-noext",
			expectType: "groq",
			wantErr:    false,
		},
		{
			name: "Invalid JSON",
			content: `{
  "version": "1.0"
  "models": [  // missing comma
    {
      "name": "test"
    }
  ]
}`,
			filename: "config.json",
			wantErr:  true,
		},
		{
			name: "Invalid YAML",
			content: `
version: 1.0
models:
  - name: test
    invalid: [unclosed array
`,
			filename: "config.yaml",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test filesystem with the config file
			testFS := fstest.MapFS{
				tt.filename: &fstest.MapFile{
					Data: []byte(tt.content),
				},
			}

			file, err := testFS.Open(tt.filename)
			if err != nil {
				t.Fatalf("Failed to open test file: %v", err)
			}
			defer file.Close()

			config, err := loadConfigFromFile(file)

			if tt.wantErr {
				if err == nil {
					t.Errorf("loadConfigFromFile() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("loadConfigFromFile() unexpected error: %v", err)
				return
			}

			if len(config.Models) == 0 {
				t.Fatal("Expected at least one model")
			}

			model := config.Models[0]
			if model.Name != tt.expectName {
				t.Errorf("Expected model name %s, got %s", tt.expectName, model.Name)
			}

			if model.Type != tt.expectType {
				t.Errorf("Expected model type %s, got %s", tt.expectType, model.Type)
			}
		})
	}
}

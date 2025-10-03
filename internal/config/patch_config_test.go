package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPatchRequestConfig(t *testing.T) {
	tests := []struct {
		name                 string
		yamlData             string
		expectPatchRequest   bool
		expectJSONPatchCount int
		expectHeaderCount    int
		validateFunc         func(t *testing.T, model Model)
	}{
		{
			name: "full configuration with patches and headers",
			yamlData: `
name: test-model
id: test-id
type: openai
api_key_env: TEST_KEY
context_window: 4096
max_output: 2048
patchRequest:
  jsonPatch:
    - op: add
      path: /custom_field
      value: custom_value
    - op: replace
      path: /model
      value: custom-model
  includeHeaders:
    X-Custom-Header: header-value
    X-Another: another-value
`,
			expectPatchRequest:   true,
			expectJSONPatchCount: 2,
			expectHeaderCount:    2,
			validateFunc: func(t *testing.T, model Model) {
				if model.PatchRequest.IncludeHeaders["X-Custom-Header"] != "header-value" {
					t.Errorf("Expected X-Custom-Header to be 'header-value', got %q", model.PatchRequest.IncludeHeaders["X-Custom-Header"])
				}

				patch1 := model.PatchRequest.JSONPatch[0]
				if patch1["op"] != "add" {
					t.Errorf("Expected first patch op to be 'add', got %q", patch1["op"])
				}
				if patch1["path"] != "/custom_field" {
					t.Errorf("Expected first patch path to be '/custom_field', got %q", patch1["path"])
				}
			},
		},
		{
			name: "no patching",
			yamlData: `
name: test-model
id: test-id
type: openai
api_key_env: TEST_KEY
context_window: 4096
max_output: 2048
`,
			expectPatchRequest:   false,
			expectJSONPatchCount: 0,
			expectHeaderCount:    0,
		},
		{
			name: "only headers",
			yamlData: `
name: test-model
id: test-id
type: openai
api_key_env: TEST_KEY
context_window: 4096
max_output: 2048
patchRequest:
  includeHeaders:
    X-Custom: value
`,
			expectPatchRequest:   true,
			expectJSONPatchCount: 0,
			expectHeaderCount:    1,
		},
		{
			name: "only JSON patch",
			yamlData: `
name: test-model
id: test-id
type: openai
api_key_env: TEST_KEY
context_window: 4096
max_output: 2048
patchRequest:
  jsonPatch:
    - op: add
      path: /field
      value: data
`,
			expectPatchRequest:   true,
			expectJSONPatchCount: 1,
			expectHeaderCount:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var model Model
			if err := yaml.Unmarshal([]byte(tt.yamlData), &model); err != nil {
				t.Fatalf("Failed to unmarshal YAML: %v", err)
			}

			if tt.expectPatchRequest {
				if model.PatchRequest == nil {
					t.Fatal("Expected PatchRequest to be non-nil")
				}

				if len(model.PatchRequest.JSONPatch) != tt.expectJSONPatchCount {
					t.Errorf("Expected %d JSON patches, got %d", tt.expectJSONPatchCount, len(model.PatchRequest.JSONPatch))
				}

				if len(model.PatchRequest.IncludeHeaders) != tt.expectHeaderCount {
					t.Errorf("Expected %d headers, got %d", tt.expectHeaderCount, len(model.PatchRequest.IncludeHeaders))
				}

				if tt.validateFunc != nil {
					tt.validateFunc(t, model)
				}
			} else {
				if model.PatchRequest != nil {
					t.Errorf("Expected PatchRequest to be nil, got %+v", model.PatchRequest)
				}
			}
		})
	}
}

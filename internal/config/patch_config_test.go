package config

import (
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"gopkg.in/yaml.v3"
)

func TestPatchRequestConfig(t *testing.T) {
	tests := []struct {
		name     string
		yamlData string
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var model Model
			if err := yaml.Unmarshal([]byte(tt.yamlData), &model); err != nil {
				t.Fatalf("Failed to unmarshal YAML: %v", err)
			}

			cupaloy.SnapshotT(t, model)
		})
	}
}

# Product Requirements Document: Request Patching Feature

## Executive Summary

This PRD introduces a request patching feature to CPE that allows users to modify HTTP request bodies and headers for model API calls through configuration. The feature enables users to customize requests sent to model providers by specifying JSON patches and additional headers in their YAML configuration, providing greater flexibility when working with different model APIs.

## Background and Problem Statement

### Current State

Currently, CPE's model configuration in
`internal/config/` supports standard parameters like model IDs, API keys, base URLs, and cost settings, but does not allow users to modify the actual HTTP request body or add custom headers when communicating with model APIs. The existing codebase shows that different model types (openai, anthropic, gemini) have their own specific request/response structures defined in
`internal/agent/`.

### Pain Points

1. **Limited API customization
   **: Users cannot modify request payloads when model providers have specific format requirements or when custom fields need to be added
2. **Missing header support
   **: Some model providers require custom headers that cannot be currently specified in the configuration
3. **Provider-specific workarounds
   **: Users need to manually patch CPE code to work with model providers that have non-standard request formats
4. **Reduced flexibility
   **: Power users who need to experiment with different API parameters are constrained by CPE's fixed request formats

## Goals and Outcomes

### Goals

1. Enable users to patch request JSON payloads using standard JSON Patch operations
2. Allow users to add custom headers to model API requests
3. Maintain backward compatibility with existing configurations
4. Provide a clean, extensible implementation that works across all model types
5. Ensure request patching is transparent to response handling

### Outcomes

After implementation, users will be able to:

- Add, remove, or modify fields in request JSON bodies using JSON Patch syntax
- Include custom headers in their model API requests
- Work with model providers that have specific request format requirements
- Experiment with different API configurations without modifying CPE source code

## Requirements

### Functional Requirements

1. **JSON Patch Support**
    - Support standard JSON Patch operations (add, remove, replace, move, copy, test)
    - Allow multiple patch operations to be applied in sequence
    - Validate patch operations before applying them
    - Provide clear error messages for invalid patch operations

2. **Header Customization**
    - Support adding custom headers to requests
    - Allow header name-value pairs in configuration
    - Preserve existing headers while adding new ones

3. **Configuration Schema**
    - Add new `patchRequest` section to model configuration
    - Support nested `jsonPatch` and `includeHeaders` fields
    - Maintain YAML/JSON configuration compatibility

4. **Model Compatibility**
    - Work seamlessly with openai model type
    - Work seamlessly with anthropic model type
    - Work seamlessly with gemini model type
    - Support any future model types that use HTTP-based communication

5. **Error Handling**
    - Gracefully handle invalid JSON patches
    - Report configuration errors at startup time
    - Log applied patches for debugging purposes

### Non-Functional Requirements

1. **Performance**
    - Request patching should add minimal overhead (<5ms per request)
    - Memory usage should scale linearly with request size
    - No impact on response processing time

2. **Reliability**
    - Patching failures should not cause request failures
    - Original request should be preserved if patching fails
    - Clear error messages for debugging patching issues

3. **Maintainability**
    - Follow existing code patterns in `internal/agent/`
    - Implement as composable HTTP middleware
    - Include comprehensive unit tests

## Technical Design

### Architecture Overview

The feature implements a custom
`http.RoundTripper` that wraps an existing transport and applies the requested modifications:

```go
// internal/agent/patch_transport.go
package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/evanphx/json-patch/v5"
)

type PatchTransport struct {
	base        http.RoundTripper
	jsonPatches []jsonpatch.Patch
	headers     map[string]string
}

func NewPatchTransport(base http.RoundTripper, patches []jsonpatch.Patch, headers map[string]string) *PatchTransport {
	return &PatchTransport{
		base:        base,
		jsonPatches: patches,
		headers:     headers,
	}
}

func (t *PatchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Apply headers first
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}

	// Apply JSON patches if request body exists and has JSON content type
	if req.Body != nil && len(t.jsonPatches) > 0 {
		contentType := req.Header.Get("Content-Type")
		if contentType == "application/json" {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, fmt.Errorf("reading request body: %w", err)
			}
			req.Body.Close()

			// Apply patches
			for _, patch := range t.jsonPatches {
				modified, err := patch.Apply(body)
				if err != nil {
					return nil, fmt.Errorf("applying JSON patch: %w", err)
				}
				body = modified
			}

			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
		}
	}

	// Delegate to wrapped transport
	return t.base.RoundTrip(req)
}
```

### Configuration Changes

Update the `Model` struct in `internal/config/`:

```go
type Model struct {
Name               string             `yaml:"name" json:"name"`
ID                 string             `yaml:"id" json:"id"`
Type               string             `yaml:"type" json:"type"`
BaseURL            string             `yaml:"base_url" json:"base_url"`
APIKeyEnv          string             `yaml:"api_key_env" json:"api_key_env"`
ContextWindow      int                `yaml:"context_window" json:"context_window"`
MaxOutput          int                `yaml:"max_output" json:"max_output"`
InputCostPerMillion  float64           `yaml:"input_cost_per_million" json:"input_cost_per_million"`
OutputCostPerMillion float64           `yaml:"output_cost_per_million" json:"output_cost_per_million"`
PatchRequest       *PatchRequestConfig `yaml:"patchRequest" json:"patchRequest"`
}

type PatchRequestConfig struct {
JSONPatch      []map[string]interface{} `yaml:"jsonPatch" json:"jsonPatch"`
IncludeHeaders map[string]string        `yaml:"includeHeaders" json:"includeHeaders"`
}
```

### Integration Points

The patch transport will be integrated into the HTTP client creation for each model type:

1. **OpenAI Models** (`internal/agent/openai.go`):
    - Wrap the existing HTTP client transport with `PatchTransport`
    - Apply configuration during client initialization

2. **Anthropic Models** (`internal/agent/anthropic.go`):
    - Similar implementation with Anthropic-specific considerations

3. **Gemini Models** (`internal/agent/gemini.go`):
    - Similar implementation with Gemini-specific considerations

### Configuration Example

```yaml
models:
  - name: qwenn
    id: qwen/qwen3-next-80b-a3b-instruct
    type: openai
    base_url: https://openrouter.ai/api/v1/
    api_key_env: OPENROUTER_API_KEY
    context_window: 262100
    max_output: 16384
    input_cost_per_million: 2
    output_cost_per_million: 2
    patchRequest:
      jsonPatch:
        - op: add
          path: /custom_field
          value: custom_value
        - op: replace
          path: /model
          value: "custom-model-id"
      includeHeaders:
        X-Custom-Header: custom-value
        X-Another-Header: another-value
```

## Implementation Plan

1. **Create Core Patch Transport Implementation**
    - Implement `PatchTransport` in `internal/agent/patch_transport.go`
    - Add comprehensive unit tests for patching logic
    - Add JSON Patch dependency to `go.mod`

2. **Update Configuration Schema**
    - Modify `internal/config/config.go` to include new fields
    - Add validation for patch configuration
    - Test configuration loading and parsing

3. **Integrate with Model Clients**
    - Update OpenAI client to use patch transport
    - Update Anthropic client to use patch transport
    - Update Gemini client to use patch transport
    - Add integration tests for each model type

4. **Add Error Handling and Logging**
    - Implement graceful error handling for patch failures
    - Add debug logging for applied patches
    - Ensure proper error messages for users

5. **Documentation and Examples**
    - Update configuration examples
    - Add feature documentation
    - Create usage examples

6. **Testing and Validation**
    - Write unit tests for patch transport
    - Write integration tests for configuration
    - Validate with different model types
    - Test edge cases and error conditions

## Risks and Mitigations

| Risk                                          | Mitigation                                                                                     |
|-----------------------------------------------|------------------------------------------------------------------------------------------------|
| Invalid JSON patches causing request failures | Validate patches at configuration load time; apply guards to prevent request corruption        |
| Breaking existing configurations              | Add comprehensive validation; maintain backward compatibility; test with existing configs      |
| Complex debugging with patches applied        | Add detailed logging of applied patches; provide configuration validation tools                |
| JSON Patch library dependencies               | Use well-maintained library (`github.com/evanphx/json-patch`); return error on library failure |

## Documentation

Yes, this requires updating:

1. **README.md**: Add section about request patching feature with examples
2. **AGENT.md**: Document the new configuration fields and usage patterns
3. **examples/cpe.yaml**: Update with request patching examples
4. **docs/prds/extra_fields.md**: This PRD document

## Appendix

### JSON Patch Operations Supported

- **add**: Adds a value to an object or inserts it into an array
- **remove**: Removes a value from an object or array
- **replace**: Replaces a value
- **move**: Removes a value from one location and adds it to another
- **copy**: Copies a value from one location to another
- **test**: Tests that a value at a location is equal to a specified value

### Example Use Cases

1. **Custom Model Parameters**:

```yaml
patchRequest:
  jsonPatch:
    - op: add
      path: /parameters/temperature
      value: 0.7
```

2. **Provider-Specific Headers**:

```yaml
patchRequest:
  includeHeaders:
    HTTP-Referer: https://my-app.example.com
    X-Title: My AI App
```

3. **Replacing Default Values**:

```yaml
patchRequest:
  jsonPatch:
    - op: replace
      path: /max_tokens
      value: 8192
```

### Testing Strategy

- Unit tests for each JSON Patch operation
- Integration tests for each model type (openai, anthropic, gemini)
- Configuration validation tests
- Error handling tests
- Performance benchmarks
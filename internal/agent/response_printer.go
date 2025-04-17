package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spachava753/gai"
)

// ResponsePrinterGenerator is a wrapper around another generator that prints out
// the response returned from the wrapped generator.
type ResponsePrinterGenerator struct {
	// wrapped is the generator being wrapped
	wrapped gai.ToolCapableGenerator
}

// NewResponsePrinterGenerator creates a new ResponsePrinterGenerator
func NewResponsePrinterGenerator(wrapped gai.ToolCapableGenerator) *ResponsePrinterGenerator {
	return &ResponsePrinterGenerator{
		wrapped: wrapped,
	}
}

// formatToolCall formats the tool call JSON as a concise, human-readable summary
func formatToolCall(content string) string {
	// Try to parse the content as an object with known keys
	var call struct {
		Name       string                 `json:"name"`
		Parameters map[string]any         `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(content), &call); err != nil || call.Name == "" {
		// Fallback: attempt to parse as raw map and infer
		var data map[string]any
		if err := json.Unmarshal([]byte(content), &data); err != nil {
			return content
		}
		// Try to get name/parameters style
		name, _ := data["name"].(string)
		params, _ := data["parameters"].(map[string]any)
		if name == "" {
			// Sometimes tool name is not included; print the map as is
			return prettyPrintJSON(content)
		}
		return toolCallSummary(name, params)
	}
	return toolCallSummary(call.Name, call.Parameters)
}

// toolCallSummary returns a human-readable summary per tool type
func toolCallSummary(name string, params map[string]any) string {
	switch name {
	case "create_file", "delete_file", "edit_file", "move_file", "view_file":
		if path, ok := params["path"].(string); ok {
			return name + ": " + path
		}
		if source, ok := params["source_path"].(string); ok {
			result := name + ": from " + source
			if target, ok := params["target_path"].(string); ok {
				result += " to " + target
			}
			return result
		}
	case "create_folder", "delete_folder", "move_folder":
		if path, ok := params["path"].(string); ok {
			return name + ": " + path
		}
		if source, ok := params["source_path"].(string); ok {
			result := name + ": from " + source
			if target, ok := params["target_path"].(string); ok {
				result += " to " + target
			}
			return result
		}
	case "bash":
		if command, ok := params["command"].(string); ok {
			return "bash: " + command
		}
	case "files_overview":
		if path, ok := params["path"].(string); ok && path != "" {
			return name + ": " + path
		}
		return name
	case "get_related_files":
		if files, ok := params["input_files"].([]any); ok {
			fileList := make([]string, 0, len(files))
			for _, f := range files {
				if str, ok := f.(string); ok {
					fileList = append(fileList, str)
				}
			}
			return name + ": [" + strings.Join(fileList, ", ") + "]"
		}
	}
	// fallback: print the tool name and pretty params
	if b, err := json.Marshal(params); err == nil {
		return name + ": " + string(b)
	}
	return name
}

// prettyPrintJSON attempts to pretty print a JSON string
// Returns the pretty-printed JSON if successful, or the original string if not
func prettyPrintJSON(content string) string {
	var jsonData interface{}
	// Try to parse the content as JSON
	if err := json.Unmarshal([]byte(content), &jsonData); err != nil {
		// Not valid JSON, return the original content
		return content
	}

	// Pretty print with indentation
	prettyJSON, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		// Failed to pretty print, return the original content
		return content
	}

	return string(prettyJSON)
}

// Generate implements the gai.Generator interface.
// It calls the wrapped generator's Generate method and then prints the response.
func (g *ResponsePrinterGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	// Call the wrapped generator
	response, err := g.wrapped.Generate(ctx, dialog, options)
	if err != nil {
		return gai.Response{}, err
	}

	// Print the response without demarcation
	for _, candidate := range response.Candidates {
		for _, block := range candidate.Blocks {
			if block.ModalityType == gai.Text {
				// For non-content blocks, print the block type as well
				if block.BlockType != gai.Content {
					fmt.Printf("[%s]\n", block.BlockType)
				}

				content := block.Content.String()
				if block.BlockType == gai.ToolCall {
					content = formatToolCall(content)
				}

				// Print to stdout only (not stderr)
				fmt.Println(content)
			} else {
				// Print non-text blocks to stdout only (not stderr)
				fmt.Printf("Received non-text block of type: %s\n", block.ModalityType)
			}
		}
	}

	// Print usage metrics if available (only to stdout)
	if response.UsageMetrics != nil {
		inputTokens, hasInputTokens := response.UsageMetrics[gai.UsageMetricInputTokens]
		outputTokens, hasOutputTokens := response.UsageMetrics[gai.UsageMetricGenerationTokens]

		if hasInputTokens && hasOutputTokens {
			fmt.Printf("\nTokens used: %v input, %v output\n", inputTokens, outputTokens)
		}
	}

	return response, nil
}

// Register implements the gai.ToolRegister interface by delegating to the wrapped generator.
func (g *ResponsePrinterGenerator) Register(tool gai.Tool) error {
	return g.wrapped.Register(tool)
}

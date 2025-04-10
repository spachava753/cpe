package agent

import (
	"context"
	"encoding/json"
	"fmt"

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

				// If it's a tool call, try to pretty print the JSON content
				// Tool calls typically contain JSON structures that are easier to read when formatted
				content := block.Content.String()
				if block.BlockType == gai.ToolCall {
					content = prettyPrintJSON(content)
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

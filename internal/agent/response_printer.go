package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spachava753/gai"
	"github.com/tidwall/pretty"
)

// ResponsePrinterGenerator is a wrapper around another generator that prints out
// the response returned from the wrapped generator.
type ResponsePrinterGenerator struct {
	// wrapped is the generator being wrapped
	wrapped gai.ToolCapableGenerator
}

var redStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

// NewResponsePrinterGenerator creates a new ResponsePrinterGenerator
func NewResponsePrinterGenerator(wrapped gai.ToolCapableGenerator) *ResponsePrinterGenerator {
	return &ResponsePrinterGenerator{
		wrapped: wrapped,
	}
}

func formatToolCall(content string) string {
	var call gai.ToolCallInput
	if err := json.Unmarshal([]byte(content), &call); err != nil || call.Name == "" {
		// Fallback: just pretty print the JSON
		return prettyPrintJSON(content)
	}

	// Format similar to streaming_printer: tool name and pretty-printed parameters
	result := fmt.Sprintf("[Tool Name: %s]\n", call.Name)

	if call.Parameters != nil {
		if paramsJSON, err := json.Marshal(call.Parameters); err == nil {
			result += string(pretty.Color(paramsJSON, nil))
		}
	}

	return result
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

	// Pretty print with color formatting
	prettyJSON, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		// Failed to pretty print, return the original content
		return content
	}

	return string(pretty.Color(prettyJSON, nil))
}

// Generate implements the gai.Generator interface.
// It calls the wrapped generator's Generate method and then prints the response.
func (g *ResponsePrinterGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	// Call the wrapped generator
	response, err := g.wrapped.Generate(ctx, dialog, options)
	if err != nil {
		return gai.Response{}, err
	}

	var sb strings.Builder
	var hasToolcalls bool
	// Print the response without demarcation
	for _, candidate := range response.Candidates {
		for _, block := range candidate.Blocks {
			if block.ModalityType != gai.Text {
				fmt.Fprintf(&sb, "Received non-text block of type: %s\n", block.ModalityType)
			}

			hasToolcalls = block.BlockType == gai.ToolCall || hasToolcalls

			// For non-content blocks, print the block type as well
			if block.BlockType != gai.Content {
				fmt.Fprintf(&sb, "\n[%s]\n", block.BlockType)
			}

			content := block.Content.String()
			if block.BlockType == gai.ToolCall {
				content = formatToolCall(content)
			}

			fmt.Fprint(&sb, content)
		}
	}

	if hasToolcalls {
		fmt.Fprintln(os.Stderr, sb.String())
	} else {
		fmt.Fprintln(os.Stdout, sb.String())
	}

	return response, nil
}

// Register implements the gai.ToolRegister interface by delegating to the wrapped generator.
func (g *ResponsePrinterGenerator) Register(tool gai.Tool) error {
	return g.wrapped.Register(tool)
}

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// PrintContent formats and prints MCP tool result content
func PrintContent(content []mcp.Content) string {
	var builder strings.Builder
	
	for _, c := range content {
		switch typedContent := c.(type) {
		case mcp.TextContent:
			builder.WriteString(typedContent.Text)
			if !strings.HasSuffix(typedContent.Text, "\n") {
				builder.WriteString("\n")
			}
		default:
			// For other content types, pretty print as JSON
			jsonBytes, err := json.MarshalIndent(c, "", "  ")
			if err != nil {
				builder.WriteString(fmt.Sprintf("Error formatting content: %v\n", err))
				continue
			}
			builder.Write(jsonBytes)
			builder.WriteString("\n")
		}
	}
	
	return builder.String()
}

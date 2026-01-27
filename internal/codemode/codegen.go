package codemode

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stoewer/go-strcase"
)

const jsonNull = "null"

// GenerateToolDefinitions generates Go type definitions and function signatures
// for a list of MCP tools. Returns a single string with all types followed by
// all function declarations.
func GenerateToolDefinitions(tools []*mcp.Tool) (string, error) {
	if len(tools) == 0 {
		return "", nil
	}

	// Sort tools by name for deterministic output
	sortedTools := make([]*mcp.Tool, len(tools))
	copy(sortedTools, tools)
	slices.SortFunc(sortedTools, func(a, b *mcp.Tool) int {
		return strings.Compare(a.Name, b.Name)
	})

	var typeDefs []string
	var funcDecls []string

	for _, tool := range sortedTools {
		// Convert tool name to PascalCase for Go identifier
		goName := strcase.UpperCamelCase(tool.Name)

		// Convert and generate input type (if tool has input schema)
		inputSchema, err := convertSchema(tool.InputSchema)
		if err != nil {
			return "", fmt.Errorf("failed to convert input schema for tool %s: %w", tool.Name, err)
		}

		hasInput := inputSchema != nil && len(inputSchema.Properties) > 0
		inputTypeName := goName + "Input"

		if hasInput {
			inputTypeDef, err := SchemaToGoType(inputSchema, inputTypeName)
			if err != nil {
				return "", fmt.Errorf("failed to generate input type for tool %s: %w", tool.Name, err)
			}
			typeDefs = append(typeDefs, inputTypeDef)
		}

		// Convert and generate output type
		outputSchema, err := convertSchema(tool.OutputSchema)
		if err != nil {
			return "", fmt.Errorf("failed to convert output schema for tool %s: %w", tool.Name, err)
		}

		outputTypeName := goName + "Output"
		outputTypeDef, err := SchemaToGoType(outputSchema, outputTypeName)
		if err != nil {
			return "", fmt.Errorf("failed to generate output type for tool %s: %w", tool.Name, err)
		}
		typeDefs = append(typeDefs, outputTypeDef)

		// Generate function declaration
		funcDecl := generateFuncDecl(goName, tool.Description, inputTypeName, outputTypeName, hasInput)
		funcDecls = append(funcDecls, funcDecl)
	}

	// Combine: all types first, then all function declarations
	var result strings.Builder
	result.WriteString(strings.Join(typeDefs, "\n\n"))

	if len(typeDefs) > 0 && len(funcDecls) > 0 {
		result.WriteString("\n\n")
	}

	result.WriteString(strings.Join(funcDecls, "\n\n"))

	return result.String(), nil
}

// convertSchema converts an MCP schema (any) to *jsonschema.Schema via JSON round-trip.
// Returns nil if the input is nil or empty.
func convertSchema(schema any) (*jsonschema.Schema, error) {
	if schema == nil {
		return nil, nil
	}

	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}

	// Check for empty object
	if string(schemaJSON) == "{}" || string(schemaJSON) == jsonNull {
		return nil, nil
	}

	var result jsonschema.Schema
	if err := json.Unmarshal(schemaJSON, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// generateFuncDecl generates a function variable declaration for a tool.
func generateFuncDecl(goName, description, inputTypeName, outputTypeName string, hasInput bool) string {
	var buf strings.Builder

	// Add doc comment if description is present, handling multiline descriptions
	if description != "" {
		lines := strings.Split(description, "\n")
		for i, line := range lines {
			if i == 0 {
				buf.WriteString(fmt.Sprintf("// %s %s\n", goName, line))
			} else {
				buf.WriteString(fmt.Sprintf("// %s\n", line))
			}
		}
	}

	// Generate function signature
	if hasInput {
		buf.WriteString(fmt.Sprintf("var %s func(ctx context.Context, input %s) (%s, error)", goName, inputTypeName, outputTypeName))
	} else {
		buf.WriteString(fmt.Sprintf("var %s func(ctx context.Context) (%s, error)", goName, outputTypeName))
	}

	return buf.String()
}

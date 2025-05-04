package mcp

import (
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spachava753/gai"
)

// ConvertMCPToolToGAITool converts an MCP tool definition to a GAI tool definition
// This allows MCP tools to be registered with the tool generator
func ConvertMCPToolToGAITool(mcpTool mcp.Tool) (gai.Tool, error) {
	// Create a basic GAI tool with name and description
	gaiTool := gai.Tool{
		Name:        mcpTool.Name,
		Description: mcpTool.Description,
		InputSchema: gai.InputSchema{
			Type: gai.Object, // Default type
		},
	}

	// Validate input schema type
	if mcpTool.InputSchema.Type != "object" {
		return gai.Tool{}, fmt.Errorf("unsupported schema type: %s (only 'object' is supported)", mcpTool.InputSchema.Type)
	}

	// Copy required fields directly
	gaiTool.InputSchema.Required = mcpTool.InputSchema.Required

	// Convert properties
	if len(mcpTool.InputSchema.Properties) > 0 {
		gaiTool.InputSchema.Properties = make(map[string]gai.Property)
		for propName, propValue := range mcpTool.InputSchema.Properties {
			propMap, ok := propValue.(map[string]interface{})
			if !ok {
				return gai.Tool{}, fmt.Errorf("property '%s' is not a valid object", propName)
			}

			prop, err := convertProperty(propMap)
			if err != nil {
				return gai.Tool{}, fmt.Errorf("failed to convert property '%s': %w", propName, err)
			}

			gaiTool.InputSchema.Properties[propName] = prop
		}
	}

	return gaiTool, nil
}

// convertProperty converts a single property from MCP format to GAI format
func convertProperty(propMap map[string]interface{}) (gai.Property, error) {
	// Extract the type
	typeStr, ok := propMap["type"].(string)
	if !ok {
		return gai.Property{}, fmt.Errorf("property type is missing or not a string")
	}

	// Create a new property
	property := gai.Property{}

	// Convert the property type
	switch typeStr {
	case "string":
		property.Type = gai.String
	case "number":
		property.Type = gai.Number
	case "integer":
		property.Type = gai.Integer
	case "boolean":
		property.Type = gai.Boolean
	case "object":
		property.Type = gai.Object
	case "array":
		property.Type = gai.Array
	default:
		return gai.Property{}, fmt.Errorf("unsupported property type: %s", typeStr)
	}

	// Add description if present
	if descStr, ok := propMap["description"].(string); ok {
		property.Description = descStr
	}

	// Handle enumerations for string properties
	if property.Type == gai.String {
		if enumValues, ok := propMap["enum"].([]interface{}); ok && len(enumValues) > 0 {
			property.Enum = make([]string, len(enumValues))
			for i, val := range enumValues {
				if strVal, ok := val.(string); ok {
					property.Enum[i] = strVal
				} else {
					return gai.Property{}, fmt.Errorf("enum value is not a string")
				}
			}
		}
	}

	// Handle nested objects
	if property.Type == gai.Object {
		propProperties, ok := propMap["properties"].(map[string]interface{})
		if ok {
			property.Properties = make(map[string]gai.Property)
			for subPropName, subPropValue := range propProperties {
				subPropMap, ok := subPropValue.(map[string]interface{})
				if !ok {
					return gai.Property{}, fmt.Errorf("sub-property '%s' is not a valid object", subPropName)
				}

				subProp, err := convertProperty(subPropMap)
				if err != nil {
					return gai.Property{}, fmt.Errorf("failed to convert sub-property '%s': %w", subPropName, err)
				}

				property.Properties[subPropName] = subProp
			}
		}

		// Handle required fields for objects
		if required, ok := propMap["required"]; ok {
			// Try as []string first
			if requiredStrings, ok := required.([]string); ok && len(requiredStrings) > 0 {
				property.Required = requiredStrings
			} else if requiredValues, ok := required.([]interface{}); ok && len(requiredValues) > 0 {
				// If not []string, try as []interface{} and convert to []string
				property.Required = make([]string, len(requiredValues))
				for i, val := range requiredValues {
					if strVal, ok := val.(string); ok {
						property.Required[i] = strVal
					} else {
						return gai.Property{}, fmt.Errorf("required field is not a string")
					}
				}
			}
		}
	}

	// Handle arrays
	if property.Type == gai.Array {
		itemsMap, ok := propMap["items"].(map[string]interface{})
		if !ok {
			return gai.Property{}, fmt.Errorf("array items schema is missing or invalid")
		}

		itemsProp, err := convertProperty(itemsMap)
		if err != nil {
			return gai.Property{}, fmt.Errorf("failed to convert array items: %w", err)
		}

		property.Items = &itemsProp
	}

	return property, nil
}

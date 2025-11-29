package codemode

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stoewer/go-strcase"
)

// SchemaToGoType converts a JSON Schema to Go type definition(s).
// Returns the generated type definitions as a string (may include multiple types for nested objects).
// For nil schemas, returns a type alias to map[string]any.
// The typeName parameter specifies the name for the root type (e.g., "GetWeatherInput").
func SchemaToGoType(schema *jsonschema.Schema, typeName string) (string, error) {
	if schema == nil {
		return fmt.Sprintf("type %s = map[string]any", typeName), nil
	}

	var nestedTypes []string
	rootType, err := schemaToGoTypeInternal(schema, typeName, &nestedTypes)
	if err != nil {
		return "", err
	}

	// Build output: nested types first, then root type
	var result strings.Builder
	for _, nested := range nestedTypes {
		result.WriteString(nested)
		result.WriteString("\n\n")
	}
	result.WriteString(rootType)

	return result.String(), nil
}

// schemaToGoTypeInternal recursively converts a schema to a Go type definition.
// It accumulates nested struct definitions in nestedTypes.
func schemaToGoTypeInternal(schema *jsonschema.Schema, typeName string, nestedTypes *[]string) (string, error) {
	// Determine the schema type
	schemaType := schema.Type
	if schemaType == "" && len(schema.Types) > 0 {
		schemaType = resolveNullableType(schema.Types)
	}

	// Handle object type with properties -> generate struct
	if schemaType == "object" && len(schema.Properties) > 0 {
		return generateStruct(schema, typeName, nestedTypes)
	}

	// For objects without properties or other types, generate a type alias
	goType := jsonTypeToGo(schemaType, schema, typeName, nestedTypes)
	return fmt.Sprintf("type %s = %s", typeName, goType), nil
}

// generateStruct generates a Go struct definition from an object schema.
func generateStruct(schema *jsonschema.Schema, typeName string, nestedTypes *[]string) (string, error) {
	var buf strings.Builder

	// Add type description as doc comment if present
	if schema.Description != "" {
		buf.WriteString(fmt.Sprintf("// %s %s\n", typeName, schema.Description))
	}

	buf.WriteString(fmt.Sprintf("type %s struct {\n", typeName))

	// Sort property names for deterministic output
	propNames := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		propNames = append(propNames, name)
	}
	slices.Sort(propNames)

	for _, propName := range propNames {
		propSchema := schema.Properties[propName]
		fieldName := FieldNameToGo(propName)

		// Generate field documentation
		if propSchema.Description != "" {
			buf.WriteString(fmt.Sprintf("\t// %s %s\n", fieldName, propSchema.Description))
		}

		// Add enum values as doc comment if present
		if len(propSchema.Enum) > 0 {
			enumVals := make([]string, len(propSchema.Enum))
			for i, v := range propSchema.Enum {
				enumVals[i] = fmt.Sprintf("%q", v)
			}
			buf.WriteString(fmt.Sprintf("\t// Must be one of %s\n", strings.Join(enumVals, ", ")))
		}

		// Determine Go type for this field
		goType := resolveFieldType(propSchema, typeName, fieldName, nestedTypes)

		buf.WriteString(fmt.Sprintf("\t%s %s `json:%q`\n", fieldName, goType, propName))
	}

	buf.WriteString("}")

	return buf.String(), nil
}

// resolveFieldType determines the Go type for a schema property.
func resolveFieldType(schema *jsonschema.Schema, parentTypeName, fieldName string, nestedTypes *[]string) string {
	if schema == nil {
		return "any"
	}

	// Check for nullable type first
	schemaType := schema.Type
	isNullable := false
	if schemaType == "" && len(schema.Types) > 0 {
		schemaType, isNullable = resolveNullableTypeWithFlag(schema.Types)
	}

	// Handle nested object with properties -> generate named nested struct
	if schemaType == "object" && len(schema.Properties) > 0 {
		nestedTypeName := fmt.Sprintf("%s_%s", parentTypeName, fieldName)
		nestedDef, _ := generateStruct(schema, nestedTypeName, nestedTypes)
		*nestedTypes = append(*nestedTypes, nestedDef)

		if isNullable {
			return "*" + nestedTypeName
		}
		return nestedTypeName
	}

	// Handle array type
	if schemaType == "array" {
		itemType := resolveArrayItemType(schema, parentTypeName, fieldName, nestedTypes)
		if isNullable {
			// Nullable array is just the slice (slices are already nullable in Go)
			return "[]" + itemType
		}
		return "[]" + itemType
	}

	goType := jsonTypeToGo(schemaType, schema, parentTypeName+"_"+fieldName, nestedTypes)
	if isNullable && goType != "any" && goType != "map[string]any" {
		return "*" + goType
	}
	return goType
}

// resolveArrayItemType determines the Go type for array items.
func resolveArrayItemType(schema *jsonschema.Schema, parentTypeName, fieldName string, nestedTypes *[]string) string {
	if schema.Items == nil {
		return "any"
	}

	itemSchema := schema.Items
	itemType := itemSchema.Type
	if itemType == "" && len(itemSchema.Types) > 0 {
		itemType = resolveNullableType(itemSchema.Types)
	}

	// Handle array of objects
	if itemType == "object" && len(itemSchema.Properties) > 0 {
		nestedTypeName := fmt.Sprintf("%s_%sItem", parentTypeName, fieldName)
		nestedDef, _ := generateStruct(itemSchema, nestedTypeName, nestedTypes)
		*nestedTypes = append(*nestedTypes, nestedDef)
		return nestedTypeName
	}

	return jsonTypeToGo(itemType, itemSchema, parentTypeName+"_"+fieldName, nestedTypes)
}

// jsonTypeToGo maps JSON schema types to Go types.
func jsonTypeToGo(jsonType string, schema *jsonschema.Schema, typeName string, nestedTypes *[]string) string {
	switch jsonType {
	case "string":
		return "string"
	case "number":
		return "float64"
	case "integer":
		return "int64"
	case "boolean":
		return "bool"
	case "array":
		if schema != nil && schema.Items != nil {
			itemType := resolveArrayItemType(schema, typeName, "", nestedTypes)
			return "[]" + itemType
		}
		return "[]any"
	case "object":
		// Object without properties
		return "map[string]any"
	case "null":
		return "any"
	default:
		return "any"
	}
}

// resolveNullableType handles type arrays like ["null", "string"] and returns the non-null type.
func resolveNullableType(types []string) string {
	for _, t := range types {
		if t != "null" {
			return t
		}
	}
	return "null"
}

// resolveNullableTypeWithFlag returns the non-null type and whether the type is nullable.
func resolveNullableTypeWithFlag(types []string) (string, bool) {
	hasNull := false
	var nonNullType string

	for _, t := range types {
		if t == "null" {
			hasNull = true
		} else {
			nonNullType = t
		}
	}

	if nonNullType == "" {
		return "null", hasNull
	}
	return nonNullType, hasNull
}

// FieldNameToGo converts a JSON field name to a Go struct field name (PascalCase).
func FieldNameToGo(name string) string {
	return strcase.UpperCamelCase(name)
}

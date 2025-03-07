package agent

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// AutoCorrectJSON attempts to correct common JSON parsing issues and returns a modified JSON string.
// If the JSON is valid as-is, it returns the original string.
func AutoCorrectJSON(jsonStr string, targetStruct interface{}) (string, error) {
	// First try to unmarshal as-is
	if err := json.Unmarshal([]byte(jsonStr), targetStruct); err == nil {
		// JSON is already valid
		return jsonStr, nil
	}

	// Try to fix common JSON issues
	correctedJSON := jsonStr

	// 1. Try to fix single quotes used instead of double quotes
	if strings.Contains(correctedJSON, "'") {
		correctedJSON = fixSingleQuotes(correctedJSON)
	}

	// 2. Try to fix unquoted field names
	correctedJSON = fixUnquotedFieldNames(correctedJSON, targetStruct)

	// 3. Try to fix arrays that should be strings
	correctedJSON = fixArraysToStrings(correctedJSON, targetStruct)

	// Test if our corrections fixed the issue
	if err := json.Unmarshal([]byte(correctedJSON), targetStruct); err != nil {
		// If still failing, return the original error
		return correctedJSON, fmt.Errorf("JSON correction failed: %w\nOriginal: %s\nAttempted correction: %s", 
			err, jsonStr, correctedJSON)
	}

	return correctedJSON, nil
}

// fixSingleQuotes replaces single quotes with double quotes when they're used for JSON strings
func fixSingleQuotes(jsonStr string) string {
	// This is a simplistic approach - a more robust implementation would need to handle
	// escaping and nested quotes properly
	inString := false
	inDoubleQuoteString := false
	var result strings.Builder

	for i := 0; i < len(jsonStr); i++ {
		ch := jsonStr[i]
		
		switch ch {
		case '"':
			if !inString || inDoubleQuoteString {
				inString = !inString
				inDoubleQuoteString = inString
			}
			result.WriteByte(ch)
		case '\'':
			if !inString {
				// Convert single quote to double quote when starting/ending a string
				result.WriteByte('"')
				inString = true
			} else if !inDoubleQuoteString {
				// End the string that was started with single quote
				result.WriteByte('"')
				inString = false
			} else {
				// We're in a double-quoted string, preserve the single quote
				result.WriteByte(ch)
			}
		default:
			result.WriteByte(ch)
		}
	}
	
	return result.String()
}

// fixUnquotedFieldNames adds quotes around field names that are missing them
func fixUnquotedFieldNames(jsonStr string, targetStruct interface{}) string {
	// Get the expected field names from the target struct
	fieldNames := getStructFieldNames(targetStruct)
	
	// Simple regex-like replacement for common patterns
	result := jsonStr
	for _, field := range fieldNames {
		// Look for patterns like: field: or field :
		unquotedPattern1 := fmt.Sprintf(`%s:`, field)
		unquotedPattern2 := fmt.Sprintf(`%s :`, field)
		quotedReplacement := fmt.Sprintf(`"%s":`, field)
		
		result = strings.ReplaceAll(result, unquotedPattern1, quotedReplacement)
		result = strings.ReplaceAll(result, unquotedPattern2, quotedReplacement)
	}
	
	return result
}

// fixArraysToStrings attempts to correct cases where array notation is used for string fields
func fixArraysToStrings(jsonStr string, targetStruct interface{}) string {
	// This implementation assumes we're only dealing with simple cases
	// A more robust implementation would use a JSON parser to handle this properly
	return strings.ReplaceAll(jsonStr, "[input_files]", "\"input_files\"")
}

// getStructFieldNames returns the JSON field names from a struct type
func getStructFieldNames(targetStruct interface{}) []string {
	t := reflect.TypeOf(targetStruct)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	
	if t.Kind() != reflect.Struct {
		return nil
	}
	
	var fieldNames []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		
		// Get the json tag name
		tag := field.Tag.Get("json")
		if tag == "" {
			fieldNames = append(fieldNames, field.Name)
			continue
		}
		
		// Handle json tag options
		parts := strings.Split(tag, ",")
		if parts[0] != "" {
			fieldNames = append(fieldNames, parts[0])
		} else {
			fieldNames = append(fieldNames, field.Name)
		}
	}
	
	return fieldNames
}
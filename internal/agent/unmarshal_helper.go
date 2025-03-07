package agent

import (
	"encoding/json"
	"fmt"
)

// TryUnmarshalWithAutocorrect attempts to unmarshal JSON into a target struct.
// If standard unmarshaling fails, it attempts to autocorrect the JSON before trying again.
// Parameters:
//   - toolName: Name of the tool (for logging)
//   - jsonInput: The JSON data to unmarshal
//   - target: Pointer to the target struct to unmarshal into
//   - logger: Logger interface for logging messages
//
// Returns:
//   - error: nil if unmarshaling succeeded, otherwise the error
func TryUnmarshalWithAutocorrect(toolName string, jsonInput []byte, target interface{}, logger Logger) error {
	// First try standard unmarshaling
	unmarshalErr := json.Unmarshal(jsonInput, target)
	if unmarshalErr == nil {
		// Standard unmarshaling succeeded
		return nil
	}

	// If that fails, try to autocorrect the JSON
	logger.Printf("JSON parsing error for %s tool: %v. Attempting autocorrection.", toolName, unmarshalErr)

	// Convert to string for autocorrection
	jsonStr := string(jsonInput)
	correctedJSON, autocorrectErr := AutoCorrectJSON(jsonStr, target)
	if autocorrectErr != nil {
		logger.Printf("JSON autocorrection failed: %v", autocorrectErr)
		return fmt.Errorf("failed to unmarshal %s tool arguments: %w", toolName, unmarshalErr)
	}

	// Try again with the corrected JSON
	if err := json.Unmarshal([]byte(correctedJSON), target); err != nil {
		return fmt.Errorf("failed to unmarshal %s tool arguments even after correction: %w", toolName, err)
	}

	logger.Printf("JSON autocorrection succeeded. Original: %s, Corrected: %s", jsonStr, correctedJSON)
	return nil
}
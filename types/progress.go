package types

import (
	"encoding/json"
	"fmt"
)

// WorkDoneProgressParams represents parameters supporting work done progress.
type WorkDoneProgressParams struct {
	// WorkDoneToken is an optional token that a server can use to report work done progress.
	WorkDoneToken ProgressToken `json:"workDoneToken,omitempty"`
}

// PartialResultParams represents parameters for partial results.
type PartialResultParams struct {
	// PartialResultToken is an optional token that a server can use to report
	// partial results (e.g. streaming) to the client.
	PartialResultToken ProgressToken `json:"partialResultToken,omitempty"`
}

// ProgressToken represents a token used for progress reporting and can be either an integer or a string.
type ProgressToken struct {
	Value interface{}
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (pt *ProgressToken) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as an integer
	var intValue int
	if err := json.Unmarshal(data, &intValue); err == nil {
		pt.Value = intValue
		return nil
	}

	// If that fails, try to unmarshal as a string
	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		pt.Value = stringValue
		return nil
	}

	// If both fail, return an error
	return fmt.Errorf("failed to unmarshal ProgressToken: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (pt ProgressToken) MarshalJSON() ([]byte, error) {
	return json.Marshal(pt.Value)
}

// IsInt returns true if the ProgressToken represents an integer.
func (pt ProgressToken) IsInt() bool {
	_, ok := pt.Value.(int)
	return ok
}

// IsString returns true if the ProgressToken represents a string.
func (pt ProgressToken) IsString() bool {
	_, ok := pt.Value.(string)
	return ok
}

// Int returns the integer value if it represents an integer, or 0 otherwise.
func (pt ProgressToken) Int() int {
	if i, ok := pt.Value.(int); ok {
		return i
	}
	return 0
}

// String returns the string value if it represents a string, or an empty string otherwise.
func (pt ProgressToken) String() string {
	if s, ok := pt.Value.(string); ok {
		return s
	}
	return ""
}

// NewIntProgressToken creates a new ProgressToken with an integer value.
func NewIntProgressToken(value int) ProgressToken {
	return ProgressToken{Value: value}
}

// NewStringProgressToken creates a new ProgressToken with a string value.
func NewStringProgressToken(value string) ProgressToken {
	return ProgressToken{Value: value}
}

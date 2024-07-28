package types

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// RequestMessage represents a generic LSP request message
type RequestMessage struct {
	// The jsonrpc version. Must be "2.0"
	JSONRPC string `json:"jsonrpc"`

	// The request id
	ID interface{} `json:"id"`

	// The method to be invoked
	Method string `json:"method"`

	// The method's params
	Params json.RawMessage `json:"params,omitempty"`
}

// String implements the fmt.Stringer interface for RequestMessage.
func (r *RequestMessage) String() string {
	var paramsStr string

	if len(r.Params) > 0 {
		var prettyJSON bytes.Buffer
		err := json.Indent(&prettyJSON, r.Params, "", "  ")
		if err != nil {
			paramsStr = string(r.Params)
		} else {
			paramsStr = prettyJSON.String()
		}
	} else {
		paramsStr = "null"
	}

	return fmt.Sprintf(
		"RequestMessage{\n"+
			"  JSONRPC: %q,\n"+
			"  ID: %v,\n"+
			"  Method: %q,\n"+
			"  Params: %s\n"+
			"}",
		r.JSONRPC, r.ID, r.Method, paramsStr,
	)
}

// NewRequest creates a new RequestMessage
func NewRequest(id interface{}, method string, params interface{}) (RequestMessage, error) {
	var paramsJSON json.RawMessage
	var err error

	if params != nil {
		paramsJSON, err = json.Marshal(params)
		if err != nil {
			return RequestMessage{}, fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	return RequestMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}, nil
}

// GetParams unmarshals the params into the provided interface
func (r *RequestMessage) GetParams(v interface{}) error {
	return json.Unmarshal(r.Params, v)
}

package types

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ResponseMessage represents a generic LSP response message
type ResponseMessage struct {
	// The jsonrpc version. Must be "2.0"
	JSONRPC string `json:"jsonrpc"`

	// The request id
	ID interface{} `json:"id"`

	// The result of the request
	Result json.RawMessage `json:"result,omitempty"`

	// The error object in case a request fails
	Error *ResponseError `json:"error,omitempty"`
}

// String implements the fmt.Stringer interface for ResponseMessage.
func (r *ResponseMessage) String() string {
	var resultStr, errorStr string

	if len(r.Result) > 0 {
		var prettyJSON bytes.Buffer
		err := json.Indent(&prettyJSON, r.Result, "", "  ")
		if err != nil {
			resultStr = string(r.Result)
		} else {
			resultStr = prettyJSON.String()
		}
	} else {
		resultStr = "null"
	}

	if r.Error != nil {
		errorStr = fmt.Sprintf("%+v", r.Error)
	} else {
		errorStr = "null"
	}

	return fmt.Sprintf(
		"ResponseMessage{\n"+
			"  JSONRPC: %q,\n"+
			"  ID: %v,\n"+
			"  Result: %s,\n"+
			"  Error: %s\n"+
			"}",
		r.JSONRPC, r.ID, resultStr, errorStr,
	)
}

// ResponseError represents an error in an LSP response
type ResponseError struct {
	// A number indicating the error type
	Code ErrorCode `json:"code"`

	// A string providing a short description of the error
	Message string `json:"message"`

	// A primitive or structured value that contains additional
	// information about the error. Can be omitted.
	Data interface{} `json:"data,omitempty"`
}

// ErrorCode represents the error codes specified in the LSP
type ErrorCode int

const (
	// ParseError is used when the server receives an invalid JSON
	ParseError ErrorCode = -32700

	// InvalidRequest is used when the JSON sent is not a valid Request object
	InvalidRequest ErrorCode = -32600

	// MethodNotFound should be returned by the handler when the method is not implemented
	MethodNotFound ErrorCode = -32601

	// InvalidParams should be returned by the handler when the method's params are invalid
	InvalidParams ErrorCode = -32602

	// InternalError is used for any other error related to the method's execution
	InternalError ErrorCode = -32603

	// ServerNotInitialized is used when a request is made before the server is initialized
	ServerNotInitialized ErrorCode = -32002

	// UnknownErrorCode is used for any error not covered by the previous ones
	UnknownErrorCode ErrorCode = -32001

	// RequestCancelled is used when a request is cancelled by the client
	RequestCancelled ErrorCode = -32800

	// ContentModified is used when a document is modified before the operation could be completed
	ContentModified ErrorCode = -32801
)

// NewResponse creates a new ResponseMessage
func NewResponse(id interface{}, result interface{}, err *ResponseError) (*ResponseMessage, error) {
	var resultJSON json.RawMessage
	var marshalErr error

	if result != nil {
		resultJSON, marshalErr = json.Marshal(result)
		if marshalErr != nil {
			return nil, marshalErr
		}
	}

	return &ResponseMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultJSON,
		Error:   err,
	}, nil
}

// GetResult unmarshals the result into the provided interface
func (r *ResponseMessage) GetResult(v interface{}) error {
	return json.Unmarshal(r.Result, v)
}

// IsError returns true if the response contains an error
func (r *ResponseMessage) IsError() bool {
	return r.Error != nil
}

// NewError creates a new ResponseError
func NewError(code ErrorCode, message string, data interface{}) *ResponseError {
	return &ResponseError{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

package types

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// NotificationMessage represents a generic LSP notification message
type NotificationMessage struct {
	// The jsonrpc version. Must be "2.0"
	JSONRPC string `json:"jsonrpc"`

	// The method to be invoked
	Method string `json:"method"`

	// The notification's params
	Params json.RawMessage `json:"params,omitempty"`
}

// String implements the fmt.Stringer interface for NotificationMessage.
func (n *NotificationMessage) String() string {
	var paramsStr string
	if len(n.Params) > 0 {
		// Attempt to pretty-print the JSON
		var prettyJSON bytes.Buffer
		err := json.Indent(&prettyJSON, n.Params, "", "  ")
		if err != nil {
			// If pretty-printing fails, use the raw JSON
			paramsStr = string(n.Params)
		} else {
			paramsStr = prettyJSON.String()
		}
	} else {
		paramsStr = "null"
	}

	return fmt.Sprintf(
		"NotificationMessage{\n  JSONRPC: %q,\n  Method: %q,\n  Params: %s\n}",
		n.JSONRPC, n.Method, paramsStr,
	)
}

// NewNotification creates a new NotificationMessage
func NewNotification(method string, params interface{}) (*NotificationMessage, error) {
	var paramsJSON json.RawMessage
	var err error

	if params != nil {
		paramsJSON, err = json.Marshal(params)
		if err != nil {
			return nil, err
		}
	}

	return &NotificationMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
	}, nil
}

// GetParams unmarshals the params into the provided interface
func (n *NotificationMessage) GetParams(v interface{}) error {
	return json.Unmarshal(n.Params, v)
}

// Common LSP notification methods

// Notifications sent from client to server
const (
	DidOpenTextDocument       = "textDocument/didOpen"
	DidChangeTextDocument     = "textDocument/didChange"
	WillSaveTextDocument      = "textDocument/willSave"
	DidSaveTextDocument       = "textDocument/didSave"
	DidCloseTextDocument      = "textDocument/didClose"
	DidChangeWatchedFiles     = "workspace/didChangeWatchedFiles"
	DidChangeWorkspaceFolders = "workspace/didChangeWorkspaceFolders"
	DidChangeConfiguration    = "workspace/didChangeConfiguration"
)

// Notifications sent from server to client
const (
	ShowMessage        = "window/showMessage"
	LogMessage         = "window/logMessage"
	PublishDiagnostics = "textDocument/publishDiagnostics"
)

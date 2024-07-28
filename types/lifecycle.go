package types

import (
	"encoding/json"
)

// InitializeParams represents the parameters of an initialize request.
type InitializeParams struct {
	ProcessID             *int               `json:"processId"`
	ClientInfo            *ClientInfo        `json:"clientInfo,omitempty"`
	Locale                string             `json:"locale,omitempty"`
	RootPath              *string            `json:"rootPath,omitempty"`
	RootURI               string             `json:"rootUri"`
	InitializationOptions json.RawMessage    `json:"initializationOptions,omitempty"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	Trace                 string             `json:"trace,omitempty"`
	WorkspaceFolders      []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
}

// ClientInfo represents information about the client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// InitializeResult represents the result of an initialize request.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

// ServerInfo represents information about the server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// InitializeError represents an error during initialization.
type InitializeError struct {
	Retry bool `json:"retry"`
}

// InitializedParams represents the parameters of an initialized notification.
type InitializedParams struct{}

// ShutdownParams represents the parameters of a shutdown request.
type ShutdownParams struct{}

// ExitParams represents the parameters of an exit notification.
type ExitParams struct{}

// WorkspaceFolder represents a workspace folder in the client.
type WorkspaceFolder struct {
	// The associated URI for this workspace folder.
	URI string `json:"uri"`

	// The name of the workspace folder. Used to refer to this
	// workspace folder in the user interface.
	Name string `json:"name"`
}

// NewWorkspaceFolder creates a new WorkspaceFolder with the given URI and name.
func NewWorkspaceFolder(uri, name string) WorkspaceFolder {
	return WorkspaceFolder{
		URI:  uri,
		Name: name,
	}
}

// Equal checks if two WorkspaceFolder structs are equal.
func (wf WorkspaceFolder) Equal(other WorkspaceFolder) bool {
	return wf.URI == other.URI && wf.Name == other.Name
}

// WorkspaceFoldersChangeEvent represents the workspace folder change event.
type WorkspaceFoldersChangeEvent struct {
	// The array of added workspace folders
	Added []WorkspaceFolder `json:"added"`

	// The array of the removed workspace folders
	Removed []WorkspaceFolder `json:"removed"`
}

// NewWorkspaceFoldersChangeEvent creates a new WorkspaceFoldersChangeEvent with the given added and removed folders.
func NewWorkspaceFoldersChangeEvent(added, removed []WorkspaceFolder) WorkspaceFoldersChangeEvent {
	return WorkspaceFoldersChangeEvent{
		Added:   added,
		Removed: removed,
	}
}

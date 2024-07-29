package types

import (
	"encoding/json"
	"fmt"
)

// WorkspaceSymbolParams represents the parameters for a workspace symbol request.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
	WorkDoneProgressParams
	PartialResultParams
}

// SymbolInformation represents information about a programming construct.
type SymbolInformation struct {
	Name          string      `json:"name"`
	Kind          SymbolKind  `json:"kind"`
	Tags          []SymbolTag `json:"tags,omitempty"`
	Deprecated    bool        `json:"deprecated,omitempty"`
	Location      Location    `json:"location"`
	ContainerName string      `json:"containerName,omitempty"`
}

// WorkspaceSymbol represents a special workspace symbol that supports locations without a range.
type WorkspaceSymbol struct {
	Name          string           `json:"name"`
	Kind          SymbolKind       `json:"kind"`
	Tags          []SymbolTag      `json:"tags,omitempty"`
	ContainerName string           `json:"containerName,omitempty"`
	Location      LocationOrDocURI `json:"location"`
	Data          interface{}      `json:"data,omitempty"`
}

// LocationOrDocURI represents either a Location or a DocumentURI.
type LocationOrDocURI struct {
	Value interface{}
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (l *LocationOrDocURI) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as Location
	var location Location
	if err := json.Unmarshal(data, &location); err == nil {
		l.Value = location
		return nil
	}

	// If that fails, try to unmarshal as a string (DocumentURI)
	var uri string
	if err := json.Unmarshal(data, &uri); err == nil {
		l.Value = uri
		return nil
	}

	// If both fail, return an error
	return fmt.Errorf("failed to unmarshal LocationOrDocURI: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (l LocationOrDocURI) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.Value)
}

// IsLocation returns true if the LocationOrDocURI represents a Location.
func (l LocationOrDocURI) IsLocation() bool {
	_, ok := l.Value.(Location)
	return ok
}

// IsURI returns true if the LocationOrDocURI represents a DocumentURI.
func (l LocationOrDocURI) IsURI() bool {
	_, ok := l.Value.(string)
	return ok
}

// Location returns the Location if it represents a Location, or nil otherwise.
func (l LocationOrDocURI) Location() *Location {
	if loc, ok := l.Value.(Location); ok {
		return &loc
	}
	return nil
}

// URI returns the DocumentURI if it represents a URI, or an empty string otherwise.
func (l LocationOrDocURI) URI() string {
	if uri, ok := l.Value.(string); ok {
		return uri
	}
	return ""
}

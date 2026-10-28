package mcpclient

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spachava753/cpe/internal/plugins"
)

// Source records where a server's configuration came from.
type Source int

const (
	// SourceSettings marks a server configured in settings.json.
	SourceSettings Source = iota + 1
	// SourcePlugin marks a server defined by an installed plugin.
	SourcePlugin
	// SourceClient marks a server supplied by the ACP client for one session.
	SourceClient
)

func (s Source) String() string {
	switch s {
	case SourceSettings:
		return "settings"
	case SourcePlugin:
		return "plugin"
	case SourceClient:
		return "ACP client"
	default:
		return "unknown source"
	}
}

// ServerSpec is plain configuration for one server; building it never connects.
type ServerSpec struct {
	// ID is the configured name: the settings key, "plugin:server", or the ACP name.
	ID ServerID
	// Plugin names the defining plugin for SourcePlugin servers.
	Plugin string
	// Description is the description configured in settings.json, if any.
	Description string
	// Transport is the launch or endpoint configuration after placeholder expansion.
	Transport plugins.MCPServer
	Source    Source
}

// origin describes a spec for diagnostics, such as "plugin github-workflow".
func (s ServerSpec) origin() string {
	if s.Source == SourcePlugin {
		return "plugin " + s.Plugin
	}
	return s.Source.String()
}

// MergeSpecs combines servers from every source. Servers from different sources
// with the same ID are an error, so a client-supplied server never silently
// replaces or hides a configured one.
func MergeSpecs(sources ...[]ServerSpec) ([]ServerSpec, error) {
	seen := make(map[ServerID]ServerSpec)
	var merged []ServerSpec
	for _, specs := range sources {
		for _, spec := range specs {
			if spec.ID == "" {
				return nil, fmt.Errorf(
					"MCP server from %s has no name",
					spec.origin(),
				)
			}
			if previous, exists := seen[spec.ID]; exists {
				return nil, fmt.Errorf(
					"MCP server name %q is defined by both %s and %s; rename or disable one of them",
					spec.ID,
					previous.origin(),
					spec.origin(),
				)
			}
			seen[spec.ID] = spec
			merged = append(
				merged,
				spec,
			)
		}
	}
	slices.SortFunc(
		merged,
		func(a, b ServerSpec) int {
			return strings.Compare(
				string(a.ID),
				string(b.ID),
			)
		},
	)
	return merged, nil
}

package mcpclient

import (
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/plugins"
)

func TestSettingsSpecs(t *testing.T) {
	disabled := false
	specs, failures := SettingsSpecs(map[string]config.MCPServer{
		"stdio": {
			Command:     "server",
			Args:        []string{"--x"},
			Cwd:         "/srv",
			Description: "configured",
		},
		"http": {
			URL: "https://example.com/mcp",
			Headers: map[string]string{
				"authorization": "Bearer x",
			},
		},
		"sse": {
			Type: "sse",
			URL:  "http://localhost:9000/sse",
		},
		"off": {
			Command: "never",
			Enabled: &disabled,
		},
		"both": {
			Command: "x",
			URL:     "https://example.com",
		},
		"relative": {
			Command: "x",
			Cwd:     "relative/dir",
		},
		"neither": {},
		"badurl": {
			URL: "ftp://example.com",
		},
	})
	if len(specs) != 3 {
		t.Fatalf(
			"specs = %+v",
			specs,
		)
	}
	byID := make(map[ServerID]ServerSpec)
	for _, spec := range specs {
		byID[spec.ID] = spec
	}
	if spec := byID["stdio"]; spec.Transport.Type != plugins.Stdio || spec.Transport.Command != "server" || spec.Transport.Cwd != "/srv" || spec.Description != "configured" || spec.Source != SourceSettings {
		t.Fatalf(
			"stdio spec = %+v",
			spec,
		)
	}
	if spec := byID["http"]; spec.Transport.Type != plugins.StreamableHTTP || spec.Transport.Headers.Get("Authorization") != "Bearer x" {
		t.Fatalf(
			"http spec = %+v",
			spec,
		)
	}
	if spec := byID["sse"]; spec.Transport.Type != plugins.SSE {
		t.Fatalf(
			"sse spec = %+v",
			spec,
		)
	}
	var failed []string
	for _, failure := range failures {
		if !strings.HasPrefix(
			failure.Err.Error(),
			"invalid configuration: ",
		) {
			t.Fatalf(
				"failure %q lacks context: %v",
				failure.Spec.ID,
				failure.Err,
			)
		}
		failed = append(
			failed,
			string(failure.Spec.ID),
		)
	}
	if strings.Join(
		failed,
		",",
	) != "badurl,both,neither,relative" {
		t.Fatalf(
			"failures = %v",
			failed,
		)
	}
}

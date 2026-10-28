package mcpclient

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"

	"golang.org/x/net/http/httpguts"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/plugins"
)

// Transport types accepted in settings.json.
const (
	kindStdio = "stdio"
	kindHTTP  = "http"
	kindSSE   = "sse"
)

// SettingsSpec converts one settings.json server. An invalid configuration is
// an error for that server only; callers skip it like a server that failed to connect.
func SettingsSpec(name string, server config.MCPServer) (ServerSpec, error) {
	spec := ServerSpec{
		ID:          ServerID(name),
		Source:      SourceSettings,
		Description: server.Description,
	}
	kind := server.Type
	if kind == "" {
		switch {
		case server.Command != "" && server.URL != "":
			return spec, errors.New("set either command or url, not both")
		case server.Command != "":
			kind = kindStdio
		case server.URL != "":
			kind = kindHTTP
		default:
			return spec, errors.New("command or url is required")
		}
	}
	switch kind {
	case kindStdio:
		if server.Command == "" || server.URL != "" || len(server.Headers) != 0 {
			return spec, errors.New("stdio servers need a command and accept no url or headers")
		}
		if server.Cwd != "" && !filepath.IsAbs(server.Cwd) {
			return spec, errors.New("cwd must be an absolute path")
		}
		spec.Transport = plugins.MCPServer{
			Type:    plugins.Stdio,
			Command: server.Command,
			Args:    slices.Clone(server.Args),
			Env:     maps.Clone(server.Env),
			Cwd:     server.Cwd,
		}
	case kindHTTP, kindSSE:
		if server.URL == "" || server.Command != "" || len(server.Args) != 0 || len(server.Env) != 0 || server.Cwd != "" {
			return spec, errors.New("HTTP servers need a url and accept no command, args, env, or cwd")
		}
		parsed, err := url.Parse(server.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != httpScheme && parsed.Scheme != httpsScheme) {
			return spec, errors.New("url must be an absolute HTTP(S) URL")
		}
		headers := make(http.Header)
		for name, value := range server.Headers {
			if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) {
				return spec, fmt.Errorf(
					"invalid HTTP header %q",
					name,
				)
			}
			headers.Set(
				name,
				value,
			)
		}
		spec.Transport = plugins.MCPServer{
			Type:    plugins.StreamableHTTP,
			URL:     *parsed,
			Headers: headers,
		}
		if kind == kindSSE {
			spec.Transport.Type = plugins.SSE
		}
	default:
		return spec, fmt.Errorf(
			"unknown type %q; use stdio, http, or sse",
			kind,
		)
	}
	return spec, nil
}

// SettingsSpecs converts every enabled settings.json server. Disabled servers
// are omitted, and invalid ones are returned as failures.
func SettingsSpecs(servers map[string]config.MCPServer) ([]ServerSpec, []Failure) {
	var specs []ServerSpec
	var failures []Failure
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		server := servers[name]
		if !server.IsEnabled() {
			continue
		}
		spec, err := SettingsSpec(
			name,
			server,
		)
		if err != nil {
			failures = append(
				failures,
				Failure{
					Spec: spec,
					Err: fmt.Errorf(
						"invalid configuration: %w",
						err,
					),
				},
			)
			continue
		}
		specs = append(
			specs,
			spec,
		)
	}
	return specs, failures
}

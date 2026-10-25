package plugins

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"github.com/google/jsonschema-go/jsonschema"
	"golang.org/x/net/http/httpguts"
)

const httpScheme = "http"

func parseMCP(data []byte, pkg packageFS) (map[string]MCPServer, []error, error) {
	fields, parseErr := jsonObject(data)
	if parseErr != nil {
		return nil, nil, parseErr
	}
	validators, parseErr := mcpSchemas()
	if parseErr != nil {
		return nil, nil, parseErr
	}
	if err := validateMCPEnvelope(
		data,
		validators.envelope,
	); err != nil {
		return nil, nil, err
	}
	entries, parseErr := jsonObject(fields["mcpServers"])
	if parseErr != nil {
		return nil, nil, fmt.Errorf(
			"mcpServers: %w",
			parseErr,
		)
	}
	servers := make(map[string]MCPServer)
	var diagnostics []error
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		server, err := parseServer(
			entries[name],
			pkg,
			validators.server,
		)
		if err != nil {
			diagnostics = append(
				diagnostics,
				fmt.Errorf(
					"server %q: %w",
					name,
					err,
				),
			)
			continue
		}
		servers[name] = server
	}
	return servers, diagnostics, nil
}

func parseServer(data []byte, pkg packageFS, schema *jsonschema.Resolved) (MCPServer, error) {
	fields, parseErr := jsonObject(data)
	if parseErr != nil {
		return MCPServer{}, parseErr
	}
	var instance any
	if err := json.Unmarshal(
		data,
		&instance,
	); err != nil {
		return MCPServer{}, err
	}
	if err := schema.Validate(instance); err != nil {
		return MCPServer{}, err
	}
	// Schema validation sees decoded JSON values, so duplicate map keys need
	// to be rejected separately before encoding/json can discard them.
	for _, key := range []string{
		"env",
		"headers",
	} {
		if raw, exists := fields[key]; exists {
			if _, err := jsonObject(raw); err != nil {
				return MCPServer{}, fmt.Errorf(
					"%s: %w",
					key,
					err,
				)
			}
		}
	}
	var config struct {
		Env     map[string]string `json:"env"`
		Cwd     *string           `json:"cwd"`
		Headers map[string]string `json:"headers"`
		Type    Transport         `json:"type"`
		Command string            `json:"command"`
		URL     string            `json:"url"`
		Args    []string          `json:"args"`
	}
	if err := json.Unmarshal(
		data,
		&config,
	); err != nil {
		return MCPServer{}, err
	}
	server := MCPServer{
		Type:    config.Type,
		Command: config.Command,
		Args:    config.Args,
		Env:     config.Env,
	}
	if server.Type == Stdio {
		if err := validateCommand(
			server.Command,
			pkg,
		); err != nil {
			return MCPServer{}, err
		}
		server.Cwd = "${PLUGIN_ROOT}"
		if config.Cwd != nil {
			server.Cwd = *config.Cwd
			if err := validateCwd(
				server.Cwd,
				pkg,
			); err != nil {
				return MCPServer{}, err
			}
		}
		return server, nil
	}
	endpoint := config.URL
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil || parsed.Hostname() == "" || (parsed.Scheme != httpScheme && parsed.Scheme != "https") || parsed.User != nil || strings.Contains(
		endpoint,
		"#",
	) {
		return MCPServer{}, fmt.Errorf("url must be absolute HTTP(S), without user information or fragments")
	}
	if parsed.Scheme == httpScheme && parsed.Hostname() != "localhost" {
		ip, err := netip.ParseAddr(parsed.Hostname())
		if err != nil || !ip.Unmap().IsLoopback() {
			return MCPServer{}, fmt.Errorf("non-loopback URL must use HTTPS")
		}
	}
	server.URL = *parsed
	if config.Headers != nil {
		server.Headers = make(
			http.Header,
			len(config.Headers),
		)
		for name, value := range config.Headers {
			if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) {
				return MCPServer{}, fmt.Errorf(
					"invalid HTTP header %q",
					name,
				)
			}
			canonical := http.CanonicalHeaderKey(name)
			if _, exists := server.Headers[canonical]; exists {
				return MCPServer{}, fmt.Errorf(
					"duplicate case-insensitive header %q",
					name,
				)
			}
			server.Headers[canonical] = []string{
				value,
			}
		}
	}
	return server, nil
}

func validateCommand(command string, pkg packageFS) error {
	if command == "" || command == "." || command == ".." || strings.ContainsRune(
		command,
		'\x00',
	) {
		return fmt.Errorf("command must be one executable token")
	}
	if relative, bundled := strings.CutPrefix(
		command,
		"./",
	); bundled {
		resolved, err := pkg.resolve(relative)
		if err != nil {
			return fmt.Errorf(
				"command: %w",
				err,
			)
		}
		info, err := fs.Stat(
			pkg.root,
			resolved,
		)
		if err != nil {
			return fmt.Errorf(
				"command: %w",
				err,
			)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("command must resolve to a bundled regular file")
		}
		return nil
	}
	if strings.ContainsAny(
		command,
		"/\\:",
	) || strings.IndexFunc(
		command,
		unicode.IsSpace,
	) >= 0 {
		return fmt.Errorf("command must be a bare executable name or ./ path")
	}
	// Availability and platform executable search are launch-time concerns.
	return nil
}

func validateCwd(cwd string, pkg packageFS) error {
	var relative string
	dataRoot := false
	switch {
	case strings.HasPrefix(
		cwd,
		"./",
	):
		relative = strings.TrimPrefix(
			cwd,
			"./",
		)
	case cwd == "${PLUGIN_ROOT}" || strings.HasPrefix(
		cwd,
		"${PLUGIN_ROOT}/",
	):
		relative = strings.TrimPrefix(
			strings.TrimPrefix(
				cwd,
				"${PLUGIN_ROOT}",
			),
			"/",
		)
	case cwd == "${PLUGIN_DATA}" || strings.HasPrefix(
		cwd,
		"${PLUGIN_DATA}/",
	):
		relative = strings.TrimPrefix(
			strings.TrimPrefix(
				cwd,
				"${PLUGIN_DATA}",
			),
			"/",
		)
		dataRoot = true
	default:
		return fmt.Errorf("cwd must be rooted at ./, ${PLUGIN_ROOT}, or ${PLUGIN_DATA}")
	}
	depth := 0
	for part := range strings.SplitSeq(
		relative,
		"/",
	) {
		switch part {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return fmt.Errorf("cwd escapes its root")
			}
		default:
			depth++
		}
	}
	if strings.ContainsAny(
		relative,
		"\\\x00",
	) {
		return fmt.Errorf("invalid cwd path")
	}
	// PLUGIN_DATA and paths containing runtime placeholders cannot be
	// filesystem-resolved until launch. Their containment must be rechecked
	// after single-pass expansion, against the actual root or data directory.
	if dataRoot || strings.Contains(
		relative,
		"${PLUGIN_ROOT}",
	) || strings.Contains(
		relative,
		"${PLUGIN_DATA}",
	) {
		return nil
	}
	info, err := pkg.Stat(relative)
	if err != nil {
		return fmt.Errorf(
			"cwd: %w",
			err,
		)
	}
	if !info.IsDir() {
		return fmt.Errorf("cwd must resolve to a directory")
	}
	return nil
}

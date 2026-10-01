package plugins

import (
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/net/http/httpguts"
)

func parseMCP(data []byte, pkg packageFS) (map[string]MCPServer, []error, error) {
	fields, err := jsonObject(data)
	if err != nil {
		return nil, nil, err
	}
	for key := range fields {
		if key != "$schema" && key != "mcpServers" {
			return nil, nil, fmt.Errorf("unknown MCP configuration field %q", key)
		}
	}
	schema, err := jsonString(fields["$schema"])
	if err != nil || schema != mcpSchema {
		return nil, nil, fmt.Errorf("unsupported or invalid MCP $schema")
	}
	entries, err := jsonObject(fields["mcpServers"])
	if err != nil {
		return nil, nil, fmt.Errorf("mcpServers: %w", err)
	}
	servers := make(map[string]MCPServer)
	var diagnostics []error
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		server, err := parseServer(entries[name], pkg)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Errorf("server %q: %w", name, err))
			continue
		}
		servers[name] = server
	}
	return servers, diagnostics, nil
}

func parseServer(data []byte, pkg packageFS) (MCPServer, error) {
	fields, err := jsonObject(data)
	if err != nil {
		return MCPServer{}, err
	}
	transport, err := jsonString(fields["type"])
	if err != nil {
		return MCPServer{}, fmt.Errorf("type: %w", err)
	}
	server := MCPServer{Type: Transport(transport)}
	for key := range fields {
		allowed := key == "type"
		switch server.Type {
		case Stdio:
			allowed = allowed || key == "command" || key == "args" || key == "env" || key == "cwd"
		case StreamableHTTP, SSE:
			allowed = allowed || key == "url" || key == "headers"
		default:
			return MCPServer{}, fmt.Errorf("unsupported transport %q", transport)
		}
		if !allowed {
			return MCPServer{}, fmt.Errorf("field %q does not belong to transport %q", key, transport)
		}
	}
	if server.Type == Stdio {
		server.Command, err = jsonString(fields["command"])
		if err != nil {
			return MCPServer{}, fmt.Errorf("command: %w", err)
		}
		if err := validateCommand(server.Command, pkg); err != nil {
			return MCPServer{}, err
		}
		server.Cwd = "${PLUGIN_ROOT}"
		if raw, exists := fields["cwd"]; exists {
			server.Cwd, err = jsonString(raw)
			if err != nil {
				return MCPServer{}, fmt.Errorf("cwd: %w", err)
			}
			if err := validateCwd(server.Cwd, pkg); err != nil {
				return MCPServer{}, err
			}
		}
		if raw, exists := fields["args"]; exists {
			server.Args, err = jsonStrings(raw)
			if err != nil {
				return MCPServer{}, fmt.Errorf("args: %w", err)
			}
		}
		if raw, exists := fields["env"]; exists {
			server.Env, err = jsonStringMap(raw)
			if err != nil {
				return MCPServer{}, fmt.Errorf("env: %w", err)
			}
			for _, reserved := range []string{"PLUGIN_ROOT", "PLUGIN_DATA"} {
				if _, exists := server.Env[reserved]; exists {
					return MCPServer{}, fmt.Errorf("env must not override %s", reserved)
				}
			}
		}
		return server, nil
	}
	endpoint, err := jsonString(fields["url"])
	if err != nil {
		return MCPServer{}, fmt.Errorf("url: %w", err)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || strings.Contains(endpoint, "#") {
		return MCPServer{}, fmt.Errorf("url must be absolute HTTP(S), without user information or fragments")
	}
	if parsed.Scheme == "http" && parsed.Hostname() != "localhost" {
		ip, err := netip.ParseAddr(parsed.Hostname())
		if err != nil || !ip.Unmap().IsLoopback() {
			return MCPServer{}, fmt.Errorf("non-loopback URL must use HTTPS")
		}
	}
	server.URL = *parsed
	if raw, exists := fields["headers"]; exists {
		headers, err := jsonStringMap(raw)
		if err != nil {
			return MCPServer{}, fmt.Errorf("headers: %w", err)
		}
		server.Headers = make(http.Header, len(headers))
		for name, value := range headers {
			if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) {
				return MCPServer{}, fmt.Errorf("invalid HTTP header %q", name)
			}
			canonical := http.CanonicalHeaderKey(name)
			if _, exists := server.Headers[canonical]; exists {
				return MCPServer{}, fmt.Errorf("duplicate case-insensitive header %q", name)
			}
			server.Headers[canonical] = []string{value}
		}
	}
	return server, nil
}

func validateCommand(command string, pkg packageFS) error {
	if command == "" || command == "." || command == ".." || strings.ContainsRune(command, '\x00') {
		return fmt.Errorf("command must be one executable token")
	}
	if relative, bundled := strings.CutPrefix(command, "./"); bundled {
		resolved, err := pkg.resolve(relative)
		if err != nil {
			return fmt.Errorf("command: %w", err)
		}
		info, err := fs.Stat(pkg.root, resolved)
		if err != nil {
			return fmt.Errorf("command: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("command must resolve to a bundled regular file")
		}
		return nil
	}
	if strings.ContainsAny(command, "/\\:") || strings.IndexFunc(command, unicode.IsSpace) >= 0 {
		return fmt.Errorf("command must be a bare executable name or ./ path")
	}
	// Availability and platform executable search are launch-time concerns.
	return nil
}

func validateCwd(cwd string, pkg packageFS) error {
	var relative string
	dataRoot := false
	switch {
	case strings.HasPrefix(cwd, "./"):
		relative = strings.TrimPrefix(cwd, "./")
	case cwd == "${PLUGIN_ROOT}" || strings.HasPrefix(cwd, "${PLUGIN_ROOT}/"):
		relative = strings.TrimPrefix(strings.TrimPrefix(cwd, "${PLUGIN_ROOT}"), "/")
	case cwd == "${PLUGIN_DATA}" || strings.HasPrefix(cwd, "${PLUGIN_DATA}/"):
		relative = strings.TrimPrefix(strings.TrimPrefix(cwd, "${PLUGIN_DATA}"), "/")
		dataRoot = true
	default:
		return fmt.Errorf("cwd must be rooted at ./, ${PLUGIN_ROOT}, or ${PLUGIN_DATA}")
	}
	depth := 0
	for part := range strings.SplitSeq(relative, "/") {
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
	if strings.ContainsAny(relative, "\\\x00") {
		return fmt.Errorf("invalid cwd path")
	}
	// PLUGIN_DATA and paths containing runtime placeholders cannot be
	// filesystem-resolved until launch. Their containment must be rechecked
	// after single-pass expansion, against the actual root or data directory.
	if dataRoot || strings.Contains(relative, "${PLUGIN_ROOT}") || strings.Contains(relative, "${PLUGIN_DATA}") {
		return nil
	}
	info, err := pkg.Stat(relative)
	if err != nil {
		return fmt.Errorf("cwd: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cwd must resolve to a directory")
	}
	return nil
}

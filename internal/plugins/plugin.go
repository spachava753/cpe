package plugins

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"

	"github.com/spachava753/cpe/internal/skills"
)

// Transport identifies an MCP server's communication protocol.
type Transport string

const (
	// Stdio exchanges MCP messages through a child process's stdin and stdout.
	Stdio Transport = "stdio"
	// StreamableHTTP uses the MCP Streamable HTTP transport.
	StreamableHTTP Transport = "streamable-http"
	// SSE uses the HTTP and Server-Sent Events transport.
	SSE Transport = "sse"
)

// MCPServer holds validated launch or connection settings for an MCP server.
// Runtime placeholders remain available for expansion by the launcher.
type MCPServer struct {
	Type Transport
	// Command is the executable for Stdio servers.
	Command string
	Args    []string
	// Env supplies environment overrides for Stdio servers.
	Env map[string]string
	// Cwd is the Stdio working directory; its default is ${PLUGIN_ROOT}.
	Cwd string
	// URL is the endpoint for StreamableHTTP and SSE servers.
	URL url.URL
	// Headers contains canonicalized HTTP request headers.
	Headers http.Header
}

// Plugin contains a native Agent Plugin's manifest metadata and loaded components.
type Plugin struct {
	Name        string
	Description string
	// Path names the package directory, source-relative when loaded and using
	// native destination paths when installed.
	Path   string
	Skills []skills.Skill
	// MCPConfig maps server names to their connection or launch settings.
	MCPConfig map[string]MCPServer
	// Diagnostics records non-fatal manifest warnings and component failures.
	Diagnostics []error
}

// Load reads native Agent Plugins from immediate child directories of root.
// Missing or invalid manifests return nil plugins and an error; component failures
// are recorded in Plugin.Diagnostics. Package and skill paths are root-relative.
// Package-root symlinks are rejected; contained relative links require fs.ReadLinkFS.
func Load(root fs.ReadDirFS) ([]Plugin, error) {
	entries, err := root.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf(
			"read plugins directory: %w",
			err,
		)
	}
	var loaded []Plugin
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf(
				"plugin root %q must not be a symbolic link",
				entry.Name(),
			)
		}
		if !entry.IsDir() {
			continue
		}
		plugin, err := readPlugin(
			root,
			entry.Name(),
		)
		if err != nil {
			return nil, err
		}
		loaded = append(
			loaded,
			plugin,
		)
	}
	return loaded, nil
}

func readPlugin(root fs.FS, directory string) (Plugin, error) {
	pkg := packageFS{
		root: sourceFS{
			FS: root,
		},
		directory: directory,
	}
	info, err := pkg.Stat("plugin.json")
	if err != nil {
		return Plugin{}, fmt.Errorf(
			"%s/plugin.json: %w",
			directory,
			err,
		)
	}
	if !info.Mode().IsRegular() {
		return Plugin{}, fmt.Errorf(
			"%s/plugin.json must resolve to a regular file",
			directory,
		)
	}
	data, err := fs.ReadFile(
		pkg,
		"plugin.json",
	)
	if err != nil {
		return Plugin{}, fmt.Errorf(
			"%s/plugin.json: %w",
			directory,
			err,
		)
	}
	manifest, diagnostics, err := parseManifest(data)
	if err != nil {
		return Plugin{}, fmt.Errorf(
			"%s/plugin.json: %w",
			directory,
			err,
		)
	}
	plugin := Plugin{
		Name:        manifest.name,
		Description: manifest.description,
		Path:        directory,
	}
	for _, diagnostic := range diagnostics {
		plugin.Diagnostics = append(
			plugin.Diagnostics,
			fmt.Errorf(
				"%s/plugin.json: %w",
				directory,
				diagnostic,
			),
		)
	}
	locations, err := pkg.ReadDir(".")
	if err != nil {
		plugin.Diagnostics = append(
			plugin.Diagnostics,
			fmt.Errorf(
				"%s: %w",
				directory,
				err,
			),
		)
		return plugin, nil
	}
	for _, location := range locations {
		switch location.Name() {
		case "skills":
			loadPluginSkills(
				pkg,
				&plugin,
			)
		case "mcp.json":
			loadPluginMCP(
				pkg,
				&plugin,
			)
		}
	}
	return plugin, nil
}

func loadPluginSkills(pkg packageFS, plugin *Plugin) {
	entries, err := pkg.ReadDir("skills")
	if err != nil {
		plugin.Diagnostics = append(
			plugin.Diagnostics,
			fmt.Errorf(
				"%s/skills: %w",
				plugin.Path,
				err,
			),
		)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
			continue
		}
		filename := path.Join(
			"skills",
			entry.Name(),
			"SKILL.md",
		)
		info, err := pkg.Stat(filename)
		if err != nil {
			if !errors.Is(
				err,
				fs.ErrNotExist,
			) {
				plugin.Diagnostics = append(
					plugin.Diagnostics,
					fmt.Errorf(
						"%s/%s: %w",
						plugin.Path,
						filename,
						err,
					),
				)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		loaded, err := skills.Load(singleSkillFS{
			pkg:   pkg,
			entry: entry,
		})
		if err != nil {
			plugin.Diagnostics = append(
				plugin.Diagnostics,
				fmt.Errorf(
					"%s/skills: %w",
					plugin.Path,
					err,
				),
			)
			continue
		}
		for _, skill := range loaded {
			skill.Path = path.Join(
				plugin.Path,
				"skills",
				skill.Path,
			)
			plugin.Skills = append(
				plugin.Skills,
				skill,
			)
		}
	}
}

func loadPluginMCP(pkg packageFS, plugin *Plugin) {
	info, err := pkg.Stat("mcp.json")
	if err != nil {
		plugin.Diagnostics = append(
			plugin.Diagnostics,
			fmt.Errorf(
				"%s/mcp.json: %w",
				plugin.Path,
				err,
			),
		)
		return
	}
	if !info.Mode().IsRegular() {
		plugin.Diagnostics = append(
			plugin.Diagnostics,
			fmt.Errorf(
				"%s/mcp.json must resolve to a regular file",
				plugin.Path,
			),
		)
		return
	}
	data, err := fs.ReadFile(
		pkg,
		"mcp.json",
	)
	if err != nil {
		plugin.Diagnostics = append(
			plugin.Diagnostics,
			fmt.Errorf(
				"%s/mcp.json: %w",
				plugin.Path,
				err,
			),
		)
		return
	}
	servers, diagnostics, err := parseMCP(
		data,
		pkg,
	)
	if err != nil {
		plugin.Diagnostics = append(
			plugin.Diagnostics,
			fmt.Errorf(
				"%s/mcp.json: %w",
				plugin.Path,
				err,
			),
		)
		return
	}
	plugin.MCPConfig = servers
	for _, diagnostic := range diagnostics {
		plugin.Diagnostics = append(
			plugin.Diagnostics,
			fmt.Errorf(
				"%s/mcp.json: %w",
				plugin.Path,
				diagnostic,
			),
		)
	}
}

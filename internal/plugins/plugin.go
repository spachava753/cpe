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

// Transport identifies a supported MCP configuration variant.
type Transport string

const (
	Stdio          Transport = "stdio"
	StreamableHTTP Transport = "streamable-http"
	SSE            Transport = "sse"
)

// MCPServer is validated configuration, not a running connection. Args, Env,
// and Cwd retain runtime placeholders; omitted Cwd defaults to ${PLUGIN_ROOT}.
type MCPServer struct {
	Type    Transport
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string
	URL     url.URL
	Headers http.Header
}

// Plugin represents an Agent Plugin as specified by https://agent-plugins.org/specification
type Plugin struct {
	Name        string
	Path        string
	Skills      []skills.Skill
	MCPConfig   map[string]MCPServer
	Diagnostics []error
}

// Load reads plugins from immediate child directories of root. A missing or
// invalid manifest stops loading and returns no partial results. Component
// failures are isolated and reported through Plugin.Diagnostics, as required
// by the Agent Plugins 1.0.0 specification. Paths are relative to root.
// Load never launches executables, connects to servers, or expands placeholders.
// Plugin root symlinks are rejected; relative links inside a package are allowed
// only when their containment can be established through fs.ReadLinkFS.
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
		pkg := packageFS{
			root:      root,
			directory: entry.Name(),
		}
		info, err := pkg.Stat("plugin.json")
		if err != nil {
			return nil, fmt.Errorf(
				"%s/plugin.json: %w",
				entry.Name(),
				err,
			)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf(
				"%s/plugin.json must resolve to a regular file",
				entry.Name(),
			)
		}
		data, err := fs.ReadFile(
			pkg,
			"plugin.json",
		)
		if err != nil {
			return nil, fmt.Errorf(
				"%s/plugin.json: %w",
				entry.Name(),
				err,
			)
		}
		name, diagnostics, err := parseManifest(data)
		if err != nil {
			return nil, fmt.Errorf(
				"%s/plugin.json: %w",
				entry.Name(),
				err,
			)
		}
		plugin := Plugin{
			Name: name,
			Path: entry.Name(),
		}
		for _, diagnostic := range diagnostics {
			plugin.Diagnostics = append(
				plugin.Diagnostics,
				fmt.Errorf(
					"%s/plugin.json: %w",
					entry.Name(),
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
					entry.Name(),
					err,
				),
			)
			loaded = append(
				loaded,
				plugin,
			)
			continue
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
		loaded = append(
			loaded,
			plugin,
		)
	}
	return loaded, nil
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

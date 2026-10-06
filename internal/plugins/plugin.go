package plugins

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

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
	Description string
	// Path identifies the package directory, not its manifest file.
	Path        string
	Skills      []skills.Skill
	MCPConfig   map[string]MCPServer
	Diagnostics []error
}

func GlobalPath() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(
		h,
		".agents",
		"plugins",
	), nil
}

func ProjectPath(cwd string) string {
	return filepath.Join(
		cwd,
		".agents",
		"plugins",
	)
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

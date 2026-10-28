package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/skills"
)

type commandCatalog struct {
	commands map[string]skills.Skill
	// pluginSkills marks commands that belong to a plugin rather than standing alone.
	pluginSkills map[string]bool
	plugins      []plugins.Plugin
	warnings     []error
}

func loadCommands(ctx context.Context, filesystem afero.Fs, cwd string, config Config) (commandCatalog, error) {
	catalog := commandCatalog{
		commands:     make(map[string]skills.Skill),
		pluginSkills: make(map[string]bool),
	}
	roots := append(
		slices.Clone(config.SkillPaths),
		skills.ProjectPath(cwd),
	)
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return catalog, err
		}
		if _, err := filesystem.Stat(root); err != nil {
			if errors.Is(
				err,
				fs.ErrNotExist,
			) {
				continue
			}
			return catalog, err
		}
		loaded, err := skills.Load(afero.IOFS{
			Fs: afero.NewBasePathFs(
				filesystem,
				root,
			),
		})
		if err != nil {
			return catalog, err
		}
		for _, skill := range loaded {
			skill.Path = filepath.Join(
				root,
				filepath.FromSlash(skill.Path),
			)
			// Project roots come last and override same-named global skills.
			catalog.commands[skill.Name] = skill
		}
	}
	roots = append(
		slices.Clone(config.PluginPaths),
		filepath.Join(
			cwd,
			".agents",
			"plugins",
		),
	)
	seen := make(map[string]bool)
	for _, root := range roots {
		if _, err := filesystem.Stat(root); err != nil {
			if errors.Is(
				err,
				fs.ErrNotExist,
			) {
				continue
			}
			return catalog, err
		}
		loaded, err := plugins.Load(afero.IOFS{
			Fs: afero.NewBasePathFs(
				filesystem,
				root,
			),
		})
		if err != nil {
			return catalog, err
		}
		for _, plugin := range loaded {
			if seen[plugin.Name] {
				return catalog, fmt.Errorf(
					"duplicate plugin %q",
					plugin.Name,
				)
			}
			seen[plugin.Name] = true
			catalog.warnings = append(
				catalog.warnings,
				plugin.Diagnostics...,
			)
			plugin.Path = filepath.Join(
				root,
				filepath.FromSlash(plugin.Path),
			)
			for _, skill := range plugin.Skills {
				skill.Path = filepath.Join(
					root,
					filepath.FromSlash(skill.Path),
				)
				name := plugin.Name + ":" + skill.Name
				if _, exists := catalog.commands[name]; exists {
					return catalog, fmt.Errorf(
						"duplicate command %q",
						name,
					)
				}
				catalog.commands[name] = skill
				catalog.pluginSkills[name] = true
			}
			catalog.plugins = append(
				catalog.plugins,
				plugin,
			)
		}
	}
	return catalog, nil
}

func (c commandCatalog) available() []acp.AvailableCommand {
	commands := make(
		[]acp.AvailableCommand,
		0,
		len(c.commands)+1,
	)
	commands = append(
		commands,
		acp.AvailableCommand{
			Name:        compactName,
			Description: "Ask the model to compact the conversation.",
		},
	)
	for name, skill := range c.commands {
		if name == compactName {
			continue
		}
		commands = append(
			commands,
			acp.AvailableCommand{
				Name:        name,
				Description: skill.Description,
			},
		)
	}
	sort.Slice(
		commands,
		func(i, j int) bool { return commands[i].Name < commands[j].Name },
	)
	return commands
}

func (c commandCatalog) expand(message gai.Message) gai.Message {
	if len(message.Blocks) == 0 || message.Blocks[0].ModalityType != gai.Text {
		return message
	}
	text := message.Blocks[0].Content.String()
	if !strings.HasPrefix(
		text,
		"/",
	) {
		return message
	}
	name := strings.TrimPrefix(
		text,
		"/",
	)
	arguments := ""
	if index := strings.IndexFunc(
		name,
		unicode.IsSpace,
	); index >= 0 {
		arguments = strings.TrimSpace(name[index:])
		name = name[:index]
	}
	if name == compactName {
		text := "Compact the conversation now by calling compact(summary) in starlark_exec."
		if arguments != "" {
			text += "\n\nUser request:\n" + arguments
		}
		message.Blocks[0] = gai.TextBlock(text)
		return message
	}
	skill, ok := c.commands[name]
	if !ok {
		return message
	}
	message.Blocks[0] = gai.TextBlock(fmt.Sprintf(
		"Use the %q skill. Read its instructions at %q with bash or read_file; resolve bundled resources relative to that file.\n\nUser request:\n%s",
		name,
		skill.Path,
		arguments,
	))
	return message
}

// promptSkills lists the invocable skills for the system prompt, sorted by name.
func (c commandCatalog) promptSkills(include func(string) bool) []promptSkill {
	var result []promptSkill
	for _, name := range slices.Sorted(maps.Keys(c.commands)) {
		skill := c.commands[name]
		if name == compactName || skill.InvocationDisabled || !include(name) {
			continue
		}
		result = append(
			result,
			promptSkill{
				Name:        name,
				Description: skill.Description,
				Path:        skill.Path,
			},
		)
	}
	return result
}

// pluginSpecs expands each plugin's MCP configuration into server specs. A
// server that cannot be expanded is returned as a failure, to be skipped.
func (c commandCatalog) pluginSpecs() ([]mcpclient.ServerSpec, []mcpclient.Failure, error) {
	var specs []mcpclient.ServerSpec
	var failures []mcpclient.Failure
	for _, plugin := range c.plugins {
		for _, name := range slices.Sorted(maps.Keys(plugin.MCPConfig)) {
			server := plugin.MCPConfig[name]
			id := mcpclient.ServerID(plugin.Name + ":" + name)
			// Expand the supported placeholder once. Never perform shell expansion.
			expand := func(value string) string {
				return strings.ReplaceAll(
					value,
					"${PLUGIN_ROOT}",
					plugin.Path,
				)
			}
			server.Command = expand(server.Command)
			if strings.HasPrefix(
				server.Command,
				"./",
			) {
				server.Command = filepath.Join(
					plugin.Path,
					server.Command,
				)
			}
			if server.Type == plugins.Stdio {
				server.Cwd = expand(server.Cwd)
				if server.Cwd == "" {
					server.Cwd = plugin.Path
				}
				if !filepath.IsAbs(server.Cwd) {
					server.Cwd = filepath.Join(
						plugin.Path,
						server.Cwd,
					)
				}
			}
			server.Args = slices.Clone(server.Args)
			for i, argument := range server.Args {
				server.Args[i] = expand(argument)
			}
			env := make(map[string]string)
			for key, value := range server.Env {
				env[key] = expand(value)
			}
			server.Env = env
			spec := mcpclient.ServerSpec{
				ID:        id,
				Source:    mcpclient.SourcePlugin,
				Plugin:    plugin.Name,
				Transport: server,
			}
			// PLUGIN_DATA needs an explicit writable data-directory policy.
			encoded, err := json.Marshal(server)
			if err != nil {
				return nil, nil, err
			}
			if strings.Contains(
				string(encoded),
				"${",
			) {
				failures = append(
					failures,
					mcpclient.Failure{
						Spec: spec,
						Err:  errors.New("invalid configuration: unsupported placeholder; only ${PLUGIN_ROOT} is supported"),
					},
				)
				continue
			}
			specs = append(
				specs,
				spec,
			)
		}
	}
	return specs, failures, nil
}

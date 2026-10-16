package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/skills"
)

type commandCatalog struct {
	commands map[string]skills.Skill
	plugins  []plugins.Plugin
}

func loadCommands(ctx context.Context, filesystem afero.Fs, cwd string, config Config) (commandCatalog, error) {
	catalog := commandCatalog{
		commands: make(map[string]skills.Skill),
	}
	roots := append(
		append(
			[]string(nil),
			config.SkillPaths...,
		),
		skills.ProjectPath(cwd),
	)
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return catalog, err
		}
		loaded, err := skills.Load(afero.IOFS{
			Fs: afero.NewBasePathFs(
				filesystem,
				root,
			),
		})
		if errors.Is(
			err,
			fs.ErrNotExist,
		) {
			continue
		}
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
		append(
			[]string(nil),
			config.PluginPaths...,
		),
		filepath.Join(
			cwd,
			".agents",
			"plugins",
		),
	)
	seen := make(map[string]bool)
	for _, root := range roots {
		loaded, err := plugins.Load(afero.IOFS{
			Fs: afero.NewBasePathFs(
				filesystem,
				root,
			),
		})
		if errors.Is(
			err,
			fs.ErrNotExist,
		) {
			continue
		}
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
			if len(plugin.Diagnostics) != 0 {
				return catalog, errors.Join(plugin.Diagnostics...)
			}
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
		len(c.commands),
	)
	for name, skill := range c.commands {
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

func (c commandCatalog) expand(filesystem afero.Fs, message gai.Message) (gai.Message, error) {
	if len(message.Blocks) == 0 || message.Blocks[0].ModalityType != gai.Text {
		return message, nil
	}
	text := message.Blocks[0].Content.String()
	if !strings.HasPrefix(
		text,
		"/",
	) {
		return message, nil
	}
	name, arguments, _ := strings.Cut(
		strings.TrimPrefix(
			text,
			"/",
		),
		" ",
	)
	skill, ok := c.commands[name]
	if !ok {
		return gai.Message{}, fmt.Errorf(
			"unknown slash command %q",
			name,
		)
	}
	data, err := afero.ReadFile(
		filesystem,
		skill.Path,
	)
	if err != nil {
		return gai.Message{}, err
	}
	if len(data) > maxTextBytes {
		return gai.Message{}, errors.New("skill exceeds instruction size limit")
	}
	message.Blocks[0] = gai.TextBlock(fmt.Sprintf(
		"Skill %s from %s:\n%s\n\nUser arguments:\n%s",
		name,
		skill.Path,
		data,
		arguments,
	))
	return message, nil
}

func (c commandCatalog) servers(client map[MCPServerID]mcpServer, cwd string) (map[MCPServerID]mcpServer, error) {
	servers := make(map[MCPServerID]mcpServer)
	maps.Copy(
		servers,
		client,
	)
	for _, plugin := range c.plugins {
		for name, server := range plugin.MCPConfig {
			id := MCPServerID(plugin.Name + ":" + name)
			if _, exists := servers[id]; exists {
				return nil, fmt.Errorf(
					"duplicate MCP server %q",
					id,
				)
			}
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
			server.Args = append(
				[]string(nil),
				server.Args...,
			)
			for i, argument := range server.Args {
				server.Args[i] = expand(argument)
			}
			env := make(map[string]string)
			for key, value := range server.Env {
				env[key] = expand(value)
			}
			server.Env = env
			// PLUGIN_DATA needs an explicit writable data-directory policy.
			encoded, err := json.Marshal(server)
			if err != nil {
				return nil, err
			}
			if strings.Contains(
				string(encoded),
				"${",
			) {
				return nil, fmt.Errorf(
					"MCP server %q uses an unsupported placeholder",
					id,
				)
			}
			servers[id] = server
		}
	}
	for id, server := range servers {
		if server.Cwd == "" {
			server.Cwd = cwd
			servers[id] = server
		}
	}
	return servers, nil
}

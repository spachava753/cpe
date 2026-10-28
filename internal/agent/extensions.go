package agent

import (
	_ "embed"
	"slices"
	"strings"
	"text/template"

	"github.com/spachava753/cpe/internal/mcpclient"
)

//go:embed extensions_instructions.go.tmpl
var extensionsInstructionsText string

var extensionsInstructionsTemplate = template.Must(template.New("extension instructions").Parse(extensionsInstructionsText))

type promptSkill struct {
	Name        string
	Description string
	Path        string
}

type promptTool struct {
	Name    string
	Summary string
}

type promptServer struct {
	Prefix      string
	Description string
	Tools       []promptTool
}

type promptPlugin struct {
	Name        string
	Description string
	Skills      []promptSkill
	Servers     []promptServer
}

type extensionsInstructions struct {
	Skills  []promptSkill
	Plugins []promptPlugin
	Servers []promptServer
}

func promptServerOf(server *mcpclient.CatalogServer) promptServer {
	entry := promptServer{
		Prefix:      server.Prefix,
		Description: server.Description(),
	}
	for _, tool := range server.Tools {
		entry.Tools = append(
			entry.Tools,
			promptTool{
				Name:    tool.Name,
				Summary: mcpclient.Summary(tool.Tool.Description),
			},
		)
	}
	return entry
}

// extensionInstructions renders skills, plugins, and MCP servers for the system
// prompt. It is built once at session setup so the prompt stays cacheable.
func (c commandCatalog) extensionInstructions(catalog *mcpclient.Catalog) (string, error) {
	data := extensionsInstructions{
		Skills: c.promptSkills(func(name string) bool { return !c.pluginSkills[name] }),
	}
	byPlugin := make(map[string][]promptServer)
	for _, server := range catalog.Servers() {
		if server.Spec.Source == mcpclient.SourcePlugin {
			byPlugin[server.Spec.Plugin] = append(
				byPlugin[server.Spec.Plugin],
				promptServerOf(server),
			)
			continue
		}
		data.Servers = append(
			data.Servers,
			promptServerOf(server),
		)
	}
	for _, plugin := range c.plugins {
		prefix := plugin.Name + ":"
		data.Plugins = append(
			data.Plugins,
			promptPlugin{
				Name:        plugin.Name,
				Description: plugin.Description,
				Skills: c.promptSkills(func(name string) bool {
					return c.pluginSkills[name] && strings.HasPrefix(
						name,
						prefix,
					)
				}),
				Servers: byPlugin[plugin.Name],
			},
		)
	}
	slices.SortFunc(
		data.Plugins,
		func(a, b promptPlugin) int {
			return strings.Compare(
				a.Name,
				b.Name,
			)
		},
	)
	var text strings.Builder
	if err := extensionsInstructionsTemplate.Execute(
		&text,
		data,
	); err != nil {
		return "", err
	}
	return text.String(), nil
}

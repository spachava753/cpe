package codemode

import (
	"bytes"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"text/template"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stoewer/go-strcase"

	mcpcpe "github.com/spachava753/cpe/internal/mcp"
)

//go:embed maingen.go.tmpl
var mainTemplateSource string

// ServerToolsInfo groups tools with their server configuration for main.go generation
type ServerToolsInfo struct {
	ServerName string
	Config     mcpcpe.ServerConfig
	Tools      []*mcp.Tool
}

// GenerateMainGo generates the complete main.go file contents for code mode execution.
// It includes MCP client setup, type definitions, function declarations, and the main entry point.
// contentOutputPath specifies where content.json will be written for multimedia output.
func GenerateMainGo(servers []ServerToolsInfo, contentOutputPath string) (string, error) {
	// Collect all tools for type/function generation
	var allTools []*mcp.Tool
	for _, server := range servers {
		allTools = append(allTools, server.Tools...)
	}

	// Generate type definitions and function declarations
	toolDefs, err := GenerateToolDefinitions(allTools)
	if err != nil {
		return "", fmt.Errorf("generating tool definitions: %w", err)
	}

	// Build template data
	data := buildTemplateData(servers, toolDefs, contentOutputPath)

	// Parse and execute template
	tmpl, err := template.New("main.go").Funcs(template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}).Parse(mainTemplateSource)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}

// templateData holds all data needed by the main.go template
type templateData struct {
	HasHeaders        bool
	HasStdio          bool
	HasHTTP           bool
	HasSSE            bool
	HasTools          bool
	HasServers        bool
	ToolDefinitions   string
	Servers           []serverTemplateData
	ToolInits         []toolInitData
	ContentOutputPath string
}

// serverTemplateData holds per-server template data
type serverTemplateData struct {
	VarName    string
	ServerName string
	Type       string
	Command    string
	Args       []string
	Env        map[string]string
	URL        string
	Headers    map[string]string
	HasHeaders bool
	HasEnv     bool
}

// toolInitData holds per-tool initialization data
type toolInitData struct {
	GoName     string
	ToolName   string
	SessionVar string
	HasInput   bool
	InputType  string
	OutputType string
}

func buildTemplateData(servers []ServerToolsInfo, toolDefs string, contentOutputPath string) templateData {
	// Sort servers by name for deterministic output
	sortedServers := make([]ServerToolsInfo, len(servers))
	copy(sortedServers, servers)
	slices.SortFunc(sortedServers, func(a, b ServerToolsInfo) int {
		return strings.Compare(a.ServerName, b.ServerName)
	})

	data := templateData{
		ToolDefinitions:   toolDefs,
		HasTools:          toolDefs != "",
		HasServers:        len(servers) > 0,
		ContentOutputPath: contentOutputPath,
	}

	// Build server data and check for features
	for _, server := range sortedServers {
		serverType := server.Config.Type
		if serverType == "" {
			serverType = "stdio"
		}

		// Convert server name to valid Go variable name using LowerCamelCase
		varName := strcase.LowerCamelCase(server.ServerName) + "Session"

		sData := serverTemplateData{
			VarName:    varName,
			ServerName: server.ServerName,
			Type:       serverType,
			Command:    server.Config.Command,
			Args:       server.Config.Args,
			Env:        server.Config.Env,
			URL:        server.Config.URL,
			Headers:    server.Config.Headers,
			HasHeaders: len(server.Config.Headers) > 0,
			HasEnv:     len(server.Config.Env) > 0,
		}

		data.Servers = append(data.Servers, sData)

		// Track feature usage for conditional imports
		switch serverType {
		case "stdio":
			data.HasStdio = true
		case "http":
			data.HasHTTP = true
		case "sse":
			data.HasSSE = true
		}
		if sData.HasHeaders {
			data.HasHeaders = true
		}

		// Build tool initialization data for this server's tools
		for _, tool := range server.Tools {
			goName := strcase.UpperCamelCase(tool.Name)
			hasInput := hasInputSchema(tool)

			init := toolInitData{
				GoName:     goName,
				ToolName:   tool.Name,
				SessionVar: varName,
				HasInput:   hasInput,
				InputType:  goName + "Input",
				OutputType: goName + "Output",
			}
			data.ToolInits = append(data.ToolInits, init)
		}
	}

	// Sort tool inits by name for deterministic output
	slices.SortFunc(data.ToolInits, func(a, b toolInitData) int {
		return strings.Compare(a.GoName, b.GoName)
	})

	return data
}

// hasInputSchema checks if a tool has a non-empty input schema
func hasInputSchema(tool *mcp.Tool) bool {
	if tool.InputSchema == nil {
		return false
	}
	// Check if it's an empty map
	if m, ok := tool.InputSchema.(map[string]any); ok {
		if props, exists := m["properties"]; exists {
			if propsMap, ok := props.(map[string]any); ok {
				return len(propsMap) > 0
			}
		}
		return false
	}
	return true
}

package commands

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	gonanoid "github.com/matoous/go-nanoid/v2"
	_ "github.com/mattn/go-sqlite3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	mcpinternal "github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/subagentlog"
	"github.com/spachava753/cpe/internal/types"
)

const serverTypeStdio = "stdio"

// MCPListServersOptions contains parameters for listing MCP servers
type MCPListServersOptions struct {
	MCPServers map[string]mcpinternal.ServerConfig
	Writer     io.Writer
}

// MCPListServers lists all configured MCP servers
func MCPListServers(ctx context.Context, opts MCPListServersOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		fmt.Fprintln(opts.Writer, "No MCP servers configured.")
		return nil
	}

	fmt.Fprintln(opts.Writer, "Configured MCP Servers:")
	for name, server := range mcpConfig {
		serverType := server.Type
		if serverType == "" {
			serverType = serverTypeStdio
		}

		timeout := server.Timeout
		if timeout == 0 {
			timeout = 60
		}

		fmt.Fprintf(opts.Writer, "- %s (Type: %s, Timeout: %ds)\n", name, serverType, timeout)

		if serverType == serverTypeStdio && server.Command != "" {
			fmt.Fprintf(opts.Writer, "  Command: %s %s\n", server.Command, strings.Join(server.Args, " "))
		}

		if server.URL != "" {
			fmt.Fprintf(opts.Writer, "  URL: %s\n", server.URL)
		}

		if serverType == serverTypeStdio && len(server.Env) > 0 {
			fmt.Fprintln(opts.Writer, "  Environment Variables:")
			for k, v := range server.Env {
				fmt.Fprintf(opts.Writer, "    %s=%s\n", k, v)
			}
		}
	}

	return nil
}

// MCPInfoOptions contains parameters for getting MCP server info
type MCPInfoOptions struct {
	MCPServers map[string]mcpinternal.ServerConfig
	ServerName string
	Writer     io.Writer
	Timeout    time.Duration
}

// MCPInfo connects to an MCP server and displays its information
func MCPInfo(ctx context.Context, opts MCPInfoOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	client := mcpinternal.NewClient()

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	transport, err := mcpinternal.CreateTransport(ctx, serverConfig, "")
	if err != nil {
		return err
	}

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}

	fmt.Fprintf(opts.Writer, "Connected to server: %s\n", opts.ServerName)

	return cs.Close()
}

// MCPListToolsOptions contains parameters for listing MCP server tools
type MCPListToolsOptions struct {
	MCPServers   map[string]mcpinternal.ServerConfig
	ServerName   string
	Writer       io.Writer
	ShowAll      bool
	ShowFiltered bool
	Renderer     types.Renderer
}

// MCPListTools lists tools available on an MCP server
func MCPListTools(ctx context.Context, opts MCPListToolsOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	client := mcpinternal.NewClient()

	var allTools []*mcp.Tool
	transport, err := mcpinternal.CreateTransport(ctx, serverConfig, "")
	if err != nil {
		return err
	}

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}

	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return err
		}
		allTools = append(allTools, tool)
	}

	if err := cs.Close(); err != nil {
		return err
	}

	filteredTools, filteredOut := mcpinternal.FilterMcpTools(allTools, serverConfig)

	var toolsToShow []*mcp.Tool
	var title string

	if opts.ShowAll {
		toolsToShow = allTools
		title = fmt.Sprintf("All tools on server '%s' (including filtered)", opts.ServerName)
	} else if opts.ShowFiltered {
		for _, toolName := range filteredOut {
			for _, tool := range allTools {
				if tool.Name == toolName {
					toolsToShow = append(toolsToShow, tool)
					break
				}
			}
		}
		title = fmt.Sprintf("Filtered-out tools on server '%s'", opts.ServerName)
	} else {
		toolsToShow = filteredTools
		title = fmt.Sprintf("Available tools on server '%s'", opts.ServerName)
	}

	var markdownBuilder strings.Builder

	markdownBuilder.WriteString(fmt.Sprintf("# %s\n\n", title))

	// Infer filter mode from which list is populated
	var filterMode string
	switch {
	case len(serverConfig.EnabledTools) > 0:
		filterMode = "whitelist"
	case len(serverConfig.DisabledTools) > 0:
		filterMode = "blacklist"
	default:
		filterMode = "all"
	}

	markdownBuilder.WriteString("**Filter mode:** `" + filterMode + "`")

	if len(serverConfig.EnabledTools) > 0 {
		markdownBuilder.WriteString(" | **Enabled tools:** `" + strings.Join(serverConfig.EnabledTools, "`, `") + "`")
	}
	if len(serverConfig.DisabledTools) > 0 {
		markdownBuilder.WriteString(" | **Disabled tools:** `" + strings.Join(serverConfig.DisabledTools, "`, `") + "`")
	}

	markdownBuilder.WriteString("\n**Total tools:** " + strconv.Itoa(len(allTools)) +
		" | **Available:** " + strconv.Itoa(len(filteredTools)) +
		" | **Filtered out:** " + strconv.Itoa(len(filteredOut)) + "\n\n")

	if len(toolsToShow) == 0 {
		markdownBuilder.WriteString("*No tools to display.*\n")
	} else {
		for _, tool := range toolsToShow {
			filteredBadge := ""
			if opts.ShowAll {
				if slices.Contains(filteredOut, tool.Name) {
					filteredBadge = " 🚫 *filtered*"
				}
			}

			markdownBuilder.WriteString(fmt.Sprintf("### `%s`%s\n", tool.Name, filteredBadge))
			markdownBuilder.WriteString(tool.Description + "\n\n")

			if tool.InputSchema != nil {
				markdownBuilder.WriteString("**Input Schema:**\n\n")

				var schemaJSON bytes.Buffer
				encoder := json.NewEncoder(&schemaJSON)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(tool.InputSchema); err != nil {
					markdownBuilder.WriteString("```json\n" + "Error encoding schema: " + err.Error() + "\n```\n\n")
				} else {
					markdownBuilder.WriteString("```json\n" + schemaJSON.String() + "\n```\n\n")
				}
			}

			if tool.OutputSchema != nil {
				markdownBuilder.WriteString("**Output Schema:**\n\n")

				var schemaJSON bytes.Buffer
				encoder := json.NewEncoder(&schemaJSON)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(tool.OutputSchema); err != nil {
					markdownBuilder.WriteString("```json\n" + "Error encoding schema: " + err.Error() + "\n```\n\n")
				} else {
					markdownBuilder.WriteString("```json\n" + schemaJSON.String() + "\n```\n\n")
				}
			}
		}
	}

	rendered, err := opts.Renderer.Render(markdownBuilder.String())
	if err != nil {
		return fmt.Errorf("failed to render markdown: %w", err)
	}

	fmt.Fprint(opts.Writer, rendered)
	return nil
}

// MCPCallToolOptions contains parameters for calling an MCP tool
type MCPCallToolOptions struct {
	MCPServers map[string]mcpinternal.ServerConfig
	ServerName string
	ToolName   string
	ToolArgs   map[string]any
	Writer     io.Writer
}

// MCPCallTool calls a specific tool on an MCP server
func MCPCallTool(ctx context.Context, opts MCPCallToolOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	client := mcpinternal.NewClient()

	transport, err := mcpinternal.CreateTransport(ctx, serverConfig, "")
	if err != nil {
		return err
	}

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}
	defer cs.Close()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      opts.ToolName,
		Arguments: opts.ToolArgs,
	})
	if err != nil {
		return err
	}

	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			fmt.Fprint(opts.Writer, textContent.Text)
		}
	}

	return nil
}

// MCPCodeDescOptions contains parameters for generating code mode description
type MCPCodeDescOptions struct {
	MCPServers map[string]mcpinternal.ServerConfig
	CodeMode   *config.CodeModeConfig
	Writer     io.Writer
	Renderer   types.Renderer
}

// MCPCodeDesc generates and prints the execute_go_code tool description
func MCPCodeDesc(ctx context.Context, opts MCPCodeDescOptions) error {
	// Use InitializeConnections instead of FetchTools
	mcpState, err := mcpinternal.InitializeConnections(ctx, opts.MCPServers, "")
	if err != nil {
		return fmt.Errorf("failed to initialize MCP connections: %w", err)
	}
	defer mcpState.Close()

	// Get excluded tool names from code mode config
	var excludedToolNames []string
	if opts.CodeMode != nil && opts.CodeMode.ExcludedTools != nil {
		excludedToolNames = opts.CodeMode.ExcludedTools
	}

	// Partition tools - only code mode tools will be included in the description
	codeModeServers, _ := codemode.PartitionTools(mcpState, excludedToolNames)

	// Collect all code mode tools
	var allCodeModeTools []*mcp.Tool
	for _, serverInfo := range codeModeServers {
		allCodeModeTools = append(allCodeModeTools, serverInfo.Tools...)
	}

	// Generate the description
	description, err := codemode.GenerateExecuteGoCodeDescription(allCodeModeTools)
	if err != nil {
		return fmt.Errorf("failed to generate description: %w", err)
	}

	// Format as markdown for rendering
	var markdownBuilder strings.Builder
	markdownBuilder.WriteString("# execute_go_code Tool Description\n\n")

	if opts.CodeMode == nil || !opts.CodeMode.Enabled {
		markdownBuilder.WriteString("> **Note:** Code mode is not enabled in current configuration.\n\n")
	}

	if len(excludedToolNames) > 0 {
		markdownBuilder.WriteString("**Excluded tools:** `" + strings.Join(excludedToolNames, "`, `") + "`\n\n")
	}

	markdownBuilder.WriteString("**Code mode tools:** " + strconv.Itoa(len(allCodeModeTools)) + "\n\n")
	markdownBuilder.WriteString("---\n\n")
	markdownBuilder.WriteString(description)

	rendered, err := opts.Renderer.Render(markdownBuilder.String())
	if err != nil {
		return fmt.Errorf("failed to render markdown: %w", err)
	}

	fmt.Fprintln(opts.Writer, rendered)
	return nil
}

// generateRunID generates a unique run ID for subagent invocations
func generateRunID() string {
	const charset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const length = 8

	id, err := gonanoid.Generate(charset, length)
	if err != nil {
		// Fallback to timestamp-based ID if nanoid fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return id
}

// MCPServeOptions contains parameters for running the MCP server
type MCPServeOptions struct {
	// ConfigPath is the path to the subagent configuration file
	ConfigPath string
}

// MCPServe runs CPE as an MCP server exposing a subagent as a tool
func MCPServe(ctx context.Context, opts MCPServeOptions) error {
	// Load raw config to check subagent is configured
	rawCfg, err := config.LoadRawConfig(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate that subagent is configured
	if rawCfg.Subagent == nil {
		return fmt.Errorf("config must define a subagent for MCP server mode")
	}

	// Load and validate output schema at startup
	var outputSchema *jsonschema.Schema
	if rawCfg.Subagent.OutputSchemaPath != "" {
		schemaBytes, err := os.ReadFile(rawCfg.Subagent.OutputSchemaPath)
		if err != nil {
			return fmt.Errorf("failed to read output schema file %q: %w", rawCfg.Subagent.OutputSchemaPath, err)
		}
		if err := json.Unmarshal(schemaBytes, &outputSchema); err != nil {
			return fmt.Errorf("invalid output schema JSON in %q: %w", rawCfg.Subagent.OutputSchemaPath, err)
		}
	}

	// Initialize storage for persisting execution traces
	storageDB, err := sql.Open("sqlite3", ".cpeconvo")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer storageDB.Close()

	dialogStorage, err := storage.NewSqlite(ctx, storageDB)
	if err != nil {
		return fmt.Errorf("failed to initialize dialog storage: %w", err)
	}

	// Check for subagent logging address from environment
	loggingAddress := os.Getenv(subagentlog.SubagentLoggingAddressEnv)
	var eventClient *subagentlog.Client
	if loggingAddress != "" {
		eventClient = subagentlog.NewClient(loggingAddress)
	}

	// Derive display name by stripping "-subagent" suffix for cleaner event output
	displayName := rawCfg.Subagent.Name
	displayName = strings.TrimSuffix(displayName, "-subagent")

	// Create the executor
	executor := createSubagentExecutor(opts.ConfigPath, outputSchema, displayName, dialogStorage, eventClient)

	// Create server config
	serverCfg := mcpinternal.MCPServerConfig{
		Subagent: mcpinternal.SubagentDef{
			Name:             rawCfg.Subagent.Name,
			Description:      rawCfg.Subagent.Description,
			OutputSchemaPath: rawCfg.Subagent.OutputSchemaPath,
		},
		MCPServers: rawCfg.MCPServers,
	}

	// Create and run the MCP server
	server, err := mcpinternal.NewServer(serverCfg, mcpinternal.ServerOptions{
		Executor: executor,
	})
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}

	return server.Serve(ctx)
}

// createSubagentExecutor creates an executor function that runs the subagent.
func createSubagentExecutor(cfgPath string, outputSchema *jsonschema.Schema, subagentName string, dialogStorage storage.DialogSaver, eventClient *subagentlog.Client) mcpinternal.SubagentExecutor {
	return func(ctx context.Context, input mcpinternal.SubagentInput) (string, error) {
		// Check context before starting
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("execution cancelled before start: %w", err)
		}

		// Use the RunID from input if provided, otherwise generate one
		runID := input.RunID
		if runID == "" {
			runID = generateRunID()
		}
		subagentLabel := fmt.Sprintf("subagent:%s:%s", subagentName, runID)

		// Resolve effective config (uses defaults.model from config)
		effectiveConfig, err := config.ResolveConfig(cfgPath, config.RuntimeOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to resolve config %q: %w", cfgPath, err)
		}

		// Build user blocks from prompt and input files
		userBlocks, err := agent.BuildUserBlocks(ctx, input.Prompt, input.Inputs)
		if err != nil {
			// Provide actionable error for input file issues
			if len(input.Inputs) > 0 {
				return "", fmt.Errorf("failed to read input files %v: %w", input.Inputs, err)
			}
			return "", fmt.Errorf("failed to build user input: %w", err)
		}

		// Load and render system prompt
		systemPrompt, err := LoadSystemPrompt(ctx, LoadSystemPromptOptions{
			SystemPromptPath: effectiveConfig.SystemPromptPath,
			Config:           effectiveConfig,
			Stderr:           os.Stderr,
		})
		if err != nil {
			return "", err
		}

		// Check context before creating generator
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("execution cancelled during setup: %w", err)
		}

		// Initialize MCP connections for this execution
		mcpState, err := mcpinternal.InitializeConnections(ctx, effectiveConfig.MCPServers, "")
		if err != nil {
			return "", fmt.Errorf("failed to initialize MCP: %w", err)
		}
		defer mcpState.Close()

		generatorOpts := []agent.GeneratorOption{
			agent.WithDisablePrinting(),
		}

		// Create the generator
		generator, err := agent.NewGenerator(
			ctx,
			effectiveConfig,
			systemPrompt,
			mcpState,
			generatorOpts...,
		)
		if err != nil {
			return "", fmt.Errorf("failed to create generator for model %q: %w", effectiveConfig.Model.Ref, err)
		}

		// Build generation options function
		var genOptsFunc gai.GenOptsGenerator
		if effectiveConfig.GenerationDefaults != nil {
			genOptsFunc = func(_ gai.Dialog) *gai.GenOpts {
				return effectiveConfig.GenerationDefaults
			}
		}

		// Execute the subagent with storage and event client
		result, err := ExecuteSubagent(ctx, SubagentOptions{
			UserBlocks:    userBlocks,
			Generator:     generator,
			GenOptsFunc:   genOptsFunc,
			OutputSchema:  outputSchema,
			Storage:       dialogStorage,
			SubagentLabel: subagentLabel,
			EventClient:   eventClient,
			SubagentName:  subagentName,
			RunID:         runID,
		})
		if err != nil {
			// Annotate context cancellation errors
			if ctx.Err() != nil {
				return "", fmt.Errorf("subagent %q execution timed out or cancelled: %w", subagentName, err)
			}
			return "", fmt.Errorf("subagent %q execution failed: %w", subagentName, err)
		}
		return result, nil
	}
}

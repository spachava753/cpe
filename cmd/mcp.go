package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/google/jsonschema-go/jsonschema"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
)

const (
	// runIDCharset is the character set for generating run IDs
	runIDCharset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	// runIDLength is the length of generated run IDs
	runIDLength = 8
)

var (
	mcpServerName string
	mcpToolName   string
	mcpToolArgs   string
)

// mcpCmd represents the mcp command
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol client",
	Long:  `Interact with Model Context Protocol (MCP) servers.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// If no subcommand is specified, show help
		return cmd.Help()
	},
}

// mcpListServersCmd represents the 'mcp list-servers' subcommand
var mcpListServersCmd = &cobra.Command{
	Use:     "list-servers",
	Short:   "List configured MCP servers",
	Long:    `List all MCP servers defined in .cpemcp.json configuration file.`,
	Aliases: []string{"ls-servers"},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{})
		if err != nil {
			return err
		}

		return commands.MCPListServers(cmd.Context(), commands.MCPListServersOptions{
			MCPServers: cfg.MCPServers,
			Writer:     os.Stdout,
		})
	},
}

// mcpInfoCmd represents the 'mcp info' subcommand
var mcpInfoCmd = &cobra.Command{
	Use:   "info [server_name]",
	Short: "Get information about an MCP server",
	Long:  `Initialize connection to an MCP server and show its information.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{})
		if err != nil {
			return err
		}

		return commands.MCPInfo(cmd.Context(), commands.MCPInfoOptions{
			MCPServers: cfg.MCPServers,
			ServerName: args[0],
			Writer:     os.Stdout,
			Timeout:    30 * time.Second,
		})
	},
}

// mcpListToolsCmd represents the 'mcp list-tools' subcommand
var mcpListToolsCmd = &cobra.Command{
	Use:   "list-tools [server_name]",
	Short: "List tools available on an MCP server",
	Long:  `Connect to an MCP server and list available tools with their input schemas.`,
	Example: `  # List tools with human-readable schema
  cpe mcp list-tools my-server
  
  # List tools with JSON schema format
  cpe mcp list-tools my-server --json
  
  # Show all tools including filtered ones
  cpe mcp list-tools my-server --show-all
  
  # Show only filtered-out tools
  cpe mcp list-tools my-server --show-filtered`,
	Aliases: []string{"ls-tools"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{})
		if err != nil {
			return err
		}

		showAll, _ := cmd.Flags().GetBool("show-all")
		showFiltered, _ := cmd.Flags().GetBool("show-filtered")

		renderer, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(120),
		)
		if err != nil {
			return fmt.Errorf("failed to create markdown renderer: %w", err)
		}

		return commands.MCPListTools(cmd.Context(), commands.MCPListToolsOptions{
			MCPServers:   cfg.MCPServers,
			ServerName:   args[0],
			Writer:       os.Stdout,
			ShowAll:      showAll,
			ShowFiltered: showFiltered,
			Renderer:     renderer,
		})
	},
}

// mcpCallToolCmd represents the 'mcp call-tool' subcommand
var mcpCallToolCmd = &cobra.Command{
	Use:   "call-tool",
	Short: "Call a tool on an MCP server",
	Long:  `Call a specific tool on an MCP server with arguments.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpServerName == "" {
			return fmt.Errorf("--server is required")
		}

		if mcpToolName == "" {
			return fmt.Errorf("--tool is required")
		}

		cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{})
		if err != nil {
			return err
		}

		toolArgs := make(map[string]any)
		if mcpToolArgs != "" {
			if err := json.Unmarshal([]byte(mcpToolArgs), &toolArgs); err != nil {
				return fmt.Errorf("invalid tool arguments JSON: %w", err)
			}
		}

		return commands.MCPCallTool(cmd.Context(), commands.MCPCallToolOptions{
			MCPServers: cfg.MCPServers,
			ServerName: mcpServerName,
			ToolName:   mcpToolName,
			ToolArgs:   toolArgs,
			Writer:     os.Stdout,
		})
	},
}

// mcpServeCmd represents the 'mcp serve' subcommand
var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run CPE as an MCP server",
	Long: `Start CPE as an MCP server that exposes a configured subagent as a tool.

The server communicates via stdio and exposes exactly one tool based on 
the subagent configuration in the provided config file.

This command requires an explicit --config flag pointing to a subagent 
configuration file. The default config search behavior is disabled.`,
	Example: `  # Start the MCP server with a subagent config
  cpe mcp serve --config ./coder_agent.cpe.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// For mcp serve, we require an explicit config path - don't use default search
		configFlag := cmd.Root().PersistentFlags().Lookup("config")
		if configFlag == nil || !configFlag.Changed {
			return fmt.Errorf("--config flag is required for mcp serve")
		}

		// Load raw config to check subagent is configured
		rawCfg, err := config.LoadRawConfig(configPath)
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
		dialogStorage, err := storage.InitDialogStorage(".cpeconvo")
		if err != nil {
			return fmt.Errorf("failed to initialize dialog storage: %w", err)
		}
		defer dialogStorage.Close()

		// Create the executor with pre-loaded schema and storage
		executor := createSubagentExecutor(configPath, outputSchema, rawCfg.Subagent.Name, dialogStorage)

		// Create server config
		serverCfg := mcp.MCPServerConfig{
			Subagent: mcp.SubagentDef{
				Name:             rawCfg.Subagent.Name,
				Description:      rawCfg.Subagent.Description,
				OutputSchemaPath: rawCfg.Subagent.OutputSchemaPath,
			},
			MCPServers: rawCfg.MCPServers,
		}

		// Create and run the MCP server
		server, err := mcp.NewServer(serverCfg, mcp.ServerOptions{
			Executor: executor,
		})
		if err != nil {
			return fmt.Errorf("failed to create MCP server: %w", err)
		}

		return server.Serve(cmd.Context())
	},
}

// generateRunID generates a unique run ID for subagent invocations
func generateRunID() string {
	id, err := gonanoid.Generate(runIDCharset, runIDLength)
	if err != nil {
		// Fallback to timestamp-based ID if nanoid fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return id
}

// createSubagentExecutor creates an executor function that runs the subagent.
// The outputSchema is pre-loaded at startup and passed to each execution.
// Storage and subagent name are used to persist execution traces.
func createSubagentExecutor(cfgPath string, outputSchema *jsonschema.Schema, subagentName string, dialogStorage commands.DialogStorage) mcp.SubagentExecutor {
	return func(ctx context.Context, input mcp.SubagentInput) (string, error) {
		// Generate unique run ID for this invocation
		runID := generateRunID()
		subagentLabel := fmt.Sprintf("subagent:%s:%s", subagentName, runID)

		// Resolve effective config (uses defaults.model from config)
		effectiveConfig, err := config.ResolveConfig(cfgPath, config.RuntimeOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to resolve config: %w", err)
		}

		// Build user blocks from prompt and input files
		userBlocks, err := agent.BuildUserBlocks(ctx, input.Prompt, input.Inputs)
		if err != nil {
			return "", fmt.Errorf("failed to build user input: %w", err)
		}

		// Load and render system prompt (same pattern as root.go)
		var systemPrompt string
		if effectiveConfig.SystemPromptPath != "" {
			f, err := os.Open(effectiveConfig.SystemPromptPath)
			if err != nil {
				return "", fmt.Errorf("could not open system prompt file: %w", err)
			}
			defer f.Close()

			contents, err := io.ReadAll(f)
			if err != nil {
				return "", fmt.Errorf("failed to read system prompt file: %w", err)
			}

			systemPrompt, err = agent.SystemPromptTemplate(string(contents), agent.TemplateData{
				Config: effectiveConfig,
			})
			if err != nil {
				return "", fmt.Errorf("failed to prepare system prompt: %w", err)
			}
		}

		// Create the generator
		generator, err := agent.CreateToolCapableGenerator(
			ctx,
			effectiveConfig.Model,
			systemPrompt,
			effectiveConfig.Timeout,
			effectiveConfig.NoStream,
			effectiveConfig.MCPServers,
			effectiveConfig.CodeMode,
		)
		if err != nil {
			return "", fmt.Errorf("failed to create generator: %w", err)
		}

		// Build generation options function
		var genOptsFunc gai.GenOptsGenerator
		if effectiveConfig.GenerationDefaults != nil {
			genOptsFunc = func(_ gai.Dialog) *gai.GenOpts {
				return effectiveConfig.GenerationDefaults
			}
		}

		// Execute the subagent with storage
		return commands.ExecuteSubagent(ctx, commands.SubagentOptions{
			UserBlocks:    userBlocks,
			Generator:     generator,
			GenOptsFunc:   genOptsFunc,
			OutputSchema:  outputSchema,
			Storage:       dialogStorage,
			SubagentLabel: subagentLabel,
		})
	}
}

func init() {
	rootCmd.AddCommand(mcpCmd)

	// Add subcommands to mcp command
	mcpCmd.AddCommand(mcpListServersCmd)
	mcpCmd.AddCommand(mcpInfoCmd)
	mcpCmd.AddCommand(mcpListToolsCmd)
	mcpCmd.AddCommand(mcpCallToolCmd)
	mcpCmd.AddCommand(mcpServeCmd)

	// Add flags to mcp list-tools command
	mcpListToolsCmd.Flags().Bool("show-all", false, "Show all tools including filtered ones")
	mcpListToolsCmd.Flags().Bool("show-filtered", false, "Show only filtered-out tools")

	// Add flags to call-tool command
	mcpCallToolCmd.Flags().StringVar(&mcpServerName, "server", "", "MCP server name")
	mcpCallToolCmd.Flags().StringVar(&mcpToolName, "tool", "", "Tool name to call")
	mcpCallToolCmd.Flags().StringVar(&mcpToolArgs, "args", "{}", "Tool arguments in JSON format")
}

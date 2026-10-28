package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lithammer/dedent"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/terminaltext"
)

// defaultMCPTimeout bounds connecting to and listing one server; it must allow
// a cold npx or uvx start.
const defaultMCPTimeout = time.Minute

// Server statuses reported by cpe mcp.
const (
	statusConnected = "connected"
	statusDisabled  = "disabled"
	statusFailed    = "failed"
)

// mcpInspector resolves and connects servers the way a session would.
type mcpInspector struct {
	connector  mcpclient.Connector
	filesystem afero.Fs
	configDir  string
}

type mcpServerInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type mcpToolReport struct {
	InputSchema  any    `json:"input_schema,omitempty"`
	OutputSchema any    `json:"output_schema,omitempty"`
	Name         string `json:"name"`
	MCPName      string `json:"mcp_name"`
	Description  string `json:"description,omitempty"`
}

// mcpServerReport describes one configured server and how connecting went.
type mcpServerReport struct {
	Server       *mcpServerInfo  `json:"server,omitempty"`
	Name         string          `json:"name"`
	Prefix       string          `json:"prefix,omitempty"`
	Source       string          `json:"source"`
	Status       string          `json:"status"`
	Error        string          `json:"error,omitempty"`
	Stderr       string          `json:"stderr,omitempty"`
	Description  string          `json:"description,omitempty"`
	Instructions string          `json:"instructions,omitempty"`
	Tools        []mcpToolReport `json:"tools"`
}

type mcpOptions struct {
	pluginPaths []string
	timeout     time.Duration
	schemas     bool
	json        bool
}

func mcpCmd(root *cobra.Command, filesystem afero.Fs, configDir string) {
	mcpCmdWithConnector(
		root,
		filesystem,
		configDir,
		mcpclient.Connect,
	)
}

func mcpCmdWithConnector(root *cobra.Command, filesystem afero.Fs, configDir string, connector mcpclient.Connector) {
	inspector := mcpInspector{
		connector:  connector,
		filesystem: filesystem,
		configDir:  configDir,
	}
	var options mcpOptions
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Inspect and debug MCP servers",
		Long: dedent.Dedent(`
		Inspect the MCP servers a session started in the current directory would
		use: servers from ~/.cpe/settings.json and from installed plugins.
		Servers an ACP client supplies exist only inside that client's sessions.

		Sessions skip servers that fail to connect, so use these commands to find
		and debug them.
		`),
	}
	c.PersistentFlags().DurationVar(
		&options.timeout,
		"timeout",
		defaultMCPTimeout,
		"Maximum time to connect to one server and list its tools",
	)
	c.PersistentFlags().StringSliceVar(
		&options.pluginPaths,
		"plugins",
		nil,
		"Additional plugin roots, as for cpe acp",
	)
	c.PersistentFlags().BoolVar(
		&options.json,
		"json",
		false,
		"Output JSON (automatic when stdout is not a terminal)",
	)
	list := &cobra.Command{
		Use: "list",
		Aliases: []string{
			"ls",
		},
		Short: "Connect to every server and report its status and tools",
		Long: dedent.Dedent(`
		Connect to every configured server concurrently, as session setup does, and
		report each server's source, status, description, and tool names. Exits
		nonzero if any enabled server failed.
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reports, err := inspector.inspect(
				cmd.Context(),
				options,
				"",
			)
			if err != nil {
				return err
			}
			if err := writeMCPList(
				cmd.OutOrStdout(),
				reports,
				options.json,
			); err != nil {
				return err
			}
			return failedServers(reports)
		},
	}
	show := &cobra.Command{
		Use:   "show NAME",
		Short: "Connect to one server and print what a session learns from it",
		Long: dedent.Dedent(`
		Connect to one server, named by its configured name or callable prefix, and
		print its server information, instructions, and tools. --schemas adds each
		tool's input and output schemas. When connecting fails, the output includes
		the stdio server's stderr.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reports, err := inspector.inspect(
				cmd.Context(),
				options,
				args[0],
			)
			if err != nil {
				return err
			}
			if err := writeMCPShow(
				cmd.OutOrStdout(),
				reports[0],
				options,
			); err != nil {
				return err
			}
			return failedServers(reports)
		},
	}
	show.Flags().BoolVar(
		&options.schemas,
		"schemas",
		false,
		"Include each tool's input and output schemas",
	)
	c.AddCommand(
		list,
		show,
	)
	root.AddCommand(c)
}

func failedServers(reports []mcpServerReport) error {
	var failed []string
	for _, report := range reports {
		if report.Status == statusFailed {
			failed = append(
				failed,
				report.Name,
			)
		}
	}
	if len(failed) != 0 {
		return fmt.Errorf(
			"failed MCP servers: %s",
			strings.Join(
				failed,
				", ",
			),
		)
	}
	return nil
}

// loadSettings reads settings.json; a missing file means no settings.
func loadSettings(filesystem afero.Fs, configDir string) (config.Config, error) {
	settings, err := config.LoadConfig(afero.NewIOFS(afero.NewBasePathFs(
		filesystem,
		configDir,
	)))
	if errors.Is(
		err,
		os.ErrNotExist,
	) {
		return config.Config{}, nil
	}
	if err != nil {
		return config.Config{}, fmt.Errorf(
			"read settings: %w",
			err,
		)
	}
	return settings, nil
}

// limitedBuffer keeps the last limit bytes written to it.
type limitedBuffer struct {
	data  []byte
	limit int
	mu    sync.Mutex
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(
		b.data,
		p...,
	)
	if len(b.data) > b.limit {
		b.data = slices.Clone(b.data[len(b.data)-b.limit:])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

// inspect connects every server, or only the one named, and reports the result.
func (i mcpInspector) inspect(ctx context.Context, options mcpOptions, name string) ([]mcpServerReport, error) {
	if options.timeout <= 0 {
		return nil, errors.New("timeout must be positive")
	}
	if i.configDir == "" {
		return nil, errors.New("CPE configuration directory is unavailable")
	}
	settings, err := loadSettings(
		i.filesystem,
		i.configDir,
	)
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	pluginPaths, err := discoveryRoots(
		"plugins",
		options.pluginPaths,
	)
	if err != nil {
		return nil, err
	}
	specs, invalid, err := agent.MCPServers(
		ctx,
		i.filesystem,
		cwd,
		agent.Config{
			MCPServers:  settings.MCPServers,
			PluginPaths: pluginPaths,
		},
	)
	if err != nil {
		return nil, err
	}
	prefixes, err := mcpclient.Prefixes(specs)
	if err != nil {
		return nil, err
	}
	reports := []mcpServerReport{}
	for _, failure := range invalid {
		reports = append(
			reports,
			mcpServerReport{
				Name:   string(failure.Spec.ID),
				Source: source(failure.Spec),
				Status: statusFailed,
				Error:  failure.Err.Error(),
			},
		)
	}
	for _, server := range slices.Sorted(maps.Keys(settings.MCPServers)) {
		if !settings.MCPServers[server].IsEnabled() {
			reports = append(
				reports,
				mcpServerReport{
					Name:   server,
					Source: mcpclient.SourceSettings.String(),
					Status: statusDisabled,
				},
			)
		}
	}
	if name != "" {
		var selected []mcpclient.ServerSpec
		for _, spec := range specs {
			if string(spec.ID) == name || prefixes[spec.ID] == name {
				selected = append(
					selected,
					spec,
				)
			}
		}
		var matched []mcpServerReport
		for _, report := range reports {
			if report.Name == name {
				matched = append(
					matched,
					report,
				)
			}
		}
		if len(selected) == 0 && len(matched) == 0 {
			return nil, fmt.Errorf(
				"no MCP server named %q",
				name,
			)
		}
		specs = selected
		reports = matched
	}
	stderr := &limitedBuffer{
		limit: 64 * 1024,
	}
	pool, err := mcpclient.NewPool(
		i.connector,
		0,
		stderr,
	)
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	servers, failures := mcpclient.Setup(
		ctx,
		pool,
		specs,
		options.timeout,
	)
	defer servers.Close()
	catalog, err := mcpclient.NewCatalog(
		prefixes,
		servers.Connected(),
	)
	if err != nil {
		return nil, err
	}
	for _, failure := range failures {
		report := mcpServerReport{
			Name:   string(failure.Spec.ID),
			Prefix: prefixes[failure.Spec.ID],
			Source: source(failure.Spec),
			Status: statusFailed,
			Error:  failure.Err.Error(),
		}
		if name != "" {
			report.Stderr = stderr.String()
		}
		reports = append(
			reports,
			report,
		)
	}
	for _, server := range catalog.Servers() {
		report := mcpServerReport{
			Name:        string(server.Spec.ID),
			Prefix:      server.Prefix,
			Source:      source(server.Spec),
			Status:      statusConnected,
			Description: server.Description(),
			Server: &mcpServerInfo{
				Name:        server.Info.Name,
				Version:     server.Info.Version,
				Description: server.Info.Description,
			},
			Instructions: server.Instructions,
			Tools:        []mcpToolReport{},
		}
		for _, tool := range server.Tools {
			entry := mcpToolReport{
				Name:        tool.Name,
				MCPName:     tool.Tool.Name,
				Description: tool.Tool.Description,
			}
			if options.schemas {
				entry.InputSchema = tool.Tool.InputSchema
				entry.OutputSchema = tool.Tool.OutputSchema
			}
			report.Tools = append(
				report.Tools,
				entry,
			)
		}
		reports = append(
			reports,
			report,
		)
	}
	for index := range reports {
		if reports[index].Tools == nil {
			reports[index].Tools = []mcpToolReport{}
		}
	}
	slices.SortFunc(
		reports,
		func(a, b mcpServerReport) int {
			return strings.Compare(
				a.Name,
				b.Name,
			)
		},
	)
	return reports, nil
}

func source(spec mcpclient.ServerSpec) string {
	if spec.Source == mcpclient.SourcePlugin {
		return "plugin " + spec.Plugin
	}
	return spec.Source.String()
}

func writeMCPList(output io.Writer, reports []mcpServerReport, asJSON bool) error {
	if commandio.UseJSON(
		output,
		asJSON,
	) {
		encoder := json.NewEncoder(output)
		encoder.SetIndent(
			"",
			"  ",
		)
		return encoder.Encode(reports)
	}
	if len(reports) == 0 {
		_, err := fmt.Fprintln(
			output,
			"No MCP servers are configured.",
		)
		return err
	}
	rows := make(
		[][]string,
		0,
		len(reports),
	)
	for _, report := range reports {
		status := report.Status
		if report.Error != "" {
			status += ": " + report.Error
		}
		var tools []string
		for _, tool := range report.Tools {
			tools = append(
				tools,
				tool.Name,
			)
		}
		rows = append(
			rows,
			[]string{
				terminaltext.Sanitize(report.Name),
				terminaltext.Sanitize(report.Prefix),
				terminaltext.Sanitize(report.Source),
				terminaltext.Sanitize(status),
				terminaltext.Sanitize(report.Description),
				terminaltext.Sanitize(strings.Join(
					tools,
					", ",
				)),
			},
		)
	}
	return commandio.WriteTable(
		output,
		[]string{
			"NAME",
			"PREFIX",
			"SOURCE",
			"STATUS",
			"DESCRIPTION",
			"TOOLS",
		},
		rows,
	)
}

func writeMCPShow(output io.Writer, report mcpServerReport, options mcpOptions) error {
	if commandio.UseJSON(
		output,
		options.json,
	) {
		encoder := json.NewEncoder(output)
		encoder.SetIndent(
			"",
			"  ",
		)
		return encoder.Encode(report)
	}
	var text strings.Builder
	fmt.Fprintf(
		&text,
		"Name: %s\nSource: %s\nStatus: %s\n",
		report.Name,
		report.Source,
		report.Status,
	)
	if report.Prefix != "" {
		fmt.Fprintf(
			&text,
			"Prefix: %s\n",
			report.Prefix,
		)
	}
	if report.Error != "" {
		fmt.Fprintf(
			&text,
			"Error: %s\n",
			report.Error,
		)
	}
	if report.Stderr != "" {
		fmt.Fprintf(
			&text,
			"\nServer stderr:\n%s\n",
			strings.TrimRight(
				report.Stderr,
				"\n",
			),
		)
	}
	if report.Server != nil {
		fmt.Fprintf(
			&text,
			"Server: %s %s\n",
			report.Server.Name,
			report.Server.Version,
		)
	}
	if report.Description != "" {
		fmt.Fprintf(
			&text,
			"Description: %s\n",
			report.Description,
		)
	}
	if report.Instructions != "" {
		fmt.Fprintf(
			&text,
			"\nInstructions:\n%s\n",
			strings.TrimRight(
				report.Instructions,
				"\n",
			),
		)
	}
	if report.Status == statusConnected {
		fmt.Fprintf(
			&text,
			"\nTools (%d):\n",
			len(report.Tools),
		)
	}
	for _, tool := range report.Tools {
		fmt.Fprintf(
			&text,
			"\n%s (%s)\n",
			tool.Name,
			tool.MCPName,
		)
		if tool.Description != "" {
			fmt.Fprintf(
				&text,
				"  %s\n",
				strings.ReplaceAll(
					strings.TrimRight(
						tool.Description,
						"\n",
					),
					"\n",
					"\n  ",
				),
			)
		}
		for _, schema := range []struct {
			value any
			label string
		}{
			{
				label: "Input schema",
				value: tool.InputSchema,
			},
			{
				label: "Output schema",
				value: tool.OutputSchema,
			},
		} {
			if schema.value == nil {
				continue
			}
			encoded, err := json.MarshalIndent(
				schema.value,
				"  ",
				"  ",
			)
			if err != nil {
				return err
			}
			fmt.Fprintf(
				&text,
				"  %s: %s\n",
				schema.label,
				encoded,
			)
		}
	}
	_, err := io.WriteString(
		output,
		terminaltext.Sanitize(text.String()),
	)
	return err
}

package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/repl"
)

var serverName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var toolPunctuation = regexp.MustCompile(`[^A-Za-z0-9_]`)

// Connection owns an MCP client session and its discovered Starlark functions.
// Close it after closing agents using Tools. Reconnect to refresh the catalog.
type Connection struct {
	session *mcp.ClientSession
	tools   []repl.Tool
}

// Connect initializes a server and discovers its tools using the supplied
// transport. Failed discovery closes the connection without returning tools.
func Connect(ctx context.Context, name string, transport mcp.Transport) (*Connection, error) {
	if !serverName.MatchString(name) {
		return nil, fmt.Errorf("MCP server name %q must be an ASCII identifier", name)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "cpe", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("MCP %s: %w", name, err)
	}
	c := &Connection{session: session}
	if err := c.discover(ctx, name); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Connection) discover(ctx context.Context, name string) error {
	names := make(map[string]bool)
	for tool, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("MCP %s tools/list: %w", name, err)
		}
		function := "mcp_" + name + "__" + toolPunctuation.ReplaceAllString(tool.Name, "_")
		if tool.Name == "" || names[function] {
			return fmt.Errorf("MCP %s: empty or colliding tool name %q", name, tool.Name)
		}
		names[function] = true
		data, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return err
		}
		var schema jsonschema.Schema
		if err := json.Unmarshal(data, &schema); err != nil {
			return fmt.Errorf("MCP %s/%s input schema: %w", name, tool.Name, err)
		}
		if schema.Type != "object" {
			return fmt.Errorf("MCP %s/%s input schema must describe an object", name, tool.Name)
		}
		c.tools = append(c.tools, repl.Tool{
			Name: function, Schema: &schema,
			Description: fmt.Sprintf("MCP %s/%s: %s\nReturns an MCP result dictionary: content contains typed text/image/audio/resource blocks; structuredContent contains structured output when present. Check result.get(\"isError\", False). Use emit_image on selected image content to show it to the model.", name, tool.Name, tool.Description),
			Execute: func(ctx context.Context, input map[string]any) (any, error) {
				return c.session.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: input})
			},
		})
	}
	slices.SortFunc(c.tools, func(a, b repl.Tool) int { return strings.Compare(a.Name, b.Name) })
	return nil
}

// Tools returns the discovered functions for injection into an agent or REPL.
// Treat the definitions as immutable; they remain usable until Close.
func (c *Connection) Tools() []repl.Tool { return slices.Clone(c.tools) }

// Close releases the transport (including a subprocess for command transports).
func (c *Connection) Close() error { return c.session.Close() }

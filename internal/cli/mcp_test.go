package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/plugins"
)

// mcpFixture connects commands named "good-*" to an in-memory server and fails
// every other command after writing to its stderr.
func mcpFixture(t *testing.T) func(context.Context, plugins.MCPServer, io.Writer) (*mcp.ClientSession, error) {
	return func(ctx context.Context, server plugins.MCPServer, stderr io.Writer) (*mcp.ClientSession, error) {
		if !strings.HasPrefix(
			server.Command,
			"good-",
		) {
			if stderr != nil {
				_, _ = io.WriteString(
					stderr,
					"fatal: token is not set\n",
				)
			}
			return nil, errors.New("server exited")
		}
		remote := mcp.NewServer(
			&mcp.Implementation{
				Name:        server.Command,
				Version:     "2.0.0",
				Description: "Reported description.",
			},
			&mcp.ServerOptions{
				Instructions: "Use carefully.",
			},
		)
		remote.AddTool(
			&mcp.Tool{
				Name:        "look-up",
				Description: "Look something up.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
			},
			func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			},
		)
		a, b := mcp.NewInMemoryTransports()
		session, err := remote.Connect(
			ctx,
			a,
			nil,
		)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = session.Close() })
		return mcp.NewClient(
			&mcp.Implementation{
				Name:    "test",
				Version: "1",
			},
			nil,
		).Connect(
			ctx,
			b,
			nil,
		)
	}
}

func runMCP(t *testing.T, settings string, args ...string) (string, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(
		"HOME",
		home,
	)
	t.Chdir(t.TempDir())
	filesystem := afero.NewOsFs()
	configDir := filepath.Join(
		home,
		".cpe",
	)
	if settings != "" {
		if err := filesystem.MkdirAll(
			configDir,
			0o700,
		); err != nil {
			t.Fatal(err)
		}
		if err := afero.WriteFile(
			filesystem,
			filepath.Join(
				configDir,
				"settings.json",
			),
			[]byte(settings),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	root := &cobra.Command{
		Use:           "cpe",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	mcpCmdWithConnector(
		root,
		filesystem,
		configDir,
		mcpFixture(t),
	)
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs(append(
		[]string{
			"mcp",
		},
		args...,
	))
	err := root.ExecuteContext(t.Context())
	return output.String(), err
}

const mcpSettings = `{"mcpServers": {
  "docs": {"command": "good-docs", "description": "Configured docs."},
  "broken": {"command": "bad-server"},
  "invalid": {},
  "off": {"command": "good-off", "enabled": false}
}}`

func TestMCPList(t *testing.T) {
	output, err := runMCP(
		t,
		mcpSettings,
		"list",
	)
	if err == nil || err.Error() != "failed MCP servers: broken, invalid" {
		t.Fatalf(
			"list error = %v",
			err,
		)
	}
	var reports []mcpServerReport
	if err := json.Unmarshal(
		[]byte(output),
		&reports,
	); err != nil {
		t.Fatalf(
			"output is not JSON: %v\n%s",
			err,
			output,
		)
	}
	statuses := make(map[string]string)
	for _, report := range reports {
		statuses[report.Name] = report.Status
	}
	if len(reports) != 4 || statuses["docs"] != "connected" || statuses["broken"] != "failed" || statuses["invalid"] != "failed" || statuses["off"] != "disabled" {
		t.Fatalf(
			"reports = %+v",
			reports,
		)
	}
	docs := reports[1]
	if docs.Name != "docs" || docs.Description != "Configured docs." || docs.Server.Version != "2.0.0" || len(docs.Tools) != 1 || docs.Tools[0].Name != "docs__look_up" || docs.Tools[0].InputSchema != nil {
		t.Fatalf(
			"docs report = %+v",
			docs,
		)
	}
	if reports[0].Stderr != "" {
		t.Fatal("list should not attach a shared stderr buffer to individual servers")
	}
}

func TestMCPShow(t *testing.T) {
	output, err := runMCP(
		t,
		mcpSettings,
		"show",
		"docs",
		"--schemas",
		"--json",
	)
	if err != nil {
		t.Fatal(err)
	}
	var report mcpServerReport
	if err = json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatal(err)
	}
	if report.Instructions != "Use carefully." || report.Tools[0].InputSchema == nil {
		t.Fatalf(
			"report = %+v",
			report,
		)
	}
	output, err = runMCP(
		t,
		mcpSettings,
		"show",
		"broken",
		"--json",
	)
	if err == nil || !strings.Contains(
		output,
		"fatal: token is not set",
	) {
		t.Fatalf(
			"broken show = %q, %v",
			output,
			err,
		)
	}
	if _, err = runMCP(
		t,
		mcpSettings,
		"show",
		"nope",
	); err == nil || !strings.Contains(
		err.Error(),
		`no MCP server named "nope"`,
	) {
		t.Fatalf(
			"unknown show error = %v",
			err,
		)
	}
	output, err = runMCP(
		t,
		"",
		"list",
	)
	if err != nil || strings.TrimSpace(output) != "[]" {
		t.Fatalf(
			"no settings = %q, %v",
			output,
			err,
		)
	}
}

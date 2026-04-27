package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/textedit"
	"github.com/spachava753/cpe/internal/version"
)

const builtinServerType = "builtin"

func connectBuiltinServerSession(
	ctx context.Context,
	client *mcpsdk.Client,
	serverName string,
	config mcpconfig.ServerConfig,
) (*MCPConn, error) {
	server := newBuiltinTextServer()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverCtx, cancelServer := context.WithCancel(ctx)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(serverCtx, serverTransport)
	}()

	operationCtx, cancelOperation := WithServerTimeout(ctx, config)
	defer cancelOperation()

	session, err := client.Connect(operationCtx, clientTransport, nil)
	if err != nil {
		cancelServer()
		return nil, fmt.Errorf("connecting builtin server: %w", err)
	}

	return &MCPConn{
		ServerName:    serverName,
		Config:        config,
		ClientSession: session,
		close: func() error {
			cancelServer()
			select {
			case err := <-serverDone:
				if err == nil || errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			case <-time.After(5 * time.Second):
				return fmt.Errorf("timed out waiting for builtin server shutdown")
			}
		},
	}, nil
}

func newBuiltinTextServer() *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "cpe-builtin-text",
		Title:   "CPE Builtin Text Tools",
		Version: version.Get(),
	}, nil)

	mcpsdk.AddTool[textedit.Input, textedit.Output](server, &mcpsdk.Tool{
		Name:        textedit.ToolName,
		Title:       "Text Edit",
		Description: "Create a new text file or replace exactly one occurrence of text in an existing UTF-8 file.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input textedit.Input) (*mcpsdk.CallToolResult, textedit.Output, error) {
		output, err := textedit.Apply(input)
		if err != nil {
			return nil, textedit.Output{}, err
		}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: output.Message()}},
		}, output, nil
	})

	return server
}

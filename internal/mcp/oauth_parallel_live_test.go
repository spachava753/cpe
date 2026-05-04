package mcp

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	authsdk "github.com/modelcontextprotocol/go-sdk/auth"

	cpeauth "github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/testutil/testgate"
)

func TestLiveParallelMCPDCRConnectAndListTools(t *testing.T) {
	testgate.RequireLive(t)
	testgate.Require(t, testgate.Interactive)

	const parallelMCPEndpoint = "https://search.parallel.ai/mcp-oauth"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	serverConfig := mcpconfig.ServerConfig{
		Type:    "http",
		URL:     parallelMCPEndpoint,
		Timeout: 300,
	}
	transport, err := createTransport(ctx, serverConfig, liveBrowserAuthorizationCodeFetcher(t, mcpOAuthRedirectURL()))
	if err != nil {
		t.Fatalf("createTransport() error = %v", err)
	}

	session, err := NewClient().Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	conn := &MCPConn{ServerName: "parallel", Config: serverConfig, ClientSession: session}
	defer conn.Close()

	if err := populateConnectionTools(ctx, conn); err != nil {
		t.Fatalf("populateConnectionTools() error = %v", err)
	}
	if len(conn.Tools) == 0 {
		t.Fatal("Parallel MCP server returned no tools")
	}

	toolNames := make([]string, 0, len(conn.Tools))
	for _, tool := range conn.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	t.Logf("connected to Parallel MCP and listed %d tool(s): %v", len(toolNames), toolNames)
}

func liveBrowserAuthorizationCodeFetcher(t *testing.T, redirectURL string) authsdk.AuthorizationCodeFetcher {
	t.Helper()
	return func(ctx context.Context, args *authsdk.AuthorizationArgs) (*authsdk.AuthorizationResult, error) {
		receiver := newCodeReceiver(redirectURL)
		parsedRedirect, err := url.Parse(redirectURL)
		if err != nil {
			return nil, fmt.Errorf("parsing OAuth redirect URL: %w", err)
		}
		listener, err := new(net.ListenConfig).Listen(ctx, "tcp", parsedRedirect.Host)
		if err != nil {
			return nil, fmt.Errorf("starting OAuth callback listener: %w", err)
		}
		go receiver.serveRedirectHandler(listener)
		defer listener.Close()
		defer receiver.close()

		t.Logf("opening browser for MCP OAuth authorization; if it does not open, visit: %s", args.URL)
		if err := cpeauth.OpenBrowser(ctx, args.URL); err != nil {
			return nil, fmt.Errorf("opening browser for OAuth authorization: %w", err)
		}

		select {
		case authRes := <-receiver.authChan:
			return authRes, nil
		case err := <-receiver.errChan:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

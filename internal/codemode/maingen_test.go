package codemode

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpcpe "github.com/spachava753/cpe/internal/mcp"
)

func TestGenerateMainGo(t *testing.T) {
	tests := []struct {
		name    string
		servers []ServerToolsInfo
		want    string
		wantErr bool
	}{
		{
			name:    "empty servers list",
			servers: []ServerToolsInfo{},
			want: `package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func fatalExit(err error) {
	fmt.Println(err)
	os.Exit(3)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err := Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
`,
		},
		{
			name: "single stdio server with one tool",
			servers: []ServerToolsInfo{
				{
					ServerName: "editor",
					Config: mcpcpe.ServerConfig{
						Type:    "stdio",
						Command: "editor-mcp",
						Args:    []string{"--verbose"},
					},
					Tools: []*mcp.Tool{
						{
							Name:        "read_file",
							Description: "Read a file from disk",
							InputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"path": map[string]any{
										"type":        "string",
										"description": "File path",
									},
								},
							},
							OutputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"content": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
			want: `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fatalExit(err error) {
	fmt.Println(err)
	os.Exit(3)
}

// callMcpTool is a reusable utility function for calling an MCP tool
func callMcpTool[I any, O any](ctx context.Context, clientSession *mcp.ClientSession, toolName string, input I) (O, error) {
	var output O

	if err := ctx.Err(); err != nil {
		return output, err
	}

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: input})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return output, err
		}
		fatalExit(fmt.Errorf("error calling tool %s: %w", toolName, err))
	}

	if len(result.Content) != 1 {
		fatalExit(fmt.Errorf("expected 1 content part from tool %s, got %d", toolName, len(result.Content)))
	}

	var textContent string

	switch c := result.Content[0].(type) {
	case *mcp.TextContent:
		textContent = c.Text
	default:
		fatalExit(fmt.Errorf("unexpected content type returned from tool %s, cannot handle multimedia except text", toolName))
	}

	if result.IsError {
		return output, errors.New(textContent)
	}

	// If O is string, return raw text content directly
	if _, isString := any(output).(string); isString {
		return any(textContent).(O), nil
	}

	outputJson := []byte(textContent)

	if result.StructuredContent != nil {
		structuredContent, err := json.Marshal(result.StructuredContent)
		if err != nil {
			fatalExit(fmt.Errorf("could not marshal structured content: %w", err))
		}
		outputJson = structuredContent
	}

	if err := json.Unmarshal(outputJson, &output); err != nil {
		fatalExit(fmt.Errorf("could not unmarshal structured content json into output for tool %s: %w", toolName, err))
	}
	return output, nil
}

type ReadFileInput struct {
	// Path File path
	Path string ` + "`json:\"path\"`" + `
}

type ReadFileOutput struct {
	Content string ` + "`json:\"content\"`" + `
}

// ReadFile Read a file from disk
var ReadFile func(ctx context.Context, input ReadFileInput) (ReadFileOutput, error)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "cpe-code-mode", Version: "v1.0.0"}, nil)

	// editor server
	editorSessionCmd := exec.Command("editor-mcp", "--verbose")
	editorSessionTransport := &mcp.CommandTransport{Command: editorSessionCmd}
	editorSession, err := mcpClient.Connect(ctx, editorSessionTransport, nil)
	if err != nil {
		fatalExit(fmt.Errorf("could not connect to editor server: %w", err))
	}
	defer editorSession.Close()

	// Initialize tool functions
	ReadFile = func(ctx context.Context, input ReadFileInput) (ReadFileOutput, error) {
		return callMcpTool[ReadFileInput, ReadFileOutput](ctx, editorSession, "read_file", input)
	}

	err = Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
`,
		},
		{
			name: "stdio server with environment variables",
			servers: []ServerToolsInfo{
				{
					ServerName: "my-server",
					Config: mcpcpe.ServerConfig{
						Type:    "stdio",
						Command: "my-mcp",
						Env: map[string]string{
							"API_KEY": "secret123",
						},
					},
					Tools: []*mcp.Tool{
						{
							Name:        "ping",
							Description: "Ping the server",
							InputSchema: map[string]any{},
							OutputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"pong": map[string]any{"type": "boolean"},
								},
							},
						},
					},
				},
			},
			want: `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fatalExit(err error) {
	fmt.Println(err)
	os.Exit(3)
}

// callMcpTool is a reusable utility function for calling an MCP tool
func callMcpTool[I any, O any](ctx context.Context, clientSession *mcp.ClientSession, toolName string, input I) (O, error) {
	var output O

	if err := ctx.Err(); err != nil {
		return output, err
	}

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: input})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return output, err
		}
		fatalExit(fmt.Errorf("error calling tool %s: %w", toolName, err))
	}

	if len(result.Content) != 1 {
		fatalExit(fmt.Errorf("expected 1 content part from tool %s, got %d", toolName, len(result.Content)))
	}

	var textContent string

	switch c := result.Content[0].(type) {
	case *mcp.TextContent:
		textContent = c.Text
	default:
		fatalExit(fmt.Errorf("unexpected content type returned from tool %s, cannot handle multimedia except text", toolName))
	}

	if result.IsError {
		return output, errors.New(textContent)
	}

	// If O is string, return raw text content directly
	if _, isString := any(output).(string); isString {
		return any(textContent).(O), nil
	}

	outputJson := []byte(textContent)

	if result.StructuredContent != nil {
		structuredContent, err := json.Marshal(result.StructuredContent)
		if err != nil {
			fatalExit(fmt.Errorf("could not marshal structured content: %w", err))
		}
		outputJson = structuredContent
	}

	if err := json.Unmarshal(outputJson, &output); err != nil {
		fatalExit(fmt.Errorf("could not unmarshal structured content json into output for tool %s: %w", toolName, err))
	}
	return output, nil
}

type PingOutput struct {
	Pong bool ` + "`json:\"pong\"`" + `
}

// Ping Ping the server
var Ping func(ctx context.Context) (PingOutput, error)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "cpe-code-mode", Version: "v1.0.0"}, nil)

	// my-server server
	myServerSessionCmd := exec.Command("my-mcp")
	myServerSessionCmd.Env = append(os.Environ(), "API_KEY"+"="+"secret123")
	myServerSessionTransport := &mcp.CommandTransport{Command: myServerSessionCmd}
	myServerSession, err := mcpClient.Connect(ctx, myServerSessionTransport, nil)
	if err != nil {
		fatalExit(fmt.Errorf("could not connect to my-server server: %w", err))
	}
	defer myServerSession.Close()

	// Initialize tool functions
	Ping = func(ctx context.Context) (PingOutput, error) {
		return callMcpTool[struct{}, PingOutput](ctx, myServerSession, "ping", struct{}{})
	}

	err = Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
`,
		},
		{
			name: "http server with headers",
			servers: []ServerToolsInfo{
				{
					ServerName: "api",
					Config: mcpcpe.ServerConfig{
						Type: "http",
						URL:  "https://api.example.com/mcp",
						Headers: map[string]string{
							"Authorization": "Bearer token123",
						},
					},
					Tools: []*mcp.Tool{
						{
							Name:         "fetch_data",
							Description:  "Fetch data from API",
							InputSchema:  map[string]any{},
							OutputSchema: nil,
						},
					},
				},
			},
			want: `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fatalExit(err error) {
	fmt.Println(err)
	os.Exit(3)
}

type headerRoundTripper struct {
	headers map[string]string
	next    http.RoundTripper
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for name, value := range h.headers {
		req.Header.Set(name, value)
	}
	if h.next == nil {
		h.next = http.DefaultTransport
	}
	return h.next.RoundTrip(req)
}

// callMcpTool is a reusable utility function for calling an MCP tool
func callMcpTool[I any, O any](ctx context.Context, clientSession *mcp.ClientSession, toolName string, input I) (O, error) {
	var output O

	if err := ctx.Err(); err != nil {
		return output, err
	}

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: input})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return output, err
		}
		fatalExit(fmt.Errorf("error calling tool %s: %w", toolName, err))
	}

	if len(result.Content) != 1 {
		fatalExit(fmt.Errorf("expected 1 content part from tool %s, got %d", toolName, len(result.Content)))
	}

	var textContent string

	switch c := result.Content[0].(type) {
	case *mcp.TextContent:
		textContent = c.Text
	default:
		fatalExit(fmt.Errorf("unexpected content type returned from tool %s, cannot handle multimedia except text", toolName))
	}

	if result.IsError {
		return output, errors.New(textContent)
	}

	// If O is string, return raw text content directly
	if _, isString := any(output).(string); isString {
		return any(textContent).(O), nil
	}

	outputJson := []byte(textContent)

	if result.StructuredContent != nil {
		structuredContent, err := json.Marshal(result.StructuredContent)
		if err != nil {
			fatalExit(fmt.Errorf("could not marshal structured content: %w", err))
		}
		outputJson = structuredContent
	}

	if err := json.Unmarshal(outputJson, &output); err != nil {
		fatalExit(fmt.Errorf("could not unmarshal structured content json into output for tool %s: %w", toolName, err))
	}
	return output, nil
}

type FetchDataOutput = string

// FetchData Fetch data from API
var FetchData func(ctx context.Context) (FetchDataOutput, error)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "cpe-code-mode", Version: "v1.0.0"}, nil)

	// api server
	apiSessionHttpClient := &http.Client{
		Transport: &headerRoundTripper{
			headers: map[string]string{
				"Authorization": "Bearer token123",
			},
		},
	}
	apiSessionTransport := &mcp.StreamableClientTransport{
		Endpoint:   "https://api.example.com/mcp",
		HTTPClient: apiSessionHttpClient,
	}
	apiSession, err := mcpClient.Connect(ctx, apiSessionTransport, nil)
	if err != nil {
		fatalExit(fmt.Errorf("could not connect to api server: %w", err))
	}
	defer apiSession.Close()

	// Initialize tool functions
	FetchData = func(ctx context.Context) (FetchDataOutput, error) {
		return callMcpTool[struct{}, FetchDataOutput](ctx, apiSession, "fetch_data", struct{}{})
	}

	err = Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
`,
		},
		{
			name: "sse server without headers",
			servers: []ServerToolsInfo{
				{
					ServerName: "events",
					Config: mcpcpe.ServerConfig{
						Type: "sse",
						URL:  "https://events.example.com/sse",
					},
					Tools: []*mcp.Tool{
						{
							Name:        "subscribe",
							Description: "Subscribe to events",
							InputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"topic": map[string]any{"type": "string"},
								},
							},
							OutputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"id": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
			want: `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fatalExit(err error) {
	fmt.Println(err)
	os.Exit(3)
}

// callMcpTool is a reusable utility function for calling an MCP tool
func callMcpTool[I any, O any](ctx context.Context, clientSession *mcp.ClientSession, toolName string, input I) (O, error) {
	var output O

	if err := ctx.Err(); err != nil {
		return output, err
	}

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: input})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return output, err
		}
		fatalExit(fmt.Errorf("error calling tool %s: %w", toolName, err))
	}

	if len(result.Content) != 1 {
		fatalExit(fmt.Errorf("expected 1 content part from tool %s, got %d", toolName, len(result.Content)))
	}

	var textContent string

	switch c := result.Content[0].(type) {
	case *mcp.TextContent:
		textContent = c.Text
	default:
		fatalExit(fmt.Errorf("unexpected content type returned from tool %s, cannot handle multimedia except text", toolName))
	}

	if result.IsError {
		return output, errors.New(textContent)
	}

	// If O is string, return raw text content directly
	if _, isString := any(output).(string); isString {
		return any(textContent).(O), nil
	}

	outputJson := []byte(textContent)

	if result.StructuredContent != nil {
		structuredContent, err := json.Marshal(result.StructuredContent)
		if err != nil {
			fatalExit(fmt.Errorf("could not marshal structured content: %w", err))
		}
		outputJson = structuredContent
	}

	if err := json.Unmarshal(outputJson, &output); err != nil {
		fatalExit(fmt.Errorf("could not unmarshal structured content json into output for tool %s: %w", toolName, err))
	}
	return output, nil
}

type SubscribeInput struct {
	Topic string ` + "`json:\"topic\"`" + `
}

type SubscribeOutput struct {
	Id string ` + "`json:\"id\"`" + `
}

// Subscribe Subscribe to events
var Subscribe func(ctx context.Context, input SubscribeInput) (SubscribeOutput, error)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "cpe-code-mode", Version: "v1.0.0"}, nil)

	// events server
	eventsSessionTransport := &mcp.SSEClientTransport{Endpoint: "https://events.example.com/sse"}
	eventsSession, err := mcpClient.Connect(ctx, eventsSessionTransport, nil)
	if err != nil {
		fatalExit(fmt.Errorf("could not connect to events server: %w", err))
	}
	defer eventsSession.Close()

	// Initialize tool functions
	Subscribe = func(ctx context.Context, input SubscribeInput) (SubscribeOutput, error) {
		return callMcpTool[SubscribeInput, SubscribeOutput](ctx, eventsSession, "subscribe", input)
	}

	err = Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
`,
		},
		{
			name: "multiple http servers with different headers",
			servers: []ServerToolsInfo{
				{
					ServerName: "api-one",
					Config: mcpcpe.ServerConfig{
						Type: "http",
						URL:  "https://one.example.com/mcp",
						Headers: map[string]string{
							"X-Api-Key": "key-one",
						},
					},
					Tools: []*mcp.Tool{
						{
							Name:         "action_one",
							InputSchema:  map[string]any{},
							OutputSchema: nil,
						},
					},
				},
				{
					ServerName: "api-two",
					Config: mcpcpe.ServerConfig{
						Type: "http",
						URL:  "https://two.example.com/mcp",
						Headers: map[string]string{
							"Authorization": "Bearer token-two",
						},
					},
					Tools: []*mcp.Tool{
						{
							Name:         "action_two",
							InputSchema:  map[string]any{},
							OutputSchema: nil,
						},
					},
				},
			},
			want: `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fatalExit(err error) {
	fmt.Println(err)
	os.Exit(3)
}

type headerRoundTripper struct {
	headers map[string]string
	next    http.RoundTripper
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for name, value := range h.headers {
		req.Header.Set(name, value)
	}
	if h.next == nil {
		h.next = http.DefaultTransport
	}
	return h.next.RoundTrip(req)
}

// callMcpTool is a reusable utility function for calling an MCP tool
func callMcpTool[I any, O any](ctx context.Context, clientSession *mcp.ClientSession, toolName string, input I) (O, error) {
	var output O

	if err := ctx.Err(); err != nil {
		return output, err
	}

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: input})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return output, err
		}
		fatalExit(fmt.Errorf("error calling tool %s: %w", toolName, err))
	}

	if len(result.Content) != 1 {
		fatalExit(fmt.Errorf("expected 1 content part from tool %s, got %d", toolName, len(result.Content)))
	}

	var textContent string

	switch c := result.Content[0].(type) {
	case *mcp.TextContent:
		textContent = c.Text
	default:
		fatalExit(fmt.Errorf("unexpected content type returned from tool %s, cannot handle multimedia except text", toolName))
	}

	if result.IsError {
		return output, errors.New(textContent)
	}

	// If O is string, return raw text content directly
	if _, isString := any(output).(string); isString {
		return any(textContent).(O), nil
	}

	outputJson := []byte(textContent)

	if result.StructuredContent != nil {
		structuredContent, err := json.Marshal(result.StructuredContent)
		if err != nil {
			fatalExit(fmt.Errorf("could not marshal structured content: %w", err))
		}
		outputJson = structuredContent
	}

	if err := json.Unmarshal(outputJson, &output); err != nil {
		fatalExit(fmt.Errorf("could not unmarshal structured content json into output for tool %s: %w", toolName, err))
	}
	return output, nil
}

type ActionOneOutput = string

type ActionTwoOutput = string

var ActionOne func(ctx context.Context) (ActionOneOutput, error)

var ActionTwo func(ctx context.Context) (ActionTwoOutput, error)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "cpe-code-mode", Version: "v1.0.0"}, nil)

	// api-one server
	apiOneSessionHttpClient := &http.Client{
		Transport: &headerRoundTripper{
			headers: map[string]string{
				"X-Api-Key": "key-one",
			},
		},
	}
	apiOneSessionTransport := &mcp.StreamableClientTransport{
		Endpoint:   "https://one.example.com/mcp",
		HTTPClient: apiOneSessionHttpClient,
	}
	apiOneSession, err := mcpClient.Connect(ctx, apiOneSessionTransport, nil)
	if err != nil {
		fatalExit(fmt.Errorf("could not connect to api-one server: %w", err))
	}
	defer apiOneSession.Close()

	// api-two server
	apiTwoSessionHttpClient := &http.Client{
		Transport: &headerRoundTripper{
			headers: map[string]string{
				"Authorization": "Bearer token-two",
			},
		},
	}
	apiTwoSessionTransport := &mcp.StreamableClientTransport{
		Endpoint:   "https://two.example.com/mcp",
		HTTPClient: apiTwoSessionHttpClient,
	}
	apiTwoSession, err := mcpClient.Connect(ctx, apiTwoSessionTransport, nil)
	if err != nil {
		fatalExit(fmt.Errorf("could not connect to api-two server: %w", err))
	}
	defer apiTwoSession.Close()

	// Initialize tool functions
	ActionOne = func(ctx context.Context) (ActionOneOutput, error) {
		return callMcpTool[struct{}, ActionOneOutput](ctx, apiOneSession, "action_one", struct{}{})
	}
	ActionTwo = func(ctx context.Context) (ActionTwoOutput, error) {
		return callMcpTool[struct{}, ActionTwoOutput](ctx, apiTwoSession, "action_two", struct{}{})
	}

	err = Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
`,
		},
		{
			name: "server with no tools",
			servers: []ServerToolsInfo{
				{
					ServerName: "empty",
					Config: mcpcpe.ServerConfig{
						Type:    "stdio",
						Command: "empty-mcp",
					},
					Tools: []*mcp.Tool{},
				},
			},
			want: `package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fatalExit(err error) {
	fmt.Println(err)
	os.Exit(3)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "cpe-code-mode", Version: "v1.0.0"}, nil)

	// empty server
	emptySessionCmd := exec.Command("empty-mcp")
	emptySessionTransport := &mcp.CommandTransport{Command: emptySessionCmd}
	emptySession, err := mcpClient.Connect(ctx, emptySessionTransport, nil)
	if err != nil {
		fatalExit(fmt.Errorf("could not connect to empty server: %w", err))
	}
	defer emptySession.Close()

	err = Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateMainGo(tt.servers)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateMainGo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GenerateMainGo() mismatch\n=== GOT ===\n%s\n=== WANT ===\n%s", got, tt.want)
			}
		})
	}
}

func TestGenerateMainGo_DeterministicOutput(t *testing.T) {
	servers := []ServerToolsInfo{
		{
			ServerName: "zebra",
			Config: mcpcpe.ServerConfig{
				Type:    "stdio",
				Command: "zebra-mcp",
			},
			Tools: []*mcp.Tool{
				{
					Name:         "z_tool",
					InputSchema:  map[string]any{},
					OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"z": map[string]any{"type": "string"}}},
				},
			},
		},
		{
			ServerName: "alpha",
			Config: mcpcpe.ServerConfig{
				Type:    "stdio",
				Command: "alpha-mcp",
			},
			Tools: []*mcp.Tool{
				{
					Name:         "a_tool",
					InputSchema:  map[string]any{},
					OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}},
				},
			},
		},
	}

	first, err := GenerateMainGo(servers)
	if err != nil {
		t.Fatalf("GenerateMainGo() error = %v", err)
	}

	for i := 0; i < 5; i++ {
		got, err := GenerateMainGo(servers)
		if err != nil {
			t.Fatalf("GenerateMainGo() iteration %d error = %v", i, err)
		}
		if got != first {
			t.Errorf("GenerateMainGo() output not deterministic on iteration %d\n=== FIRST ===\n%s\n=== GOT ===\n%s", i, first, got)
		}
	}
}

func TestHasInputSchema(t *testing.T) {
	tests := []struct {
		name     string
		tool     *mcp.Tool
		expected bool
	}{
		{
			name:     "nil input schema",
			tool:     &mcp.Tool{InputSchema: nil},
			expected: false,
		},
		{
			name:     "empty map input schema",
			tool:     &mcp.Tool{InputSchema: map[string]any{}},
			expected: false,
		},
		{
			name: "empty properties",
			tool: &mcp.Tool{
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			expected: false,
		},
		{
			name: "with properties",
			tool: &mcp.Tool{
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasInputSchema(tt.tool)
			if got != tt.expected {
				t.Errorf("hasInputSchema() = %v, want %v", got, tt.expected)
			}
		})
	}
}

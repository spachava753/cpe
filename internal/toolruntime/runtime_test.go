package toolruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/plugins"
)

type emptyMCP struct{}

func (emptyMCP) Servers() []mcpclient.ServerID          { return nil }
func (emptyMCP) Instructions(mcpclient.ServerID) string { return "" }
func (emptyMCP) Load(context.Context, mcpclient.ServerID) ([]*mcp.Tool, error) {
	return nil, errors.New("no MCP server")
}
func (emptyMCP) Call(context.Context, mcpclient.ServerID, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return nil, errors.New("no MCP server")
}

type runnerFunc func(context.Context, dyson.Command) (dyson.CommandResult, error)

func (f runnerFunc) RunCommand(ctx context.Context, command dyson.Command) (dyson.CommandResult, error) {
	return f(
		ctx,
		command,
	)
}

func hostFixture(t *testing.T, tools MCPTools, runner dyson.CommandRunner, configure ...func(*Config)) *toolHost {
	t.Helper()
	config := Config{
		Cwd: "/work",
		MCP: tools,
	}
	for _, option := range configure {
		option(&config)
	}
	host, err := New(
		t.Context(),
		config,
		dyson.StdlibConfig{
			FS:            afero.NewMemMapFs(),
			CommandRunner: runner,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := host.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return host.(*toolHost)
}

func toolRequest(name, key, value string) gaiagent.ToolRequest {
	return gaiagent.ToolRequest{
		Block: gai.Block{
			ID: "call-1",
		},
		Call: gai.ToolCallInput{
			Name: name,
			Parameters: map[string]any{
				key: value,
			},
		},
	}
}

func TestLocalHost(t *testing.T) {
	for _, test := range []struct {
		name string
		code string
		want string
		bash bool
	}{
		{
			name: "host environment process and working directory",
			code: "load('os.star', 'os')\nprint(os.getenv('CPE_HOST_TEST'))\nprint(os.getpid() > 0)\nprint(os.getcwd() != '')",
			want: "host-access\nTrue\nTrue",
		},
		{
			name: "bash inherits updated host environment",
			code: "printf '%s' \"$CPE_HOST_TEST\"",
			want: "host-access",
			bash: true,
		},
		{
			name: "subprocess inherits updated host environment",
			code: "load('subprocess.star', 'subprocess')\nprint(subprocess.run(['sh', '-c', 'printf %s \"$CPE_HOST_TEST\"'], capture_output=True).stdout.decode())",
			want: "host-access",
		},
		{
			name: "subprocess explicit environment overrides host",
			code: "load('subprocess.star', 'subprocess')\nprint(subprocess.run(['sh', '-c', 'printf %s \"$CPE_HOST_TEST\"'], capture_output=True, env={'CPE_HOST_TEST': 'explicit'}).stdout.decode())",
			want: "explicit",
		},
		{
			name: "subprocess empty environment does not inherit host",
			code: "load('subprocess.star', 'subprocess')\nprint(subprocess.run(['sh', '-c', 'printf %s \"${CPE_HOST_TEST-unset}\"'], capture_output=True, env={}).stdout.decode())",
			want: "unset",
		},
		{
			name: "filesystem access outside session directory",
			code: "load('os.star', 'os')\nos.mkdir('../outside')\nprint(os.path.isdir('../outside'))",
			want: "True",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Setenv(
					"CPE_HOST_TEST",
					"before-host-creation",
				)
				cwd := filepath.Join(
					t.TempDir(),
					"work",
				)
				filesystem := afero.NewOsFs()
				if err := filesystem.MkdirAll(
					cwd,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				host, err := Local(
					t.Context(),
					Config{
						Cwd: cwd,
						MCP: emptyMCP{},
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := host.Close(context.Background()); err != nil {
						t.Error(err)
					}
				})
				setup, err := host.(*toolHost).execute(
					t.Context(),
					toolRequest(
						starlarkToolName,
						"code",
						"load('os.star', 'os')\nos.putenv('CPE_HOST_TEST', 'host-access')",
					),
				)
				if err != nil || setup.ToolResultError {
					t.Fatalf(
						"set host environment: %v %v",
						setup,
						err,
					)
				}
				name, parameter := starlarkToolName, "code"
				if test.bash {
					name, parameter = bashToolName, "command"
				}
				result, err := host.(*toolHost).execute(
					t.Context(),
					toolRequest(
						name,
						parameter,
						test.code,
					),
				)
				if err != nil {
					t.Fatal(err)
				}
				var output strings.Builder
				for _, block := range result.Blocks {
					output.WriteString(block.Content.String())
				}
				if result.ToolResultError || !strings.Contains(
					output.String(),
					test.want,
				) {
					t.Fatalf(
						"host execution: %q",
						output.String(),
					)
				}
			},
		)
	}
}

func TestToolHost(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   string
		want   string
		failed bool
	}{
		{
			name: "persistent globals",
			code: "print(value + 1)",
			want: "42",
		},
		{
			name:   "syntax error is tool result",
			code:   "this is invalid",
			want:   "Execution error",
			failed: true,
		},
		{
			name:   "partial output survives error",
			code:   "print('before')\nfail('boom')",
			want:   "before",
			failed: true,
		},
		{
			name: "output is bounded",
			code: "print('x' * 200000)",
			want: "[output truncated]",
		},
		{
			name:   "error output is bounded",
			code:   "fail('x' * 200000)",
			want:   "[output truncated]",
			failed: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				host := hostFixture(
					t,
					emptyMCP{},
					runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) { return dyson.CommandResult{}, nil }),
				)
				initial, err := host.execute(
					t.Context(),
					toolRequest(
						"starlark_repl",
						"code",
						"value = 41",
					),
				)
				if err != nil || initial.ToolResultError {
					t.Fatalf(
						"initialize: %v %v",
						initial,
						err,
					)
				}
				result, err := host.execute(
					t.Context(),
					toolRequest(
						"starlark_repl",
						"code",
						test.code,
					),
				)
				if err != nil {
					t.Fatal(err)
				}
				var output strings.Builder
				for _, block := range result.Blocks {
					output.WriteString(block.Content.String())
				}
				if result.ToolResultError != test.failed || !strings.Contains(
					output.String(),
					test.want,
				) {
					t.Fatalf(
						"result: %v %q",
						result.ToolResultError,
						output.String(),
					)
				}
				if len(output.String()) > maxTextBytes+1024 {
					t.Fatal("unbounded output")
				}
			},
		)
	}
}

func TestCommandPaths(t *testing.T) {
	for _, test := range []struct {
		name        string
		request     gaiagent.ToolRequest
		largeOutput bool
	}{
		{
			name: "stderr survives large stdout",
			request: toolRequest(
				"bash",
				"command",
				"build",
			),
			largeOutput: true,
		},
		{
			name: "direct bash",
			request: toolRequest(
				"bash",
				"command",
				"printf hello",
			),
		},
		{
			name: "Starlark subprocess",
			request: toolRequest(
				"starlark_repl",
				"code",
				"load('subprocess.star', 'subprocess')\nr = subprocess.run(['printf', 'hello'], capture_output=True)\nprint(r.stdout.decode())",
			),
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				calls := 0
				host := hostFixture(
					t,
					emptyMCP{},
					runnerFunc(func(_ context.Context, command dyson.Command) (dyson.CommandResult, error) {
						calls++
						if command.Stdin == dyson.StreamInherit || command.Stdout == dyson.StreamInherit || command.Stderr == dyson.StreamInherit {
							t.Error("child inherited ACP streams")
						}
						if command.Dir != "/work" {
							t.Errorf(
								"cwd: %q",
								command.Dir,
							)
						}
						if test.largeOutput {
							return dyson.CommandResult{
								Stdout: []byte(strings.Repeat(
									"x",
									maxTextBytes,
								)),
								Stderr:     []byte("fatal: build failed"),
								ReturnCode: 1,
							}, nil
						}
						return dyson.CommandResult{
							Stdout: []byte("hello"),
						}, nil
					}),
				)
				result, err := host.execute(
					t.Context(),
					test.request,
				)
				if err != nil || result.ToolResultError != test.largeOutput || calls != 1 {
					t.Fatalf(
						"execute: %#v, %v, calls=%d",
						result,
						err,
						calls,
					)
				}
				if test.largeOutput {
					var output strings.Builder
					for _, block := range result.Blocks {
						output.WriteString(block.Content.String())
					}
					if !strings.Contains(
						output.String(),
						"fatal: build failed",
					) || !strings.Contains(
						output.String(),
						"exit_code: 1",
					) {
						t.Fatal("stderr or exit status lost behind stdout")
					}
				}
			},
		)
	}
}

func TestMCPEmission(t *testing.T) {
	imageData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		emit          bool
		toolError     bool
		protocolError bool
	}{
		{
			name: "raw results stay private",
		},
		{
			name: "explicit image",
			emit: true,
		},
		{
			name:      "tool error remains inspectable",
			emit:      true,
			toolError: true,
		},
		{
			name:          "protocol error becomes failed execution",
			protocolError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				server := mcp.NewServer(
					&mcp.Implementation{
						Name:    "fixture",
						Version: "1",
					},
					nil,
				)
				server.AddTool(
					&mcp.Tool{
						Name:        "screenshot",
						InputSchema: json.RawMessage(`{"type":"object"}`),
					},
					func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						if test.protocolError {
							return nil, errors.New("transport-side failure")
						}
						return &mcp.CallToolResult{
							Content: []mcp.Content{
								&mcp.ImageContent{
									Data:     imageData,
									MIMEType: "image/png",
								},
							},
							StructuredContent: map[string]any{
								"count": 7,
							},
							IsError: test.toolError,
						}, nil
					},
				)
				registry, err := mcpclient.NewRegistry(
					func(ctx context.Context, _ plugins.MCPServer) (*mcp.ClientSession, error) {
						a, b := mcp.NewInMemoryTransports()
						session, err := server.Connect(
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
					},
					map[mcpclient.ServerID]plugins.MCPServer{
						"test": {},
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = registry.Close() })
				host := hostFixture(
					t,
					registry,
					runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) { return dyson.CommandResult{}, nil }),
				)
				code := "r = mcp__test__screenshot({})\nprint(r.structured_content['count'])\nprint(r.is_error)"
				if test.emit {
					code += "\nemit(r.content[0])"
				}
				result, err := host.execute(
					t.Context(),
					toolRequest(
						"starlark_repl",
						"code",
						code,
					),
				)
				if err != nil {
					t.Fatal(err)
				}
				if result.ToolResultError != test.protocolError {
					t.Fatalf(
						"unexpected failed result: %#v",
						result,
					)
				}
				images := 0
				for _, block := range result.Blocks {
					if block.ModalityType == gai.Image {
						images++
					}
				}
				if (images == 1) != test.emit {
					t.Fatalf(
						"images: %d",
						images,
					)
				}
			},
		)
	}
}

func TestHostCancellation(t *testing.T) {
	host := hostFixture(
		t,
		emptyMCP{},
		runnerFunc(func(ctx context.Context, _ dyson.Command) (dyson.CommandResult, error) {
			<-ctx.Done()
			return dyson.CommandResult{}, ctx.Err()
		}),
	)
	ctx, cancel := context.WithTimeout(
		t.Context(),
		20*time.Millisecond,
	)
	defer cancel()
	result, err := host.execute(
		ctx,
		toolRequest(
			"starlark_repl",
			"code",
			"load('subprocess.star', 'subprocess')\nsubprocess.run(['sleep', '100'])",
		),
	)
	if err != nil || !result.ToolResultError {
		t.Fatalf(
			"cancelled execution: %#v %v",
			result,
			err,
		)
	}
	result, err = host.execute(
		t.Context(),
		toolRequest(
			"starlark_repl",
			"code",
			"print('still alive')",
		),
	)
	if err != nil || result.ToolResultError {
		t.Fatalf(
			"next invocation: %#v %v",
			result,
			err,
		)
	}
}

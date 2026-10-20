package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/toolruntime"
)

type generatorFunc func(context.Context, gai.GenerationRequest) (gai.Response, error)

func (f generatorFunc) Generate(ctx context.Context, request gai.GenerationRequest) (gai.Response, error) {
	return f(
		ctx,
		request,
	)
}

type testAuth struct{}

func (testAuth) Authenticate(context.Context, auth.Provider) error { return nil }
func (testAuth) Logout(context.Context) error                      { return nil }

type testClient struct {
	t       *testing.T
	mu      sync.Mutex
	updates []acp.SessionNotification
}

func (c *testClient) Update(_ context.Context, notification *acp.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(
		c.updates,
		*notification,
	)
	return nil
}
func (c *testClient) RequestPermission(context.Context, *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	c.t.Error("agent requested execution approval")
	return nil, errors.New("execution approval is not supported")
}

type agentFixture struct {
	client         *acp.Client
	store          conversation.Store
	cancel         context.CancelFunc
	done           chan error
	disconnect     func() error
	commandStarted chan struct{}
	notifications  *testClient
}

func serverFixture(t *testing.T, generator GeneratorFactory, configure ...func(*Config, *Dependencies, *acp.InitializeRequest)) agentFixture {
	t.Helper()
	filesystem := afero.NewMemMapFs()
	if err := filesystem.MkdirAll(
		"/work",
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	store, err := conversation.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := Config{
		Models: map[models.ID]models.Model{
			"one": {
				ID: "one",
				InputModalities: []gai.Modality{
					gai.Text,
					gai.Image,
				},
				ReasoningLevels: []string{
					"high",
				},
			},
			"two": {
				ID: "two",
				InputModalities: []gai.Modality{
					gai.Text,
				},
				ReasoningLevels: []string{
					"low",
				},
			},
		},
		Initial: models.Selection{
			Model: "one",
		},
		Instructions:       "test agent",
		TurnTimeout:        5 * time.Second,
		MaxGenerations:     4,
		MaxResponseRetries: 1,
	}
	commandStarted := make(
		chan struct{},
		1,
	)
	deps := Dependencies{
		Filesystem:     filesystem,
		Authentication: testAuth{},
		Generators:     generator,
		Hosts: func(ctx context.Context, config toolruntime.Config) (toolruntime.Host, error) {
			return toolruntime.New(
				ctx,
				config,
				dyson.StdlibConfig{
					FS: filesystem,
					CommandRunner: runnerFunc(func(ctx context.Context, command dyson.Command) (dyson.CommandResult, error) {
						if len(command.Args) > 0 && command.Args[0] == "block" {
							commandStarted <- struct{}{}
							<-ctx.Done()
							return dyson.CommandResult{}, ctx.Err()
						}
						return dyson.CommandResult{}, nil
					}),
				},
			)
		},
		MCP:     mcpclient.Connect,
		History: store,
		Traces:  noop.NewTracerProvider(),
		Stderr:  io.Discard,
	}
	initialize := &acp.InitializeRequest{
		ProtocolVersion: 1,
	}
	for _, option := range configure {
		option(
			&config,
			&deps,
			initialize,
		)
	}
	ctx, cancel := context.WithCancel(t.Context())
	a, b := acp.NewInMemoryTransports()
	done := make(
		chan error,
		1,
	)
	go func() {
		done <- Serve(
			ctx,
			b,
			config,
			deps,
		)
	}()
	transport := &capturedTransport{
		Transport: a,
	}
	notifications := &testClient{
		t: t,
	}
	client, err := acp.Connect(
		t.Context(),
		transport,
		notifications,
	)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server did not stop")
		}
	})
	_, err = client.Initialize(
		t.Context(),
		initialize,
	)
	if err != nil {
		t.Fatal(err)
	}
	return agentFixture{
		client:         client,
		store:          deps.History,
		cancel:         cancel,
		done:           done,
		disconnect:     transport.connection.Close,
		commandStarted: commandStarted,
		notifications:  notifications,
	}
}

func scriptGenerator(_ context.Context, _ models.Model, _ models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
	return generatorFunc(func(_ context.Context, request gai.GenerationRequest) (gai.Response, error) {
		last := request.Dialog[len(request.Dialog)-1]
		if last.Role == gai.ToolResult {
			return gai.Response{
				Candidates: []gai.Message{
					{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							gai.TextBlock("done"),
						},
					},
				},
				FinishReason: gai.EndTurn,
			}, nil
		}
		block, err := gai.ToolCallBlock(
			fmt.Sprintf(
				"call-%d",
				len(request.Dialog),
			),
			"starlark_repl",
			map[string]any{
				"code": last.Blocks[0].Content.String(),
			},
		)
		if err != nil {
			return gai.Response{}, err
		}
		return gai.Response{
			Candidates: []gai.Message{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						block,
					},
				},
			},
			FinishReason: gai.ToolUse,
		}, nil
	}), gai.GenerationOptions{}, nil
}

func openFixtureSession(t *testing.T, client *acp.Client) acp.SessionId {
	t.Helper()
	response, err := client.NewSession(
		t.Context(),
		&acp.NewSessionRequest{
			Cwd: "/work",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return response.SessionID
}

func promptFixture(t *testing.T, client *acp.Client, id acp.SessionId, text string) {
	t.Helper()
	response, err := client.Prompt(
		t.Context(),
		&acp.PromptRequest{
			SessionID: id,
			Prompt: []acp.ContentBlock{
				acp.TextContentBlock(text),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != acp.StopReasonEndTurn {
		t.Fatalf(
			"stop: %s",
			response.StopReason,
		)
	}
}

func lastToolOutput(record conversation.Record) string {
	for i := range slices.Backward(record.Dialog) {
		if record.Dialog[i].Role == gai.ToolResult {
			var output strings.Builder
			for _, block := range record.Dialog[i].Blocks {
				output.WriteString(block.Content.String())
			}
			return output.String()
		}
	}
	return ""
}

func TestSessionLifecycle(t *testing.T) {
	for _, operation := range []string{
		"live session",
		"model switch",
		"fork",
		"load",
		"resume",
		"delete",
		"isolated sessions",
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				requests := make(
					chan gai.GenerationRequest,
					16,
				)
				fixture := serverFixture(
					t,
					func(ctx context.Context, model models.Model, reasoning models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						generator, options, err := scriptGenerator(
							ctx,
							model,
							reasoning,
						)
						return generatorFunc(func(ctx context.Context, request gai.GenerationRequest) (gai.Response, error) {
							requests <- request
							return generator.Generate(
								ctx,
								request,
							)
						}), options, err
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				promptFixture(
					t,
					fixture.client,
					id,
					"value = 41",
				)
				for range 2 {
					request := <-requests
					for _, message := range request.Dialog {
						if message.Blocks[0].Content.String() == runtimeResetNotice {
							t.Fatal("new session was described as a runtime reset")
						}
					}
				}
				target := id
				want := "42"
				switch operation {
				case "model switch":
					_, err := fixture.client.SetSessionConfigOption(
						t.Context(),
						&acp.SetSessionConfigOptionRequest{
							SessionID: id,
							ConfigID:  "model",
							Value:     "two",
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					_, err = fixture.client.SetSessionConfigOption(
						t.Context(),
						&acp.SetSessionConfigOptionRequest{
							SessionID: id,
							ConfigID:  "reasoning",
							Value:     "high",
						},
					)
					if err == nil {
						t.Fatal("accepted unsupported reasoning effort")
					}
					_, err = fixture.client.SetSessionConfigOption(
						t.Context(),
						&acp.SetSessionConfigOptionRequest{
							SessionID: id,
							ConfigID:  "reasoning",
							Value:     "low",
						},
					)
					if err != nil {
						t.Fatal(err)
					}
				case "fork":
					fork, err := fixture.client.ForkSession(
						t.Context(),
						&acp.ForkSessionRequest{
							SessionID: id,
							Cwd:       "/work",
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					target = fork.SessionID
					want = "Execution error"
				case "load", "resume":
					if _, err := fixture.client.CloseSession(
						t.Context(),
						&acp.CloseSessionRequest{
							SessionID: id,
						},
					); err != nil {
						t.Fatal(err)
					}
					var err error
					if operation == "load" {
						_, err = fixture.client.LoadSession(
							t.Context(),
							&acp.LoadSessionRequest{
								SessionID: id,
								Cwd:       "/work",
							},
						)
					} else {
						_, err = fixture.client.ResumeSession(
							t.Context(),
							&acp.ResumeSessionRequest{
								SessionID: id,
								Cwd:       "/work",
							},
						)
					}
					if err != nil {
						t.Fatal(err)
					}
					want = "Execution error"
				case "isolated sessions":
					target = openFixtureSession(
						t,
						fixture.client,
					)
					want = "Execution error"
				case "delete":
					if _, err := fixture.client.DeleteSession(
						t.Context(),
						&acp.DeleteSessionRequest{
							SessionID: id,
						},
					); err != nil {
						t.Fatal(err)
					}
					listed, err := fixture.client.ListSessions(
						t.Context(),
						&acp.ListSessionsRequest{},
					)
					if err != nil || len(listed.Sessions) != 0 {
						t.Fatalf(
							"list: %v %v",
							listed,
							err,
						)
					}
					return
				}
				promptFixture(
					t,
					fixture.client,
					target,
					"print(value + 1)",
				)
				record, err := fixture.store.Load(
					t.Context(),
					conversation.ID(target),
				)
				if err != nil {
					t.Fatal(err)
				}
				if output := lastToolOutput(record); !strings.Contains(
					output,
					want,
				) {
					t.Fatalf(
						"output %q, want %q",
						output,
						want,
					)
				}
				if operation == "model switch" && (record.Options.Model != "two" || record.Options.Reasoning != "low") {
					t.Fatalf(
						"options: %#v",
						record.Options,
					)
				}
				// Reconstructed state must survive later turns and model changes.
				promptFixture(
					t,
					fixture.client,
					target,
					"value = 10",
				)
				if _, err := fixture.client.SetSessionConfigOption(
					t.Context(),
					&acp.SetSessionConfigOptionRequest{
						SessionID: target,
						ConfigID:  "model",
						Value:     "two",
					},
				); err != nil {
					t.Fatal(err)
				}
				promptFixture(
					t,
					fixture.client,
					target,
					"value += 1\nprint(value)",
				)
				for range 6 {
					request := <-requests
					notices := 0
					for index, message := range request.Dialog {
						if message.Blocks[0].Content.String() == runtimeResetNotice {
							notices++
							if index != 4 || message.Role != gai.User || request.Dialog[index+1].Blocks[0].Content.String() != "print(value + 1)" {
								t.Fatal("reset notice moved away from the restoration boundary")
							}
						}
					}
					wantNotices := 0
					switch operation {
					case "load", "resume", "fork":
						wantNotices = 1
					}
					if notices != wantNotices {
						t.Fatalf(
							"reset notices: got %d, want %d",
							notices,
							wantNotices,
						)
					}
				}
				record, err = fixture.store.Load(
					t.Context(),
					conversation.ID(target),
				)
				if err != nil || lastToolOutput(record) != "11\n" {
					t.Fatalf(
						"reconstructed runtime state did not persist: %#v, %v",
						record,
						err,
					)
				}
				for _, message := range record.Dialog {
					if message.Blocks[0].Content.String() == runtimeResetNotice {
						t.Fatal("model-only notice was saved in the transcript")
					}
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				fixture.notifications.mu.Lock()
				defer fixture.notifications.mu.Unlock()
				updates, err := json.Marshal(fixture.notifications.updates)
				if err != nil || strings.Contains(
					string(updates),
					runtimeResetNotice,
				) {
					t.Fatal("model-only notice was sent to the ACP client")
				}
			},
		)
	}
}

func TestSessionCancellation(t *testing.T) {
	for _, mode := range []string{
		"cancel turn",
		"disconnect",
		"cancel server",
		"concurrent sessions",
	} {
		t.Run(
			mode,
			func(t *testing.T) {
				entered := make(
					chan struct{},
					2,
				)
				generator := func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
					return generatorFunc(func(ctx context.Context, request gai.GenerationRequest) (gai.Response, error) {
						last := request.Dialog[len(request.Dialog)-1]
						if last.Blocks[0].Content.String() == "wait" {
							entered <- struct{}{}
							<-ctx.Done()
							return gai.Response{}, ctx.Err()
						}
						return gai.Response{
							Candidates: []gai.Message{
								{
									Role: gai.Assistant,
									Blocks: []gai.Block{
										gai.TextBlock("ready"),
									},
								},
							},
							FinishReason: gai.EndTurn,
						}, nil
					}), gai.GenerationOptions{}, nil
				}
				fixture := serverFixture(
					t,
					generator,
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				finished := make(
					chan error,
					1,
				)
				go func() {
					response, err := fixture.client.Prompt(
						t.Context(),
						&acp.PromptRequest{
							SessionID: id,
							Prompt: []acp.ContentBlock{
								acp.TextContentBlock("wait"),
							},
						},
					)
					if err == nil && response.StopReason != acp.StopReasonCancelled {
						err = fmt.Errorf(
							"stop: %s",
							response.StopReason,
						)
					}
					finished <- err
				}()
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("generation did not start")
				}
				_, err := fixture.client.SetSessionConfigOption(
					t.Context(),
					&acp.SetSessionConfigOptionRequest{
						SessionID: id,
						ConfigID:  "model",
						Value:     "two",
					},
				)
				if err == nil {
					t.Fatal("configuration changed during active turn")
				}
				switch mode {
				case "disconnect":
					_ = fixture.disconnect()
				case "cancel server":
					fixture.cancel()
				case "concurrent sessions":
					other := openFixtureSession(
						t,
						fixture.client,
					)
					promptFixture(
						t,
						fixture.client,
						other,
						"hello",
					)
					fallthrough
				default:
					if err := fixture.client.Cancel(
						t.Context(),
						&acp.CancelNotification{
							SessionID: id,
						},
					); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case err := <-finished:
					if mode != "disconnect" && mode != "cancel server" && err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("prompt did not cancel promptly")
				}
				if mode == "cancel turn" {
					promptFixture(
						t,
						fixture.client,
						id,
						"hello",
					)
				}
			},
		)
	}
}

func TestInterruptedToolHistory(t *testing.T) {
	block, err := gai.ToolCallBlock(
		"effect",
		"bash",
		map[string]any{
			"command": "touch file",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	settled := settleDialog(gai.Dialog{
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				block,
			},
		},
	})
	if len(settled) != 2 || !settled[1].ToolResultError || settled[1].Blocks[0].ID != "effect" {
		t.Fatalf(
			"unsettled dialog: %#v",
			settled,
		)
	}
	if len(settleDialog(settled)) != 2 {
		t.Fatal("duplicated tool result")
	}
}

type runnerFunc func(context.Context, dyson.Command) (dyson.CommandResult, error)

func (f runnerFunc) RunCommand(ctx context.Context, command dyson.Command) (dyson.CommandResult, error) {
	return f(
		ctx,
		command,
	)
}

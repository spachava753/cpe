package agent

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"

	"github.com/spachava753/cpe/internal/conversation"
)

func TestSessionKVCheckpoint(t *testing.T) {
	for _, operation := range []string{
		"script failure",
		"cancellation",
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				directory := t.TempDir()
				history, err := conversation.NewStore(directory)
				if err != nil {
					t.Fatal(err)
				}
				fixture := serverFixture(
					t,
					scriptGenerator,
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						deps.History = history
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				code := "kv.store('value', ({'count': 42}, b'bytes'))\n"
				if operation == "script failure" {
					promptFixture(
						t,
						fixture.client,
						id,
						code+"fail('later error')",
					)
				} else {
					finished := make(
						chan error,
						1,
					)
					go func() {
						_, err := fixture.client.Prompt(
							t.Context(),
							&acp.PromptRequest{
								SessionID: id,
								Prompt: []acp.ContentBlock{
									acp.TextContentBlock(code + "load('subprocess.star', 'subprocess')\nsubprocess.run(['block'])"),
								},
							},
						)
						finished <- err
					}()
					select {
					case <-fixture.commandStarted:
					case <-time.After(2 * time.Second):
						t.Fatal("script did not reach command after store")
					}
					// Inspect disk while the invocation is still running: the KV write must
					// already be durable, together with its tool intent but not its result.
					record, err := history.Load(
						t.Context(),
						conversation.ID(id),
					)
					if err != nil || len(record.KV["value"]) == 0 || len(record.Dialog) != 2 {
						t.Fatalf(
							"store was not checkpointed before returning: %#v, %v",
							record,
							err,
						)
					}
					if err := fixture.client.Cancel(
						t.Context(),
						&acp.CancelNotification{
							SessionID: id,
						},
					); err != nil {
						t.Fatal(err)
					}
					select {
					case err := <-finished:
						if err != nil {
							t.Fatal(err)
						}
					case <-time.After(2 * time.Second):
						t.Fatal("script did not cancel")
					}
				}
				if _, err := fixture.client.CloseSession(
					t.Context(),
					&acp.CloseSessionRequest{
						SessionID: id,
					},
				); err != nil {
					t.Fatal(err)
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				// Recreate both the server and file store; no live runtime data is shared.
				reopened, err := conversation.NewStore(directory)
				if err != nil {
					t.Fatal(err)
				}
				restored := serverFixture(
					t,
					scriptGenerator,
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						deps.History = reopened
					},
				)
				if _, err := restored.client.ResumeSession(
					t.Context(),
					&acp.ResumeSessionRequest{
						SessionID: id,
						Cwd:       "/work",
					},
				); err != nil {
					t.Fatal(err)
				}
				promptFixture(
					t,
					restored.client,
					id,
					"print(kv.get('value') == ({'count': 42}, b'bytes'))",
				)
				record, err := reopened.Load(
					t.Context(),
					conversation.ID(id),
				)
				if err != nil || lastToolOutput(record) != "True\n" {
					t.Fatalf(
						"KV lost across server restart: %#v, %v",
						record,
						err,
					)
				}
			},
		)
	}
}

func TestSessionKVStore(t *testing.T) {
	for _, outcome := range []string{
		"success",
		"save failure",
		"canceled context",
	} {
		t.Run(
			outcome,
			func(t *testing.T) {
				history, err := conversation.NewStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				s := &session{
					deps: Dependencies{
						History: history,
					},
					record: conversation.Record{
						Info: conversation.Info{
							ID: "kv-test",
						},
					},
				}
				if err := s.Store(
					t.Context(),
					"value",
					[]byte("old"),
				); err != nil {
					t.Fatal(err)
				}
				snapshot, err := s.snapshot()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				switch outcome {
				case "save failure":
					s.deps.History = &failingCheckpointStore{
						Store: history,
					}
				case "canceled context":
					cancel()
				}
				value := []byte("new")
				err = s.Store(
					ctx,
					"value",
					value,
				)
				want := "new"
				if outcome != "success" {
					want = "old"
					if err == nil {
						t.Fatal("failed write succeeded")
					}
					if outcome == "canceled context" && !errors.Is(
						err,
						context.Canceled,
					) {
						t.Fatal(err)
					}
					if outcome == "save failure" && !strings.Contains(
						err.Error(),
						"checkpoint unavailable",
					) {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				value[0] = '!'
				got, err := s.Get(
					t.Context(),
					"value",
				)
				if err != nil || string(got) != want {
					t.Fatalf(
						"live value: %q, %v; want %q",
						got,
						err,
						want,
					)
				}
				got[0] = '!'
				again, err := s.Get(
					t.Context(),
					"value",
				)
				if err != nil || string(again) != want {
					t.Fatal("Get did not return owned bytes")
				}
				saved, err := history.Load(
					t.Context(),
					s.record.Info.ID,
				)
				if err != nil || string(saved.KV["value"]) != want {
					t.Fatal("saved value differs from live value")
				}
				if !bytes.Equal(
					snapshot.KV["value"],
					[]byte("old"),
				) {
					t.Fatal("write mutated existing snapshot")
				}
				keys, err := s.Keys(t.Context())
				if err != nil || !slices.Equal(
					keys,
					[]string{
						"value",
					},
				) {
					t.Fatal("unexpected keys")
				}
			},
		)
	}
}

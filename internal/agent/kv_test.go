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
		"overflow",
		"overflow before cancellation",
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				directory := t.TempDir()
				history, storeErr := conversation.NewStore(directory)
				if storeErr != nil {
					t.Fatal(storeErr)
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
				if strings.HasPrefix(
					operation,
					"overflow",
				) {
					code += "print('x' * 200000 + 'final summary')\n"
				}
				if operation == "script failure" || operation == "overflow" {
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
				reopened, storeErr := conversation.NewStore(directory)
				if storeErr != nil {
					t.Fatal(storeErr)
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
				read := "print(kv.get('value') == ({'count': 42}, b'bytes'))"
				if strings.HasPrefix(
					operation,
					"overflow",
				) {
					read = "keys = [key for key in kv.keys() if key.startswith('overflow_')]\nprint(len(keys) == 1 and kv.get(keys[0]).startswith('x' * 200000 + 'final summary\\n'))"
				}
				promptFixture(
					t,
					restored.client,
					id,
					read,
				)
				record, storeErr := reopened.Load(
					t.Context(),
					conversation.ID(id),
				)
				if storeErr != nil || lastToolOutput(record) != "True\n" {
					t.Fatalf(
						"KV lost across server restart: %#v, %v",
						record,
						storeErr,
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
				history, storeErr := conversation.NewStore(t.TempDir())
				if storeErr != nil {
					t.Fatal(storeErr)
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
				snapshot, storeErr := s.snapshot()
				if storeErr != nil {
					t.Fatal(storeErr)
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
				storeErr = s.Store(
					ctx,
					"value",
					value,
				)
				want := "new"
				if outcome != "success" {
					want = "old"
					if storeErr == nil {
						t.Fatal("failed write succeeded")
					}
					if outcome == "canceled context" && !errors.Is(
						storeErr,
						context.Canceled,
					) {
						t.Fatal(storeErr)
					}
					if outcome == "save failure" && !strings.Contains(
						storeErr.Error(),
						"checkpoint unavailable",
					) {
						t.Fatal(storeErr)
					}
				} else if storeErr != nil {
					t.Fatal(storeErr)
				}
				value[0] = '!'
				got, storeErr := s.Get(
					t.Context(),
					"value",
				)
				if storeErr != nil || string(got) != want {
					t.Fatalf(
						"live value: %q, %v; want %q",
						got,
						storeErr,
						want,
					)
				}
				got[0] = '!'
				again, storeErr := s.Get(
					t.Context(),
					"value",
				)
				if storeErr != nil || string(again) != want {
					t.Fatal("Get did not return owned bytes")
				}
				saved, storeErr := history.Load(
					t.Context(),
					s.record.Info.ID,
				)
				if storeErr != nil || string(saved.KV["value"]) != want {
					t.Fatal("saved value differs from live value")
				}
				if !bytes.Equal(
					snapshot.KV["value"],
					[]byte("old"),
				) {
					t.Fatal("write mutated existing snapshot")
				}
				keys, storeErr := s.Keys(t.Context())
				if storeErr != nil || !slices.Equal(
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

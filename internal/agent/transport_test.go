package agent

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"

	"github.com/spachava753/cpe/internal/conversation"
)

type capturedTransport struct {
	acp.Transport
	connection acp.Connection
}

func (t *capturedTransport) Connect(ctx context.Context) (acp.Connection, error) {
	connection, err := t.Transport.Connect(ctx)
	t.connection = connection
	return connection, err
}

func TestSessionToolCancellation(t *testing.T) {
	fixture := serverFixture(
		t,
		scriptGenerator,
	)
	id := openFixtureSession(
		t,
		fixture.client,
	)
	done := make(
		chan error,
		1,
	)
	go func() {
		_, err := fixture.client.Prompt(
			t.Context(),
			&acp.PromptRequest{
				SessionID: id,
				Prompt: []acp.ContentBlock{
					acp.TextContentBlock("load('subprocess.star', 'subprocess')\nsubprocess.run(['block'])"),
				},
			},
		)
		done <- err
	}()
	select {
	case <-fixture.commandStarted:
	case <-time.After(time.Second):
		t.Fatal("command did not start")
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
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("tool did not cancel")
	}
	promptFixture(
		t,
		fixture.client,
		id,
		"print('next turn')",
	)
	record, err := fixture.store.Load(
		t.Context(),
		conversation.ID(id),
	)
	if err != nil {
		t.Fatal(err)
	}
	if output := lastToolOutput(record); output != "next turn" && output != "next turn\n" {
		t.Fatalf(
			"output: %q",
			output,
		)
	}
	select {
	case <-fixture.commandStarted:
		t.Fatal("replayed interrupted command")
	default:
	}
}

type failedWriteConnection struct {
	acp.Connection
}

func (failedWriteConnection) Write(ctx context.Context, _ jsonrpc.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return io.ErrClosedPipe
}

func TestConnectionWrite(t *testing.T) {
	for _, cancelled := range []bool{
		true,
		false,
	} {
		t.Run(
			map[bool]string{
				true:  "cancelled request preserves sessions",
				false: "broken transport cancels sessions",
			}[cancelled],
			func(t *testing.T) {
				fixture := serverFixture(
					t,
					scriptGenerator,
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				connectionCancelled := false
				connection := &lifetimeConnection{
					Connection: failedWriteConnection{},
					cancel:     func() { connectionCancelled = true; fixture.cancel() },
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if cancelled {
					cancel()
				}
				if err := connection.Write(
					ctx,
					nil,
				); err == nil {
					t.Fatal("expected write error")
				}
				if connectionCancelled == cancelled {
					t.Fatalf(
						"connection cancelled: %v",
						connectionCancelled,
					)
				}
				if cancelled {
					promptFixture(
						t,
						fixture.client,
						id,
						"print('alive')",
					)
				}
			},
		)
	}
}

func TestServeIdleCancellation(t *testing.T) {
	fixture := serverFixture(
		t,
		scriptGenerator,
	)
	fixture.cancel()
	// Cleanup joins Serve with a deadline; no request is needed to unblock Read.
}

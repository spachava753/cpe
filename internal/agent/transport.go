package agent

import (
	"context"
	"sync"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// The SDK drains handlers before returning from RunAgent. Cancel session work
// at the transport boundary, rather than waiting for RunAgent to return.
type lifetimeTransport struct {
	base     acp.Transport
	lifetime context.Context
	cancel   context.CancelFunc
}

//nolint:contextcheck // The serving lifetime, not the SDK's detached handler context, owns this connection.
func (t lifetimeTransport) Connect(context.Context) (acp.Connection, error) {
	connection, err := t.base.Connect(t.lifetime)
	if err != nil {
		return nil, err
	}
	watched := &lifetimeConnection{
		Connection: connection,
		cancel:     t.cancel,
	}
	context.AfterFunc(
		t.lifetime,
		func() { _ = watched.Close() },
	)
	return watched, nil
}

type lifetimeConnection struct {
	acp.Connection
	err    error
	cancel context.CancelFunc
	once   sync.Once
}

func (c *lifetimeConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	message, err := c.Connection.Read(ctx)
	if err != nil {
		c.cancel()
	}
	return message, err
}

func (c *lifetimeConnection) Write(ctx context.Context, message jsonrpc.Message) error {
	err := c.Connection.Write(
		ctx,
		message,
	)
	// Request cancellation is not transport failure; other sessions stay alive.
	if err != nil && ctx.Err() == nil {
		c.cancel()
	}
	return err
}

func (c *lifetimeConnection) Close() error {
	c.cancel()
	c.once.Do(func() { c.err = c.Connection.Close() })
	return c.err
}

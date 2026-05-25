package acp

import (
	"context"
	"io"
	"os"

	"github.com/coder/acp-go-sdk"
	"github.com/spachava753/cpe/internal/sync"
)

type ServeOptions struct {
	Stdout     io.Writer
	Stderr     io.Writer
	ConfigPath string
}

func Serve(ctx context.Context, opts ServeOptions) error {
	// create db
	// load config to support multiple model profile picking
	// likely should store config and create runtime per session based on init and model options
	// should also make incognito a new session config
	ag := Agent{
		activeSessions: new(sync.Map[acp.SessionId, sync.Guard[session]]),
	}
	asc := acp.NewAgentSideConnection(&ag, os.Stdout, os.Stdin)
	ag.conn = asc
	<-asc.Done()
	return nil
}

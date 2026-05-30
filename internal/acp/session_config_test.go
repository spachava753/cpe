package acp

import (
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/nalgeon/be"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

func TestSessionConfig(t *testing.T) {
	clientConn, store := setup(t, &noOpAcpClient{}, &config.RawConfig{}, unreachableRuntimeFactory)

	// seed the db
	sessionEntries := []storage.CreateACPSessionParams{
		{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir",
				SessionId: "abc123",
			},
			LastMessageID: "",
			ModelRef:      "gpt-5.5",
			ThinkingLevel: "low",
		},
		{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir2",
				SessionId: "123abc",
			},
			LastMessageID: "",
			ModelRef:      "gpt-5.4-mini",
			ThinkingLevel: "xhigh",
		},
	}
	for _, se := range sessionEntries {
		be.Err(t, store.CreateACPSession(t.Context(), se), nil)
	}

	_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: false,
		},
		ClientInfo: &acp.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acp.ProtocolVersionNumber,
	})
	t.Log("called init")
	// we should not get an error on init connection
	be.Err(t, err, nil)

	// TODO: we should assert the order as well, as the order returned
	// will be based on most recent acp session first and descendng
	resp, err := clientConn.ListSessions(t.Context(), acp.ListSessionsRequest{})
	be.Err(t, err, nil)
	be.Equal(t, len(resp.Sessions), len(sessionEntries))
}

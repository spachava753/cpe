package acp

import (
	"context"
	"log/slog"

	"github.com/spachava753/acp-sdk/acp"

	cpelogging "github.com/spachava753/cpe/internal/logging"
)

func withSessionLogAttrs(ctx context.Context, sessionID acp.SessionId, cwd string) context.Context {
	attrs := make([]slog.Attr, 0, 2)
	if sessionID != "" {
		attrs = append(attrs, slog.String("session_id", string(sessionID)))
	}
	if cwd != "" {
		attrs = append(attrs, slog.String("cwd", cwd))
	}
	return cpelogging.WithAttrs(ctx, attrs...)
}

package textedit

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xctx"
)

const toolDescription = "Create a new text file or replace exactly one occurrence of text in an existing UTF-8 file."

type sessionUpdator interface {
	SessionUpdate(ctx context.Context, params *acp.SessionNotification) error
}

// MakeTool returns the text_edit tool definition and callback for direct gai registration.
func MakeTool(sessionId acp.SessionId, conn sessionUpdator) (gai.Tool, gai.ToolCallback) {
	tool := gai.Tool{
		Name:        ToolName,
		Description: toolDescription,
		InputSchema: inputSchema(),
	}
	callback := gai.ToolCallBackFunc[Input](func(ctx context.Context, input Input) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", gai.CallbackExecErr{Err: err}
		}

		// send in progress update for toolcall
		inProgress := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(ctx))
		inProgress.Kind = new(acp.ToolKindEdit)
		inProgress.Status = new(acp.ToolCallStatusInProgress)
		conn.SessionUpdate(ctx, &acp.SessionNotification{
			SessionID: sessionId,
			Update:    inProgress,
		})

		output, err := Apply(input)
		if err != nil {
			// send failed update for toolcall
			failed := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(ctx))
			failed.Status = new(acp.ToolCallStatusFailed)
			conn.SessionUpdate(ctx, &acp.SessionNotification{
				SessionID: sessionId,
				Update:    failed,
			})

			return "", err
		}

		// send completed update for toolcall
		completed := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(ctx))
		completed.Kind = new(acp.ToolKindEdit)
		completed.Status = new(acp.ToolCallStatusCompleted)
		diffContent := acp.DiffToolCallContent(input.Path, input.NewText)
		diffContent.OldText = &input.OldText
		completed.Content = []acp.ToolCallContent{diffContent}
		conn.SessionUpdate(ctx, &acp.SessionNotification{
			SessionID: sessionId,
			Update:    completed,
		})

		return output.Message(), nil
	})
	return tool, callback
}

func inputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"path": {
				Type:        "string",
				Description: "Path to the file to edit or create.",
			},
			"old_text": {
				Type:        "string",
				Description: "Exact text to find and replace. If empty, creates a new file instead.",
			},
			"new_text": {
				Type:        "string",
				Description: "Replacement text or content for new file.",
			},
		},
		Required: []string{"path", "new_text"},
	}
}

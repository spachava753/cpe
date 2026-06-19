package client

import (
	"bytes"
	"context"
	"testing"

	"github.com/nalgeon/be"
	"github.com/spachava753/acp-sdk/acp"
)

func TestHandlerUpdate(t *testing.T) {
	t.Run("prints message chunks", func(t *testing.T) {
		var out bytes.Buffer
		h := handler{out: &out}

		err := h.Update(t.Context(), &acp.SessionNotification{
			SessionID: "test-session",
			Update:    acp.AgentMessageChunkSessionUpdate(acp.TextContentBlock("hello")),
		})
		be.Err(t, err, nil)
		be.Equal(t, out.String(), "hello")
	})

	t.Run("prints thought chunks with marker", func(t *testing.T) {
		var out bytes.Buffer
		h := handler{out: &out}

		err := h.Update(t.Context(), &acp.SessionNotification{
			SessionID: "test-session",
			Update:    acp.AgentThoughtChunkSessionUpdate(acp.TextContentBlock("consider options")),
		})
		be.Err(t, err, nil)
		be.Equal(t, out.String(), "\n[thought] consider options\n")
	})

	t.Run("prints tool call", func(t *testing.T) {
		var out bytes.Buffer
		h := handler{out: &out}

		err := h.Update(t.Context(), &acp.SessionNotification{
			SessionID: "test-session",
			Update:    acp.ToolCallSessionUpdate("tool-call-1", "execute_go_code"),
		})
		be.Err(t, err, nil)
		be.Equal(t, out.String(), "\n[tool: execute_go_code]\n")
	})

	t.Run("prints tool update with status and raw output", func(t *testing.T) {
		var out bytes.Buffer
		h := handler{out: &out}
		status := acp.ToolCallStatusCompleted
		title := "execute_go_code"

		err := h.Update(context.Background(), &acp.SessionNotification{
			SessionID: "test-session",
			Update: acp.SessionUpdate{
				SessionUpdate: acp.SessionUpdateTypeToolCallUpdate,
				ToolCallID:    "tool-call-1",
				Title:         &title,
				Status:        &status,
				RawOutput: map[string]any{
					"ok": true,
				},
			},
		})
		be.Err(t, err, nil)
		be.Equal(t, out.String(), "\n[tool update: tool-call-1 | execute_go_code | completed]\n{\n  \"ok\": true\n}\n")
	})

	t.Run("prints plan update", func(t *testing.T) {
		var out bytes.Buffer
		h := handler{out: &out}

		err := h.Update(t.Context(), &acp.SessionNotification{
			SessionID: "test-session",
			Update: acp.PlanSessionUpdate([]acp.PlanEntry{
				{Status: acp.PlanEntryStatusPending, Content: "inspect code"},
				{Status: acp.PlanEntryStatusInProgress, Content: "write tests"},
			}),
		})
		be.Err(t, err, nil)
		be.Equal(t, out.String(), "\n[plan: pending] inspect code\n\n[plan: in_progress] write tests\n")
	})

	t.Run("prints usage update", func(t *testing.T) {
		var out bytes.Buffer
		h := handler{out: &out}

		err := h.Update(t.Context(), &acp.SessionNotification{
			SessionID: "test-session",
			Update:    acp.UsageUpdateSessionUpdate(42, 100),
		})
		be.Err(t, err, nil)
		be.Equal(t, out.String(), "\n[usage: 42/100]\n")
	})
}

func TestContentText(t *testing.T) {
	t.Run("text block", func(t *testing.T) {
		be.Equal(t, contentText(acp.TextContentBlock("hello")), "hello")
	})

	t.Run("decoded text block", func(t *testing.T) {
		be.Equal(t, contentText(map[string]any{"type": "text", "text": "hello"}), "hello")
	})

	t.Run("image block placeholder", func(t *testing.T) {
		be.Equal(t, contentText(acp.ImageContentBlock("data", "image/png")), "[image content]")
	})

	t.Run("resource link", func(t *testing.T) {
		block := acp.ResourceLinkContentBlock("README", "file:///tmp/README.md")
		be.Equal(t, contentText(block), "[README](file:///tmp/README.md)")
	})

	t.Run("fallback marshals unknown content", func(t *testing.T) {
		be.Equal(t, contentText(map[string]any{"text": "hello"}), "{\n  \"text\": \"hello\"\n}")
	})
}

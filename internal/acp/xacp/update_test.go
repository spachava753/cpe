package xacp

import (
	"reflect"
	"slices"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/spachava753/gai"
)

func mustToolCallBlock(t *testing.T, id, name string, params map[string]any) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(id, name, params)
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}
	return block
}

func TestMsgToSessionUpdate(t *testing.T) {
	toolCall := mustToolCallBlock(t, "call-1", "lookup", map[string]any{"query": "docs"})

	secondToolCall := mustToolCallBlock(t, "call-2", "read", map[string]any{"path": "README.md"})

	tests := []struct {
		name      string
		msg       gai.Message
		want      []acpsdk.SessionUpdate
		wantPanic bool
	}{
		{
			name: "user text",
			msg: gai.Message{
				Role:   gai.User,
				Blocks: []gai.Block{gai.TextBlock("hello")},
			},
			want: []acpsdk.SessionUpdate{{UserMessageChunk: &acpsdk.SessionUpdateUserMessageChunk{
				Content: acpsdk.TextBlock("hello"),
			}}},
		},
		{
			name: "user multiple blocks",
			msg: gai.Message{Role: gai.User, Blocks: []gai.Block{
				gai.TextBlock("hello"),
				{BlockType: gai.Content, ModalityType: gai.Image, MimeType: "image/png", Content: gai.Str("iVBORw0KGgo=")},
			}},
			want: []acpsdk.SessionUpdate{
				{UserMessageChunk: &acpsdk.SessionUpdateUserMessageChunk{Content: acpsdk.TextBlock("hello")}},
				{UserMessageChunk: &acpsdk.SessionUpdateUserMessageChunk{Content: acpsdk.ImageBlock("iVBORw0KGgo=", "image/png")}},
			},
		},
		{
			name: "assistant text",
			msg: gai.Message{
				Role:   gai.Assistant,
				Blocks: []gai.Block{gai.TextBlock("answer")},
			},
			want: []acpsdk.SessionUpdate{{AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
				Content: acpsdk.TextBlock("answer"),
			}}},
		},
		{
			name: "assistant thought",
			msg: gai.Message{Role: gai.Assistant, Blocks: []gai.Block{{
				BlockType:    gai.Thinking,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("reasoning"),
			}}},
			want: []acpsdk.SessionUpdate{{AgentThoughtChunk: &acpsdk.SessionUpdateAgentThoughtChunk{
				Content: acpsdk.TextBlock("reasoning"),
			}}},
		},
		{
			name: "assistant tool call",
			msg:  gai.Message{Role: gai.Assistant, Blocks: []gai.Block{toolCall}},
			want: []acpsdk.SessionUpdate{{ToolCall: &acpsdk.SessionUpdateToolCall{
				RawInput:   map[string]any{"query": "docs"},
				Status:     acpsdk.ToolCallStatusPending,
				Title:      "lookup",
				ToolCallId: "call-1",
			}}},
		},
		{
			name: "assistant thinking then multiple tool calls",
			msg: gai.Message{Role: gai.Assistant, Blocks: []gai.Block{
				{BlockType: gai.Thinking, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str("first thought")},
				{BlockType: gai.Thinking, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str("second thought")},
				toolCall,
				secondToolCall,
			}},
			want: []acpsdk.SessionUpdate{
				{AgentThoughtChunk: &acpsdk.SessionUpdateAgentThoughtChunk{Content: acpsdk.TextBlock("first thought")}},
				{AgentThoughtChunk: &acpsdk.SessionUpdateAgentThoughtChunk{Content: acpsdk.TextBlock("second thought")}},
				{ToolCall: &acpsdk.SessionUpdateToolCall{
					RawInput:   map[string]any{"query": "docs"},
					Status:     acpsdk.ToolCallStatusPending,
					Title:      "lookup",
					ToolCallId: "call-1",
				}},
				{ToolCall: &acpsdk.SessionUpdateToolCall{
					RawInput:   map[string]any{"path": "README.md"},
					Status:     acpsdk.ToolCallStatusPending,
					Title:      "read",
					ToolCallId: "call-2",
				}},
			},
		},
		{
			name: "malformed assistant tool call panics",
			msg: gai.Message{Role: gai.Assistant, Blocks: []gai.Block{{
				ID:           "call-bad",
				BlockType:    gai.ToolCall,
				ModalityType: gai.Text,
				Content:      gai.Str("not-json"),
			}}},
			wantPanic: true,
		},
		{
			name: "successful tool result with consecutive matching ids",
			msg: gai.Message{Role: gai.ToolResult, Blocks: []gai.Block{
				{ID: "call-2", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("result")},
				{ID: "call-2", BlockType: gai.Content, ModalityType: gai.Image, MimeType: "image/png", Content: gai.Str("iVBORw0KGgo=")},
			}},
			want: []acpsdk.SessionUpdate{
				{ToolCallUpdate: &acpsdk.SessionToolCallUpdate{
					Content:    []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock("result"))},
					Status:     new(acpsdk.ToolCallStatusCompleted),
					ToolCallId: "call-2",
				}},
				{ToolCallUpdate: &acpsdk.SessionToolCallUpdate{
					Content:    []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.ImageBlock("iVBORw0KGgo=", "image/png"))},
					Status:     new(acpsdk.ToolCallStatusCompleted),
					ToolCallId: "call-2",
				}},
			},
		},
		{
			name: "tool result with multiple tool call ids",
			msg: gai.Message{Role: gai.ToolResult, Blocks: []gai.Block{
				{ID: "call-1", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("lookup result")},
				{ID: "call-2", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("read result")},
			}},
			want: []acpsdk.SessionUpdate{
				{ToolCallUpdate: &acpsdk.SessionToolCallUpdate{
					Content:    []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock("lookup result"))},
					Status:     new(acpsdk.ToolCallStatusCompleted),
					ToolCallId: "call-1",
				}},
				{ToolCallUpdate: &acpsdk.SessionToolCallUpdate{
					Content:    []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock("read result"))},
					Status:     new(acpsdk.ToolCallStatusCompleted),
					ToolCallId: "call-2",
				}},
			},
		},
		{
			name: "failed tool result",
			msg: gai.Message{
				Role:            gai.ToolResult,
				Blocks:          []gai.Block{{ID: "call-3", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("boom")}},
				ToolResultError: true,
			},
			want: []acpsdk.SessionUpdate{{ToolCallUpdate: &acpsdk.SessionToolCallUpdate{
				Content:    []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock("boom"))},
				Status:     new(acpsdk.ToolCallStatusFailed),
				ToolCallId: "call-3",
			}}},
		},
		{
			name: "unsupported modality becomes text",
			msg: gai.Message{Role: gai.User, Blocks: []gai.Block{{
				BlockType:    gai.Content,
				ModalityType: gai.Video,
				MimeType:     "video/mp4",
				Content:      gai.Str("video-data"),
			}}},
			want: []acpsdk.SessionUpdate{{UserMessageChunk: &acpsdk.SessionUpdateUserMessageChunk{
				Content: acpsdk.TextBlock("video-data"),
			}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []acpsdk.SessionUpdate
			panicked := false
			func() {
				defer func() {
					if recover() != nil {
						panicked = true
					}
				}()
				got = slices.Collect(MsgToSessionUpdate(tt.msg))
			}()

			if panicked != tt.wantPanic {
				t.Fatalf("msgToSessionUpdate() panicked = %t, want %t", panicked, tt.wantPanic)
			}
			if tt.wantPanic {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("msgToSessionUpdate() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

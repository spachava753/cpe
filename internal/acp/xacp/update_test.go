package xacp

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	acpsdk "github.com/spachava753/acp-sdk/acp"
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

func toolCallSessionUpdate(id, title string, rawInput map[string]any) acpsdk.SessionUpdate {
	status := acpsdk.ToolCallStatusPending
	update := acpsdk.ToolCallSessionUpdate(acpsdk.ToolCallId(id), title)
	update.RawInput = rawInput
	update.Status = &status
	return update
}

func toolCallResultSessionUpdate(id string, status acpsdk.ToolCallStatus, content acpsdk.ContentBlock) acpsdk.SessionUpdate {
	update := acpsdk.ToolCallUpdateSessionUpdate(acpsdk.ToolCallId(id))
	update.Content = []acpsdk.ToolCallContent{acpsdk.ContentToolCallContent(content)}
	update.Status = &status
	return update
}

func TestEmptyTextSessionUpdateMarshalsTextField(t *testing.T) {
	updates := slices.Collect(MsgToSessionUpdate(gai.Message{
		Role:   gai.Assistant,
		Blocks: []gai.Block{gai.TextBlock("")},
	}))
	if len(updates) != 1 {
		t.Fatalf("len(updates) = %d, want 1", len(updates))
	}

	got, err := json.Marshal(updates[0])
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":""}}`
	if string(got) != want {
		t.Fatalf("json.Marshal() = %s, want %s", got, want)
	}
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
			want: []acpsdk.SessionUpdate{
				acpsdk.UserMessageChunkSessionUpdate(acpsdk.TextContentBlock("hello")),
			},
		},
		{
			name: "user multiple blocks",
			msg: gai.Message{Role: gai.User, Blocks: []gai.Block{
				gai.TextBlock("hello"),
				{BlockType: gai.Content, ModalityType: gai.Image, MimeType: "image/png", Content: gai.Str("iVBORw0KGgo=")},
			}},
			want: []acpsdk.SessionUpdate{
				acpsdk.UserMessageChunkSessionUpdate(acpsdk.TextContentBlock("hello")),
				acpsdk.UserMessageChunkSessionUpdate(acpsdk.ImageContentBlock("iVBORw0KGgo=", "image/png")),
			},
		},
		{
			name: "assistant text",
			msg: gai.Message{
				Role:   gai.Assistant,
				Blocks: []gai.Block{gai.TextBlock("answer")},
			},
			want: []acpsdk.SessionUpdate{
				acpsdk.AgentMessageChunkSessionUpdate(acpsdk.TextContentBlock("answer")),
			},
		},
		{
			name: "assistant thought",
			msg: gai.Message{Role: gai.Assistant, Blocks: []gai.Block{{
				BlockType:    gai.Thinking,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("reasoning"),
			}}},
			want: []acpsdk.SessionUpdate{
				acpsdk.AgentThoughtChunkSessionUpdate(acpsdk.TextContentBlock("reasoning")),
			},
		},
		{
			name: "assistant tool call",
			msg:  gai.Message{Role: gai.Assistant, Blocks: []gai.Block{toolCall}},
			want: []acpsdk.SessionUpdate{
				toolCallSessionUpdate("call-1", "lookup", map[string]any{"query": "docs"}),
			},
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
				acpsdk.AgentThoughtChunkSessionUpdate(acpsdk.TextContentBlock("first thought")),
				acpsdk.AgentThoughtChunkSessionUpdate(acpsdk.TextContentBlock("second thought")),
				toolCallSessionUpdate("call-1", "lookup", map[string]any{"query": "docs"}),
				toolCallSessionUpdate("call-2", "read", map[string]any{"path": "README.md"}),
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
				toolCallResultSessionUpdate("call-2", acpsdk.ToolCallStatusCompleted, acpsdk.TextContentBlock("result")),
				toolCallResultSessionUpdate("call-2", acpsdk.ToolCallStatusCompleted, acpsdk.ImageContentBlock("iVBORw0KGgo=", "image/png")),
			},
		},
		{
			name: "tool result with multiple tool call ids",
			msg: gai.Message{Role: gai.ToolResult, Blocks: []gai.Block{
				{ID: "call-1", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("lookup result")},
				{ID: "call-2", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("read result")},
			}},
			want: []acpsdk.SessionUpdate{
				toolCallResultSessionUpdate("call-1", acpsdk.ToolCallStatusCompleted, acpsdk.TextContentBlock("lookup result")),
				toolCallResultSessionUpdate("call-2", acpsdk.ToolCallStatusCompleted, acpsdk.TextContentBlock("read result")),
			},
		},
		{
			name: "failed tool result",
			msg: gai.Message{
				Role:            gai.ToolResult,
				Blocks:          []gai.Block{{ID: "call-3", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("boom")}},
				ToolResultError: true,
			},
			want: []acpsdk.SessionUpdate{
				toolCallResultSessionUpdate("call-3", acpsdk.ToolCallStatusFailed, acpsdk.TextContentBlock("boom")),
			},
		},
		{
			name: "unsupported modality becomes text",
			msg: gai.Message{Role: gai.User, Blocks: []gai.Block{{
				BlockType:    gai.Content,
				ModalityType: gai.Video,
				MimeType:     "video/mp4",
				Content:      gai.Str("video-data"),
			}}},
			want: []acpsdk.SessionUpdate{
				acpsdk.UserMessageChunkSessionUpdate(acpsdk.TextContentBlock("video-data")),
			},
		},
		{
			name: "empty assistant thought is preserved",
			msg: gai.Message{Role: gai.Assistant, Blocks: []gai.Block{
				{BlockType: gai.Thinking, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str("")},
				{BlockType: gai.Thinking, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str("kept thought")},
			}},
			want: []acpsdk.SessionUpdate{
				acpsdk.AgentThoughtChunkSessionUpdate(acpsdk.TextContentBlock("")),
				acpsdk.AgentThoughtChunkSessionUpdate(acpsdk.TextContentBlock("kept thought")),
			},
		},
		{
			name: "empty assistant content is preserved",
			msg: gai.Message{Role: gai.Assistant, Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("")},
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("answer")},
			}},
			want: []acpsdk.SessionUpdate{
				acpsdk.AgentMessageChunkSessionUpdate(acpsdk.TextContentBlock("")),
				acpsdk.AgentMessageChunkSessionUpdate(acpsdk.TextContentBlock("answer")),
			},
		},
		{
			name: "empty user text is preserved",
			msg: gai.Message{Role: gai.User, Blocks: []gai.Block{
				gai.TextBlock(""),
				gai.TextBlock("hello"),
			}},
			want: []acpsdk.SessionUpdate{
				acpsdk.UserMessageChunkSessionUpdate(acpsdk.TextContentBlock("")),
				acpsdk.UserMessageChunkSessionUpdate(acpsdk.TextContentBlock("hello")),
			},
		},
		{
			name: "empty image content is not skipped",
			msg: gai.Message{Role: gai.Assistant, Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Image, MimeType: "image/png", Content: gai.Str("")},
			}},
			want: []acpsdk.SessionUpdate{
				acpsdk.AgentMessageChunkSessionUpdate(acpsdk.ImageContentBlock("", "image/png")),
			},
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

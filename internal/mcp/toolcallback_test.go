package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xctx"
	"github.com/spachava753/cpe/internal/mcpconfig"
)

const testSessionID acp.SessionId = "session-1"

type recordingSessionUpdator struct {
	updates []acp.SessionNotification
	err     error
}

func (r *recordingSessionUpdator) SessionUpdate(ctx context.Context, params *acp.SessionNotification) error {
	r.updates = append(r.updates, *params)
	return r.err
}

var _ sessionUpdator = (*recordingSessionUpdator)(nil)

func newToolCallbackTestSession(t *testing.T, result *mcpsdk.CallToolResult, handlerErr error) *mcpsdk.ClientSession {
	t.Helper()

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test-server", Version: "test"}, nil)
	server.AddTool(&mcpsdk.Tool{
		Name:        "lookup",
		Description: "Look up test data.",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return result, handlerErr
	})

	serverCtx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(serverCtx, serverTransport)
	}()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Log("timed out waiting for test MCP server to exit")
		}
	})
	return session
}

func requireToolCallUpdate(t *testing.T, got acp.SessionNotification, wantID acp.ToolCallId, wantStatus acp.ToolCallStatus) acp.SessionUpdate {
	t.Helper()

	if got.SessionID != testSessionID {
		t.Fatalf("SessionId = %q, want %q", got.SessionID, testSessionID)
	}
	update := got.Update
	if update.SessionUpdate != acp.SessionUpdateTypeToolCallUpdate {
		t.Fatalf("SessionUpdate = %q, want %q in %#v", update.SessionUpdate, acp.SessionUpdateTypeToolCallUpdate, update)
	}
	if update.ToolCallID != wantID {
		t.Fatalf("ToolCallId = %q, want %q", update.ToolCallID, wantID)
	}
	if update.Status == nil || *update.Status != wantStatus {
		t.Fatalf("Status = %#v, want %q", update.Status, wantStatus)
	}
	return update
}

func requireToolResultText(t *testing.T, msg gai.Message, wantError bool) string {
	t.Helper()

	if msg.Role != gai.ToolResult {
		t.Fatalf("Role = %q, want %q", msg.Role, gai.ToolResult)
	}
	if msg.ToolResultError != wantError {
		t.Fatalf("ToolResultError = %t, want %t", msg.ToolResultError, wantError)
	}
	if len(msg.Blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1: %#v", len(msg.Blocks), msg.Blocks)
	}
	return msg.Blocks[0].Content.String()
}

func requireToolCallContentText(t *testing.T, update acp.SessionUpdate) string {
	t.Helper()

	content, ok := update.Content.([]acp.ToolCallContent)
	if !ok || len(content) != 1 || content[0].Type != acp.ToolCallContentTypeContent || content[0].Content.Type != acp.ContentBlockTypeText {
		t.Fatalf("tool call content = %#v, want one text content", update.Content)
	}
	return content[0].Content.Text
}

func requireText(t *testing.T, label, got, want string, contains bool) {
	t.Helper()

	if contains {
		if !strings.Contains(got, want) {
			t.Fatalf("%s = %q, want containing %q", label, got, want)
		}
		return
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", label, got, want)
	}
}

func TestToolCallback(t *testing.T) {
	t.Run("returns session update error", func(t *testing.T) {
		t.Parallel()

		updateErr := errors.New("session update failed")
		updator := &recordingSessionUpdator{err: updateErr}
		session := newToolCallbackTestSession(t, &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "must not be returned"}},
		}, nil)
		callback := NewToolCallback(updator, testSessionID, session, "test-server", "lookup", mcpconfig.ServerConfig{})
		toolCallID := acp.ToolCallId("call-update-error")
		_, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), map[string]any{"query": "docs"})
		if !errors.Is(err, updateErr) {
			t.Fatalf("Call() error = %v, want session update error", err)
		}
		if len(updator.updates) != 1 {
			t.Fatalf("SessionUpdate count = %d, want only in-progress attempt: %#v", len(updator.updates), updator.updates)
		}
		requireToolCallUpdate(t, updator.updates[0], toolCallID, acp.ToolCallStatusInProgress)
	})
	t.Run("forwards resource link", func(t *testing.T) {
		t.Parallel()

		resource := &mcpsdk.ResourceLink{
			URI:         "file:///workspace/guide.md",
			Name:        "guide.md",
			Title:       "Project guide",
			Description: "Instructions for this project",
			MIMEType:    "text/markdown",
			Size:        new(int64(42)),
			Meta:        mcpsdk.Meta{"source": "test-server"},
			Annotations: &mcpsdk.Annotations{
				Audience:     []mcpsdk.Role{mcpsdk.Role("assistant")},
				LastModified: "2026-07-18T12:00:00Z",
				Priority:     0.75,
			},
		}
		session := newToolCallbackTestSession(t, &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{resource},
		}, nil)
		updator := &recordingSessionUpdator{}
		callback := NewToolCallback(updator, testSessionID, session, "server", "lookup", mcpconfig.ServerConfig{})
		toolCallID := acp.ToolCallId("call-resource-link")

		msg, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), nil)
		if err != nil {
			t.Fatalf("Call() error = %v", err)
		}
		modelText := requireToolResultText(t, msg, false)
		if !strings.Contains(modelText, resource.Name) || !strings.Contains(modelText, resource.URI) {
			t.Fatalf("model result text = %q, want resource name and URI", modelText)
		}
		if len(updator.updates) != 2 {
			t.Fatalf("updates len = %d, want in-progress and completed: %#v", len(updator.updates), updator.updates)
		}
		final := requireToolCallUpdate(t, updator.updates[1], toolCallID, acp.ToolCallStatusCompleted)
		content, ok := final.Content.([]acp.ToolCallContent)
		if !ok || len(content) != 1 || content[0].Content.Type != acp.ContentBlockTypeResourceLink {
			t.Fatalf("final content = %#v, want one resource link", final.Content)
		}
		got := content[0].Content
		if got.URI == nil || *got.URI != resource.URI || got.Name != resource.Name {
			t.Fatalf("resource link identity = %#v, want URI %q and name %q", got, resource.URI, resource.Name)
		}
		if got.Title == nil || *got.Title != resource.Title || got.Description == nil || *got.Description != resource.Description {
			t.Fatalf("resource link labels = %#v, want title and description preserved", got)
		}
		if got.MimeType == nil || *got.MimeType != resource.MIMEType || got.Size == nil || *got.Size != *resource.Size {
			t.Fatalf("resource link metadata = %#v, want MIME type and size preserved", got)
		}
		if !reflect.DeepEqual(got.Meta, acp.Meta(resource.Meta)) {
			t.Fatalf("resource link _meta = %#v, want %#v", got.Meta, resource.Meta)
		}
		if got.Annotations == nil || got.Annotations.Audience == nil || !reflect.DeepEqual(*got.Annotations.Audience, []acp.Role{acp.RoleAssistant}) {
			t.Fatalf("resource link audience = %#v, want assistant", got.Annotations)
		}
		if got.Annotations.LastModified == nil || *got.Annotations.LastModified != resource.Annotations.LastModified {
			t.Fatalf("resource link last modified = %#v, want %q", got.Annotations, resource.Annotations.LastModified)
		}
		if got.Annotations.Priority == nil || *got.Annotations.Priority != resource.Annotations.Priority {
			t.Fatalf("resource link priority = %#v, want %v", got.Annotations, resource.Annotations.Priority)
		}
	})
	t.Run("forwards embedded", func(t *testing.T) {
		t.Run("text resource", func(t *testing.T) {
			t.Parallel()

			resource := &mcpsdk.ResourceContents{
				URI:      "file:///workspace/guide.md",
				MIMEType: "text/markdown",
				Text:     "# Project guide",
				Meta:     mcpsdk.Meta{"revision": 3.0},
			}
			embedded := &mcpsdk.EmbeddedResource{
				Resource: resource,
				Meta:     mcpsdk.Meta{"source": "test-server"},
				Annotations: &mcpsdk.Annotations{
					Audience:     []mcpsdk.Role{mcpsdk.Role("user")},
					LastModified: "2026-07-18T13:00:00Z",
					Priority:     0.5,
				},
			}
			session := newToolCallbackTestSession(t, &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{embedded},
			}, nil)
			updator := &recordingSessionUpdator{}
			callback := NewToolCallback(updator, testSessionID, session, "server", "lookup", mcpconfig.ServerConfig{})
			toolCallID := acp.ToolCallId("call-embedded-text")

			msg, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), nil)
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if got := requireToolResultText(t, msg, false); got != resource.Text {
				t.Fatalf("model result text = %q, want %q", got, resource.Text)
			}
			if len(updator.updates) != 2 {
				t.Fatalf("updates len = %d, want in-progress and completed: %#v", len(updator.updates), updator.updates)
			}
			final := requireToolCallUpdate(t, updator.updates[1], toolCallID, acp.ToolCallStatusCompleted)
			wantResource := acp.TextResourceContentsEmbeddedResourceResource(resource.Text, resource.URI)
			wantResource.MimeType = new(resource.MIMEType)
			wantResource.Meta = acp.Meta{"revision": 3.0}
			wantBlock := acp.ResourceContentBlock(wantResource)
			wantBlock.Meta = acp.Meta{"source": "test-server"}
			wantAudience := []acp.Role{acp.RoleUser}
			wantBlock.Annotations = &acp.Annotations{
				Audience:     &wantAudience,
				LastModified: new("2026-07-18T13:00:00Z"),
				Priority:     new(0.5),
			}
			want := []acp.ToolCallContent{acp.ContentToolCallContent(wantBlock)}
			if got, ok := final.Content.([]acp.ToolCallContent); !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("final content = %#v, want %#v", final.Content, want)
			}
		})
		t.Run("pdf resource", func(t *testing.T) {
			t.Parallel()

			resource := &mcpsdk.ResourceContents{
				URI:      "file:///workspace/report.pdf",
				MIMEType: pdfMIMEType,
				Blob:     []byte("pdf-data"),
			}
			session := newToolCallbackTestSession(t, &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.EmbeddedResource{Resource: resource}},
			}, nil)
			updator := &recordingSessionUpdator{}
			callback := NewToolCallback(updator, testSessionID, session, "server", "lookup", mcpconfig.ServerConfig{})
			toolCallID := acp.ToolCallId("call-embedded-pdf")

			msg, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), nil)
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			encoded := base64.StdEncoding.EncodeToString(resource.Blob)
			if msg.Role != gai.ToolResult || msg.ToolResultError || len(msg.Blocks) != 1 {
				t.Fatalf("model result = %#v, want one successful PDF block", msg)
			}
			block := msg.Blocks[0]
			if block.ModalityType != gai.Image || block.MimeType != pdfMIMEType || block.Content.String() != encoded {
				t.Fatalf("model PDF block = %#v, want encoded application/pdf", block)
			}
			if got := block.ExtraFields[gai.BlockFieldFilenameKey]; got != "report.pdf" {
				t.Fatalf("model PDF filename = %#v, want report.pdf", got)
			}
			if len(updator.updates) != 2 {
				t.Fatalf("updates len = %d, want in-progress and completed: %#v", len(updator.updates), updator.updates)
			}
			final := requireToolCallUpdate(t, updator.updates[1], toolCallID, acp.ToolCallStatusCompleted)
			wantResource := acp.BlobResourceContentsEmbeddedResourceResource(encoded, resource.URI)
			wantResource.MimeType = new(resource.MIMEType)
			want := []acp.ToolCallContent{acp.ContentToolCallContent(acp.ResourceContentBlock(wantResource))}
			if got, ok := final.Content.([]acp.ToolCallContent); !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("final content = %#v, want %#v", final.Content, want)
			}
		})
		t.Run("blob resources", func(t *testing.T) {
			t.Parallel()

			resources := []*mcpsdk.ResourceContents{
				{URI: "file:///workspace/image.png", MIMEType: "image/png", Blob: []byte("image-data")},
				{URI: "file:///workspace/audio.wav", MIMEType: "audio/wav", Blob: []byte("audio-data")},
				{URI: "file:///workspace/video.mp4", MIMEType: "video/mp4", Blob: []byte("video-data")},
				{URI: "file:///workspace/archive.bin", MIMEType: "application/octet-stream", Blob: []byte("binary-data")},
			}
			resultContent := make([]mcpsdk.Content, len(resources))
			for i, resource := range resources {
				resultContent[i] = &mcpsdk.EmbeddedResource{Resource: resource}
			}
			session := newToolCallbackTestSession(t, &mcpsdk.CallToolResult{Content: resultContent}, nil)
			updator := &recordingSessionUpdator{}
			callback := NewToolCallback(updator, testSessionID, session, "server", "lookup", mcpconfig.ServerConfig{})
			toolCallID := acp.ToolCallId("call-embedded-blobs")

			msg, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), nil)
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if msg.Role != gai.ToolResult || msg.ToolResultError || len(msg.Blocks) != len(resources) {
				t.Fatalf("model result = %#v, want four successful blocks", msg)
			}
			wantModalities := []gai.Modality{gai.Image, gai.Audio, gai.Video}
			for i, wantModality := range wantModalities {
				block := msg.Blocks[i]
				if block.ModalityType != wantModality || block.MimeType != resources[i].MIMEType || block.Content.String() != base64.StdEncoding.EncodeToString(resources[i].Blob) {
					t.Fatalf("model media block %d = %#v, want modality %v and encoded %s", i, block, wantModality, resources[i].MIMEType)
				}
			}
			binaryText := msg.Blocks[3].Content.String()
			if msg.Blocks[3].ModalityType != gai.Text || !strings.Contains(binaryText, resources[3].URI) || !strings.Contains(binaryText, resources[3].MIMEType) {
				t.Fatalf("model binary block = %#v, want text containing URI and MIME type", msg.Blocks[3])
			}

			if len(updator.updates) != 2 {
				t.Fatalf("updates len = %d, want in-progress and completed: %#v", len(updator.updates), updator.updates)
			}
			final := requireToolCallUpdate(t, updator.updates[1], toolCallID, acp.ToolCallStatusCompleted)
			want := make([]acp.ToolCallContent, len(resources))
			for i, resource := range resources {
				embedded := acp.BlobResourceContentsEmbeddedResourceResource(
					base64.StdEncoding.EncodeToString(resource.Blob),
					resource.URI,
				)
				embedded.MimeType = new(resource.MIMEType)
				want[i] = acp.ContentToolCallContent(acp.ResourceContentBlock(embedded))
			}
			if got, ok := final.Content.([]acp.ToolCallContent); !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("final content = %#v, want %#v", final.Content, want)
			}
		})
	})
	t.Run("reports", func(t *testing.T) {
		t.Run("pdf as embedded acp resource", func(t *testing.T) {
			t.Parallel()

			data := []byte("pdf-data")
			session := newToolCallbackTestSession(t, &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.ImageContent{Data: data, MIMEType: pdfMIMEType}},
			}, nil)
			updator := &recordingSessionUpdator{}
			callback := NewToolCallback(updator, testSessionID, session, "server", "lookup", mcpconfig.ServerConfig{})
			toolCallID := acp.ToolCallId("call-pdf")

			msg, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), nil)
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if msg.Role != gai.ToolResult || len(msg.Blocks) != 1 || msg.Blocks[0].MimeType != pdfMIMEType {
				t.Fatalf("model result = %#v, want one PDF block", msg)
			}
			if len(updator.updates) != 2 {
				t.Fatalf("updates len = %d, want in-progress and completed: %#v", len(updator.updates), updator.updates)
			}
			final := requireToolCallUpdate(t, updator.updates[1], toolCallID, acp.ToolCallStatusCompleted)
			wantResource := acp.BlobResourceContentsEmbeddedResourceResource(
				base64.StdEncoding.EncodeToString(data),
				"artifact:///document.pdf",
			)
			wantResource.MimeType = new(pdfMIMEType)
			want := []acp.ToolCallContent{acp.ContentToolCallContent(acp.ResourceContentBlock(wantResource))}
			if got, ok := final.Content.([]acp.ToolCallContent); !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("final content = %#v, want %#v", final.Content, want)
			}
		})
		t.Run("session updates", func(t *testing.T) {
			t.Parallel()

			params := map[string]any{"query": "docs"}
			tests := []struct {
				name                string
				result              *mcpsdk.CallToolResult
				handlerErr          error
				toolCallID          acp.ToolCallId
				wantToolResultError bool
				wantFinalStatus     acp.ToolCallStatus
				wantText            string
				wantTextContains    bool
			}{
				{
					name: "success",
					result: &mcpsdk.CallToolResult{
						Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "lookup result"}},
					},
					toolCallID:      "call-success",
					wantFinalStatus: acp.ToolCallStatusCompleted,
					wantText:        "lookup result",
				},
				{
					name: "tool error",
					result: &mcpsdk.CallToolResult{
						Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "lookup failed"}},
						IsError: true,
					},
					toolCallID:          "call-tool-error",
					wantToolResultError: true,
					wantFinalStatus:     acp.ToolCallStatusFailed,
					wantText:            "lookup failed",
				},
				{
					name:             "call error",
					handlerErr:       fmt.Errorf("server exploded"),
					toolCallID:       "call-call-error",
					wantFinalStatus:  acp.ToolCallStatusFailed,
					wantText:         "server exploded",
					wantTextContains: true,
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					session := newToolCallbackTestSession(t, tt.result, tt.handlerErr)
					updator := &recordingSessionUpdator{}
					callback := NewToolCallback(updator, testSessionID, session, "server", "lookup", mcpconfig.ServerConfig{})

					msg, err := callback.Call(xctx.WithToolCallId(t.Context(), tt.toolCallID), params)
					if err != nil {
						t.Fatalf("Call() error = %v", err)
					}
					requireText(t, "tool result text", requireToolResultText(t, msg, tt.wantToolResultError), tt.wantText, tt.wantTextContains)

					if len(updator.updates) != 2 {
						t.Fatalf("updates len = %d, want 2: %#v", len(updator.updates), updator.updates)
					}
					inProgress := requireToolCallUpdate(t, updator.updates[0], tt.toolCallID, acp.ToolCallStatusInProgress)
					if inProgress.Kind == nil || *inProgress.Kind != acp.ToolKindOther {
						t.Fatalf("Kind = %#v, want %q", inProgress.Kind, acp.ToolKindOther)
					}
					if !reflect.DeepEqual(inProgress.RawInput, params) {
						t.Fatalf("RawInput = %#v, want %#v", inProgress.RawInput, params)
					}

					final := requireToolCallUpdate(t, updator.updates[1], tt.toolCallID, tt.wantFinalStatus)
					requireText(t, "final update text", requireToolCallContentText(t, final), tt.wantText, tt.wantTextContains)
				})
			}
		})
	})
}

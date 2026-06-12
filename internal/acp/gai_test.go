package acp

import (
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/spachava753/gai"
)

func TestPromptToMessagePreservesACPBase64Content(t *testing.T) {
	t.Parallel()

	const imageBase64 = "iVBORw0KGgo="
	const audioBase64 = "UklGRiQAAABXQVZF"

	msg := (&Agent{}).promptToMessage([]acpsdk.ContentBlock{
		acpsdk.TextBlock("look"),
		acpsdk.ImageBlock(imageBase64, "image/png"),
		acpsdk.AudioBlock(audioBase64, "audio/wav"),
	})

	if msg.Role != gai.User {
		t.Fatalf("role = %v, want %v", msg.Role, gai.User)
	}
	if len(msg.Blocks) != 3 {
		t.Fatalf("blocks len = %d, want 3", len(msg.Blocks))
	}

	image := msg.Blocks[1]
	if image.ModalityType != gai.Image || image.MimeType != "image/png" {
		t.Fatalf("image block = %#v", image)
	}
	if got := image.Content.String(); got != imageBase64 {
		t.Fatalf("image content = %q, want %q", got, imageBase64)
	}

	audio := msg.Blocks[2]
	if audio.ModalityType != gai.Audio || audio.MimeType != "audio/wav" {
		t.Fatalf("audio block = %#v", audio)
	}
	if got := audio.Content.String(); got != audioBase64 {
		t.Fatalf("audio content = %q, want %q", got, audioBase64)
	}
}

func TestPromptToMessageEmbeddedResources(t *testing.T) {
	t.Parallel()

	t.Run("text and inferred image blob", func(t *testing.T) {
		msg := (&Agent{}).promptToMessage([]acpsdk.ContentBlock{
			acpsdk.ResourceBlock(acpsdk.EmbeddedResourceResource{TextResourceContents: &acpsdk.TextResourceContents{
				Uri:  "file:///workspace/notes.md",
				Text: "# Notes\nKeep this context.",
			}}),
			acpsdk.ResourceBlock(acpsdk.EmbeddedResourceResource{BlobResourceContents: &acpsdk.BlobResourceContents{
				Uri:  "file:///workspace/screenshots/input.png",
				Blob: "iVBORw0KGgo=",
			}}),
		})

		if msg.Role != gai.User {
			t.Fatalf("role = %v, want %v", msg.Role, gai.User)
		}
		if len(msg.Blocks) != 2 {
			t.Fatalf("blocks len = %d, want 2", len(msg.Blocks))
		}

		wantText := "`file:///workspace/notes.md` contents:\n```# Notes\nKeep this context.\n```\n"
		if got := msg.Blocks[0].Content.String(); got != wantText {
			t.Fatalf("text resource content = %q, want %q", got, wantText)
		}

		blob := msg.Blocks[1]
		if blob.BlockType != gai.Content || blob.ModalityType != gai.Image || blob.MimeType != "image/png" {
			t.Fatalf("blob block = %#v", blob)
		}
		if got := blob.Content.String(); got != "iVBORw0KGgo=" {
			t.Fatalf("blob content = %q, want base64 payload", got)
		}
		if got := blob.ExtraFields[gai.BlockFieldFilenameKey]; got != "input.png" {
			t.Fatalf("blob filename = %#v, want %q", got, "input.png")
		}
	})

	t.Run("explicit MIME type", func(t *testing.T) {
		mimeType := "application/pdf"
		msg := (&Agent{}).promptToMessage([]acpsdk.ContentBlock{
			acpsdk.ResourceBlock(acpsdk.EmbeddedResourceResource{BlobResourceContents: &acpsdk.BlobResourceContents{
				Uri:      "file:///workspace/docs/report.unknown",
				MimeType: &mimeType,
				Blob:     "JVBERi0xLjQ=",
			}}),
		})

		if len(msg.Blocks) != 1 {
			t.Fatalf("blocks len = %d, want 1", len(msg.Blocks))
		}

		pdf := msg.Blocks[0]
		if pdf.ModalityType != gai.Image || pdf.MimeType != "application/pdf" {
			t.Fatalf("pdf block = %#v", pdf)
		}
		if got := pdf.ExtraFields[gai.BlockFieldFilenameKey]; got != "report.unknown" {
			t.Fatalf("pdf filename = %#v, want %q", got, "report.unknown")
		}
	})

	t.Run("panic on unknown MIME type", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("promptToMessage() did not panic")
			}
		}()

		_ = (&Agent{}).promptToMessage([]acpsdk.ContentBlock{
			acpsdk.ResourceBlock(acpsdk.EmbeddedResourceResource{BlobResourceContents: &acpsdk.BlobResourceContents{
				Uri:  "untitled-resource",
				Blob: "AAAA",
			}}),
		})
	})

	t.Run("panic on invalid URI", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("promptToMessage() did not panic")
			}
		}()

		_ = (&Agent{}).promptToMessage([]acpsdk.ContentBlock{
			acpsdk.ResourceBlock(acpsdk.EmbeddedResourceResource{BlobResourceContents: &acpsdk.BlobResourceContents{
				Uri:  "file:///%zz",
				Blob: "AAAA",
			}}),
		})
	})
}

package agent

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
)

func TestParseContent(t *testing.T) {
	var imageData bytes.Buffer
	encoder := png.Encoder{
		CompressionLevel: png.NoCompression,
	}
	if err := encoder.Encode(
		&imageData,
		image.NewGray(image.Rect(
			0,
			0,
			3072,
			3072,
		)),
	); err != nil {
		t.Fatal(err)
	}
	encodedImage := base64.StdEncoding.EncodeToString(imageData.Bytes())
	largeText := strings.Repeat(
		"x",
		9<<20,
	)
	for _, test := range []struct {
		name     string
		content  acp.ContentBlock
		want     string
		modality gai.Modality
		wantErr  bool
	}{
		{
			name:     "text",
			content:  acp.TextContentBlock("task"),
			want:     "task",
			modality: gai.Text,
		},
		{
			name:     "large text",
			content:  acp.TextContentBlock(largeText),
			want:     largeText,
			modality: gai.Text,
		},
		{
			name: "large image",
			content: acp.ImageContentBlock(
				encodedImage,
				"image/png",
			),
			want:     encodedImage,
			modality: gai.Image,
		},
		{
			name: "invalid base64",
			content: acp.ImageContentBlock(
				"!",
				"image/png",
			),
			wantErr: true,
		},
		{
			name: "empty image",
			content: acp.ImageContentBlock(
				"",
				"image/png",
			),
			wantErr: true,
		},
		{
			name: "unsupported image format",
			content: acp.ImageContentBlock(
				base64.StdEncoding.EncodeToString([]byte("not an image")),
				"image/png",
			),
			wantErr: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				message, err := parseContent([]acp.ContentBlock{
					test.content,
				})
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"parse error: %v",
						err,
					)
				}
				if test.wantErr {
					return
				}
				if message.Role != gai.User || len(message.Blocks) != 1 {
					t.Fatal("prompt did not retain its content block")
				}
				block := message.Blocks[0]
				if block.ModalityType != test.modality || block.Content.String() != test.want {
					t.Fatal("prompt content was altered or truncated")
				}
			},
		)
	}
}

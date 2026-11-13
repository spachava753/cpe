package agent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
)

func imageBlock(data []byte) (gai.Block, error) {
	if len(data) == 0 {
		return gai.Block{}, errors.New("image is empty")
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return gai.ImageBlock(
			data,
			mime,
		), nil
	default:
		return gai.Block{}, errors.New("only PNG, JPEG, GIF, and WebP images are supported")
	}
}

// resourceLinkBlock describes a resource link to the model as text. CPE does not
// fetch the resource; the model can read it with its tools when it needs to.
func resourceLinkBlock(content acp.ContentBlock) (gai.Block, error) {
	if content.URI == nil || *content.URI == "" {
		return gai.Block{}, errors.New("resource link must have a URI")
	}
	var text strings.Builder
	fmt.Fprintf(
		&text,
		"[Resource link]\nName: %s\nURI: %s",
		content.Name,
		*content.URI,
	)
	optional := []struct {
		value *string
		label string
	}{
		{
			label: "Title",
			value: content.Title,
		},
		{
			label: "Description",
			value: content.Description,
		},
		{
			label: "MIME type",
			value: content.MimeType,
		},
	}
	for _, field := range optional {
		if field.value != nil && *field.value != "" {
			fmt.Fprintf(
				&text,
				"\n%s: %s",
				field.label,
				*field.value,
			)
		}
	}
	if content.Size != nil {
		fmt.Fprintf(
			&text,
			"\nSize: %d bytes",
			*content.Size,
		)
	}
	return gai.TextBlock(text.String()), nil
}

func acpContent(block gai.Block) (acp.ContentBlock, error) {
	if block.Content == nil {
		return acp.ContentBlock{}, errors.New("empty message block")
	}
	switch {
	case block.ModalityType == gai.Text:
		return acp.TextContentBlock(block.Content.String()), nil
	case block.ModalityType == gai.Image && strings.HasPrefix(
		block.MimeType,
		"image/",
	):
		return acp.ImageContentBlock(
			block.Content.String(),
			block.MimeType,
		), nil
	case block.ModalityType == gai.Audio:
		return acp.AudioContentBlock(
			block.Content.String(),
			block.MimeType,
		), nil
	default:
		// ACP has no inline content for video or documents; describe them instead.
		return acp.TextContentBlock(fmt.Sprintf(
			"[%s media shown to the model]",
			block.MimeType,
		)), nil
	}
}

func parseContent(contents []acp.ContentBlock) (gai.Message, error) {
	if len(contents) == 0 {
		return gai.Message{}, errors.New("prompt must contain content")
	}
	message := gai.Message{
		Role: gai.User,
	}
	for _, content := range contents {
		switch content.Type {
		case acp.ContentBlockTypeText:
			message.Blocks = append(
				message.Blocks,
				gai.TextBlock(content.Text),
			)
		case acp.ContentBlockTypeImage:
			data, err := base64.StdEncoding.DecodeString(content.Data)
			if err != nil {
				return gai.Message{}, err
			}
			block, err := imageBlock(data)
			if err != nil {
				return gai.Message{}, err
			}
			message.Blocks = append(
				message.Blocks,
				block,
			)
		case acp.ContentBlockTypeResourceLink:
			block, err := resourceLinkBlock(content)
			if err != nil {
				return gai.Message{}, err
			}
			message.Blocks = append(
				message.Blocks,
				block,
			)
		default:
			return gai.Message{}, errors.New("only text, image, and resource link prompts are supported")
		}
	}
	return message, nil
}

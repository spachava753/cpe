package client

import (
	"encoding/json"
	"fmt"

	"github.com/spachava753/acp-sdk/acp"
)

func contentText(v any) string {
	switch block := v.(type) {
	case acp.ContentBlock:
		return contentBlockText(block)
	case map[string]any:
		if text, ok := decodedContentText(block); ok {
			return text
		}
	}
	return marshal(v)
}

func contentBlockText(block acp.ContentBlock) string {
	switch block.Type {
	case acp.ContentBlockTypeText:
		return block.Text
	case acp.ContentBlockTypeImage, acp.ContentBlockTypeAudio:
		return fmt.Sprintf("[%s content]", block.Type)
	case acp.ContentBlockTypeResourceLink:
		if block.URI != nil {
			return fmt.Sprintf("[%s](%s)", block.Name, *block.URI)
		}
		return block.Name
	default:
		return marshal(block)
	}
}

func decodedContentText(block map[string]any) (string, bool) {
	typeName, ok := block["type"].(string)
	if !ok {
		return "", false
	}
	switch acp.ContentBlockType(typeName) {
	case acp.ContentBlockTypeText:
		text, _ := block["text"].(string)
		return text, true
	case acp.ContentBlockTypeImage, acp.ContentBlockTypeAudio:
		return fmt.Sprintf("[%s content]", typeName), true
	case acp.ContentBlockTypeResourceLink:
		name, _ := block["name"].(string)
		uri, ok := block["uri"].(string)
		if ok {
			return fmt.Sprintf("[%s](%s)", name, uri), true
		}
		return name, true
	default:
		return "", false
	}
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func marshal(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

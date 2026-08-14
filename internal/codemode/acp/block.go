package acp

import (
	"fmt"
	"strings"

	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"
)

type blockValue struct {
	id        string
	kind      string
	modality  string
	mimeType  string
	content   starlark.Value
	name      string
	arguments starlark.Value
	filename  string
	frozen    bool
}

var blockAttrNames = []string{
	"arguments",
	"content",
	"filename",
	"id",
	"kind",
	"mime_type",
	"modality",
	"name",
}

func newBlockValue(block gai.Block) (*blockValue, error) {
	value := &blockValue{
		id:        block.ID,
		kind:      block.BlockType,
		mimeType:  block.MimeType,
		content:   starlark.None,
		arguments: starlark.None,
	}
	if block.BlockType != gai.ToolCall {
		value.modality = starlarkModality(block.ModalityType)
	}
	if block.Content != nil {
		value.content = starlark.String(block.Content.String())
	}
	value.filename, _ = extraString(block.ExtraFields, gai.BlockFieldFilenameKey)

	if block.BlockType == gai.ToolCall {
		if block.Content == nil {
			return nil, fmt.Errorf("tool call %q has no content", block.ID)
		}
		name, arguments, err := decodeToolCall(block.Content.String())
		if err != nil {
			return nil, fmt.Errorf("decode tool call %q: %w", block.ID, err)
		}
		value.name = name
		value.arguments = arguments
	}
	return value, nil
}

func (b *blockValue) String() string {
	parts := []string{fmt.Sprintf("kind=%q", b.kind)}
	if b.id != "" {
		parts = append(parts, fmt.Sprintf("id=%q", b.id))
	}
	if b.name != "" {
		parts = append(parts, fmt.Sprintf("name=%q", b.name))
	}
	return "acp.Block(" + strings.Join(parts, ", ") + ")"
}

func (*blockValue) Type() string { return "acp.Block" }

func (b *blockValue) Freeze() {
	if b.frozen {
		return
	}
	b.frozen = true
	b.content.Freeze()
	b.arguments.Freeze()
}

func (*blockValue) Truth() starlark.Bool { return starlark.True }

func (b *blockValue) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable: %s", b.Type())
}

func (b *blockValue) Attr(name string) (starlark.Value, error) {
	switch name {
	case "id":
		return optionalStarlarkString(b.id), nil
	case "kind":
		return starlark.String(b.kind), nil
	case "modality":
		return optionalStarlarkString(b.modality), nil
	case "mime_type":
		return optionalStarlarkString(b.mimeType), nil
	case "content":
		return b.content, nil
	case "name":
		return optionalStarlarkString(b.name), nil
	case "arguments":
		return b.arguments, nil
	case "filename":
		return optionalStarlarkString(b.filename), nil
	default:
		return nil, nil
	}
}

func (*blockValue) AttrNames() []string { return blockAttrNames }

func starlarkModality(modality gai.Modality) string {
	switch modality {
	case gai.Text:
		return "text"
	case gai.Image:
		return "image"
	case gai.Audio:
		return "audio"
	case gai.Video:
		return "video"
	default:
		return ""
	}
}

var _ starlark.HasAttrs = (*blockValue)(nil)

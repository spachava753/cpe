package agent

import (
	"encoding/json"
	"fmt"

	"github.com/spachava753/gai"
)

// wireMessage encodes fmt.Stringer block content explicitly. This avoids loss
// of tool IDs, thinking signatures, multimodal data, and provider replay fields.
type wireMessage struct {
	Role   gai.Role       `json:"role"`
	Blocks []wireBlock    `json:"blocks"`
	Error  bool           `json:"tool_result_error,omitempty"`
	Extra  map[string]any `json:"extra_fields,omitempty"`
}
type wireBlock struct {
	ID       string         `json:"id,omitempty"`
	Type     string         `json:"block_type"`
	Modality gai.Modality   `json:"modality_type"`
	MIME     string         `json:"mime_type,omitempty"`
	Content  *string        `json:"content,omitempty"`
	Extra    map[string]any `json:"extra_fields,omitempty"`
}

func encodeMessage(m gai.Message) wireMessage {
	w := wireMessage{Role: m.Role, Error: m.ToolResultError, Extra: m.ExtraFields}
	for _, b := range m.Blocks {
		var text *string
		if b.Content != nil {
			v := b.Content.String()
			text = &v
		}
		w.Blocks = append(w.Blocks, wireBlock{ID: b.ID, Type: b.BlockType, Modality: b.ModalityType, MIME: b.MimeType, Content: text, Extra: b.ExtraFields})
	}
	return w
}
func decodeMessage(data []byte) (gai.Message, error) {
	var w wireMessage
	if err := json.Unmarshal(data, &w); err != nil {
		return gai.Message{}, err
	}
	if w.Role != gai.User && w.Role != gai.Assistant && w.Role != gai.ToolResult {
		return gai.Message{}, fmt.Errorf("invalid stored role %v", w.Role)
	}
	m := gai.Message{Role: w.Role, ToolResultError: w.Error, ExtraFields: w.Extra}
	for _, b := range w.Blocks {
		block := gai.Block{ID: b.ID, BlockType: b.Type, ModalityType: b.Modality, MimeType: b.MIME, ExtraFields: b.Extra}
		if b.Content != nil {
			block.Content = gai.Str(*b.Content)
		}
		m.Blocks = append(m.Blocks, block)
	}
	return m, nil
}

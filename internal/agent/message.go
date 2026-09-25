package agent

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/spachava753/gai"
)

// wireMessage encodes fmt.Stringer block content explicitly. This avoids loss
// of tool IDs, thinking signatures, multimodal data, and provider replay fields.
type wireMessage struct {
	Origin *messageOrigin `json:"origin,omitempty"`
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
func decodeMessage(data []byte) (gai.Message, *messageOrigin, error) {
	var w wireMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&w); err != nil {
		return gai.Message{}, nil, err
	}
	if w.Role != gai.User && w.Role != gai.Assistant && w.Role != gai.ToolResult {
		return gai.Message{}, nil, fmt.Errorf("invalid stored role %v", w.Role)
	}
	if o := w.Origin; o != nil {
		prefix, err := hex.DecodeString(o.Prefix)
		service, serviceErr := hex.DecodeString(o.Identity.Service)
		if o.Version != 1 || o.Identity.Model == "" || w.Role != gai.Assistant || err != nil || len(prefix) != 32 || serviceErr != nil || len(service) != 32 {
			return gai.Message{}, nil, fmt.Errorf("unsupported or malformed message origin version %d", o.Version)
		}
	}
	m := gai.Message{Role: w.Role, ToolResultError: w.Error, ExtraFields: w.Extra}
	for _, b := range w.Blocks {
		block := gai.Block{ID: b.ID, BlockType: b.Type, ModalityType: b.Modality, MimeType: b.MIME, ExtraFields: b.Extra}
		if b.Content != nil {
			block.Content = gai.Str(*b.Content)
		}
		m.Blocks = append(m.Blocks, block)
	}
	return m, w.Origin, nil
}

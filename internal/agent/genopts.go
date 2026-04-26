package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

const responsesPromptCacheKeyVersion = "v1"

// BuildGenOptsForDialog returns a fresh copy of the base generation options for
// a specific dialog.
//
// For Responses models, it also applies the default reasoning-summary setting
// and injects a stable prompt cache key unless the user already provided one.
func BuildGenOptsForDialog(model config.Model, base *gai.GenOpts, dialog gai.Dialog) *gai.GenOpts {
	opts := cloneGenOpts(base)
	if !strings.EqualFold(model.Type, ModelTypeResponses) {
		return opts
	}
	if opts == nil {
		opts = &gai.GenOpts{}
	}

	ApplyResponsesThinkingSummary(opts)

	if opts.ExtraArgs != nil {
		if _, exists := opts.ExtraArgs[gai.ResponsesPromptCacheKeyParam]; exists {
			return opts
		}
	} else {
		opts.ExtraArgs = make(map[string]any)
	}

	if key := responsesPromptCacheKey(model.ID, dialog); key != "" {
		opts.ExtraArgs[gai.ResponsesPromptCacheKeyParam] = key
	}

	return opts
}

func cloneGenOpts(base *gai.GenOpts) *gai.GenOpts {
	if base == nil {
		return nil
	}

	clone := *base
	clone.StopSequences = slices.Clone(base.StopSequences)
	clone.OutputModalities = slices.Clone(base.OutputModalities)
	clone.ExtraArgs = maps.Clone(base.ExtraArgs)
	return &clone
}

type promptCachePayload struct {
	Version  string               `json:"version"`
	ModelID  string               `json:"modelId"`
	Messages []promptCacheMessage `json:"messages"`
}

type promptCacheMessage struct {
	Role   string             `json:"role"`
	Phase  string             `json:"phase,omitempty"`
	ToolID string             `json:"toolId,omitempty"`
	Blocks []promptCacheBlock `json:"blocks,omitempty"`
}

type promptCacheBlock struct {
	BlockType      string `json:"blockType"`
	Modality       string `json:"modality"`
	ID             string `json:"id,omitempty"`
	MimeType       string `json:"mimeType,omitempty"`
	Content        string `json:"content,omitempty"`
	Filename       string `json:"filename,omitempty"`
	ToolName       string `json:"toolName,omitempty"`
	ToolParameters any    `json:"toolParameters,omitempty"`
}

func responsesPromptCacheKey(modelID string, dialog gai.Dialog) string {
	payload := promptCachePayload{
		Version: responsesPromptCacheKeyVersion,
		ModelID: modelID,
	}

	for _, msg := range dialogPrefixThroughLastUser(dialog) {
		canonical := promptCacheMessage{
			Role: msg.Role.String(),
		}
		if phase, ok := msg.ExtraFields[gai.ResponsesMessageExtraFieldPhase].(string); ok && phase != "" {
			canonical.Phase = phase
		}
		canonical.ToolID = promptCacheToolResultID(msg)
		canonical.Blocks = canonicalPromptCacheBlocks(msg)
		payload.Messages = append(payload.Messages, canonical)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON = fmt.Appendf(nil, "%#v", payload)
	}

	hash := sha256.Sum256(payloadJSON)
	return hex.EncodeToString(hash[:])
}

func dialogPrefixThroughLastUser(dialog gai.Dialog) gai.Dialog {
	if len(dialog) == 0 {
		return nil
	}
	for i := len(dialog) - 1; i >= 0; i-- {
		if dialog[i].Role == gai.User {
			return dialog[:i+1]
		}
	}
	return dialog
}

func skipPromptCacheBlock(block gai.Block) bool {
	switch block.BlockType {
	case gai.Thinking, gai.MetadataBlockType:
		return true
	default:
		return false
	}
}

func canonicalPromptCacheBlocks(msg gai.Message) []promptCacheBlock {
	blocks := filteredPromptCacheBlocks(msg.Blocks)
	if len(blocks) == 0 {
		return nil
	}
	if msg.Role == gai.ToolResult && promptCacheBlocksAreAllText(blocks) {
		var sb strings.Builder
		for _, block := range blocks {
			sb.WriteString(blockContentString(block))
		}
		return []promptCacheBlock{{
			BlockType: gai.Content,
			Modality:  gai.Text.String(),
			Content:   sb.String(),
		}}
	}

	canonical := make([]promptCacheBlock, 0, len(blocks))
	for _, block := range blocks {
		canonical = append(canonical, canonicalPromptCacheBlock(block))
	}
	return canonical
}

func filteredPromptCacheBlocks(blocks []gai.Block) []gai.Block {
	filtered := make([]gai.Block, 0, len(blocks))
	for _, block := range blocks {
		if skipPromptCacheBlock(block) {
			continue
		}
		filtered = append(filtered, block)
	}
	return filtered
}

func promptCacheBlocksAreAllText(blocks []gai.Block) bool {
	for _, block := range blocks {
		if block.ModalityType != gai.Text {
			return false
		}
	}
	return true
}

func promptCacheToolResultID(msg gai.Message) string {
	if msg.Role != gai.ToolResult || len(msg.Blocks) == 0 {
		return ""
	}
	return msg.Blocks[0].ID
}

func canonicalPromptCacheBlock(block gai.Block) promptCacheBlock {
	canonical := promptCacheBlock{
		BlockType: block.BlockType,
		Modality:  block.ModalityType.String(),
	}
	if block.BlockType == gai.ToolCall && block.ID != "" {
		canonical.ID = block.ID
	}
	if block.ModalityType != gai.Text {
		canonical.MimeType = block.MimeType
	}
	if filename, ok := block.ExtraFields[gai.BlockFieldFilenameKey].(string); ok && filename != "" {
		canonical.Filename = filename
	}
	if block.BlockType == gai.ToolCall {
		toolName, toolParams, raw := canonicalizeToolCallBlock(block)
		if toolName == "" && toolParams == nil {
			canonical.Content = raw
		} else {
			canonical.ToolName = toolName
			canonical.ToolParameters = toolParams
		}
		return canonical
	}
	canonical.Content = blockContentString(block)
	return canonical
}

func canonicalizeToolCallBlock(block gai.Block) (string, any, string) {
	raw := blockContentString(block)
	if raw == "" {
		return "", nil, raw
	}

	var toolCall struct {
		Name       string `json:"name"`
		Parameters any    `json:"parameters"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&toolCall); err != nil {
		return "", nil, raw
	}

	var extra any
	if err := decoder.Decode(&extra); err == nil || err != io.EOF {
		return "", nil, raw
	}

	return toolCall.Name, toolCall.Parameters, raw
}

func blockContentString(block gai.Block) string {
	if block.Content == nil {
		return ""
	}
	return block.Content.String()
}

package commands

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

func messageMetadataInline(msg gai.Message) string {
	var parts []string
	if model := messageModel(msg); model != "" {
		parts = append(parts, "model: "+model)
	}
	if tokens := messageTokenSummary(msg, false); tokens != "" {
		parts = append(parts, "tokens: "+tokens)
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, "] [") + "]"
}

func messageMetadataMarkdown(msg gai.Message) string {
	var lines []string
	if model := messageModel(msg); model != "" {
		lines = append(lines, fmt.Sprintf("> model: `%s`", model))
	}
	if tokens := messageTokenSummary(msg, true); tokens != "" {
		lines = append(lines, "> tokens: "+tokens)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n\n"
}

func messageModel(msg gai.Message) string {
	modelID, _ := msg.ExtraFields[storage.AgentMetadataModelIDKey].(string)
	modelType, _ := msg.ExtraFields[storage.AgentMetadataModelTypeKey].(string)
	modelRef, _ := msg.ExtraFields[storage.AgentMetadataModelRefKey].(string)
	modelDisplayName, _ := msg.ExtraFields[storage.AgentMetadataModelDisplayNameKey].(string)

	switch {
	case modelType != "" && modelID != "":
		return modelType + "/" + modelID
	case modelID != "":
		return modelID
	case modelRef != "":
		return modelRef
	default:
		return modelDisplayName
	}
}

func messageTokenSummary(msg gai.Message, markdown bool) string {
	fields := []struct {
		label string
		key   string
	}{
		{"in", storage.AgentMetadataInputTokensKey},
		{"out", storage.AgentMetadataOutputTokensKey},
		{"cache", storage.AgentMetadataCacheReadTokensKey},
		{"cache write", storage.AgentMetadataCacheWriteTokensKey},
	}

	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		tokens, ok := messageExtraInt64(msg, field.key)
		if !ok {
			continue
		}
		value := strconv.FormatInt(tokens, 10)
		if markdown {
			value = "`" + value + "`"
		}
		parts = append(parts, field.label+" "+value)
	}

	separator := " / "
	if markdown {
		separator = ", "
	}
	return strings.Join(parts, separator)
}

func messageExtraInt64(msg gai.Message, key string) (int64, bool) {
	if msg.ExtraFields == nil {
		return 0, false
	}
	value, ok := msg.ExtraFields[key]
	if !ok || value == nil {
		return 0, false
	}
	return metadataInt64(value)
}

func metadataInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return uint64ToInt64(uint64(v))
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return uint64ToInt64(v)
	case float64:
		if math.Trunc(v) != v || v < math.MinInt64 || v > math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	case json.Number:
		parsed, err := v.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func uint64ToInt64(v uint64) (int64, bool) {
	if v > math.MaxInt64 {
		return 0, false
	}
	return int64(v), true
}

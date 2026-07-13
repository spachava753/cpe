package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"github.com/spachava753/gai"
)

// roleToString converts a gai.Role to its string representation.
func roleToString(role gai.Role) string {
	switch role {
	case gai.User:
		return "user"
	case gai.Assistant:
		return "assistant"
	case gai.ToolResult:
		return "tool_result"
	default:
		return "unknown"
	}
}

// stringToRole converts a string to its gai.Role representation.
func stringToRole(s string) (gai.Role, error) {
	switch s {
	case "user":
		return gai.User, nil
	case "assistant":
		return gai.Assistant, nil
	case "tool_result":
		return gai.ToolResult, nil
	default:
		return 0, fmt.Errorf("invalid role: %s", s)
	}
}

// getExtraFieldString safely extracts a string value from an ExtraFields map.
func getExtraFieldString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

var messageColumnExtraFieldKeys = map[string]struct{}{
	MessageIDKey:                     {},
	MessageParentIDKey:               {},
	MessageCompactionParentIDKey:     {},
	MessageCreatedAtKey:              {},
	AgentMetadataModelRefKey:         {},
	AgentMetadataModelIDKey:          {},
	AgentMetadataModelTypeKey:        {},
	AgentMetadataModelDisplayNameKey: {},
	AgentMetadataInputTokensKey:      {},
	AgentMetadataOutputTokensKey:     {},
	AgentMetadataCacheReadTokensKey:  {},
	AgentMetadataCacheWriteTokensKey: {},
}

func encodeMessageExtraFields(extra map[string]any) (sql.NullString, error) {
	if len(extra) == 0 {
		return sql.NullString{}, nil
	}
	filtered := make(map[string]any, len(extra))
	for key, value := range extra {
		if _, ok := messageColumnExtraFieldKeys[key]; ok {
			continue
		}
		filtered[key] = value
	}
	if len(filtered) == 0 {
		return sql.NullString{}, nil
	}

	extraFieldsJSON, err := json.Marshal(filtered)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("failed to marshal message ExtraFields to JSON: %w", err)
	}
	return sql.NullString{String: string(extraFieldsJSON), Valid: true}, nil
}

func messageMetadataString(extra map[string]any, key string) (sql.NullString, error) {
	if extra == nil {
		return sql.NullString{}, nil
	}
	value, ok := extra[key]
	if !ok || value == nil {
		return sql.NullString{}, nil
	}
	str, ok := value.(string)
	if !ok {
		return sql.NullString{}, fmt.Errorf("message ExtraFields[%q] must be a string, got %T", key, value)
	}
	return sql.NullString{String: str, Valid: true}, nil
}

func messageMetadataInt64(extra map[string]any, key string) (sql.NullInt64, error) {
	if extra == nil {
		return sql.NullInt64{}, nil
	}
	value, ok := extra[key]
	if !ok || value == nil {
		return sql.NullInt64{}, nil
	}
	intValue, err := extraFieldInt64(value)
	if err != nil {
		return sql.NullInt64{}, fmt.Errorf("message ExtraFields[%q] must be an integer: %w", key, err)
	}
	return sql.NullInt64{Int64: intValue, Valid: true}, nil
}

func extraFieldInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		if uint64(v) > math.MaxInt64 {
			return 0, fmt.Errorf("%d overflows int64", v)
		}
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return 0, fmt.Errorf("%d overflows int64", v)
		}
		return int64(v), nil
	case float64:
		if math.Trunc(v) != v || v < math.MinInt64 || v > math.MaxInt64 {
			return 0, fmt.Errorf("%v is not an int64", v)
		}
		return int64(v), nil
	default:
		return 0, fmt.Errorf("got %T", value)
	}
}

func decodeMessageExtraFields(encoded sql.NullString) (map[string]any, error) {
	if !encoded.Valid || encoded.String == "" {
		return map[string]any{}, nil
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(encoded.String), &extra); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message ExtraFields: %w", err)
	}
	if extra == nil {
		return map[string]any{}, nil
	}
	return extra, nil
}

func putNullString(extra map[string]any, key string, value sql.NullString) {
	if value.Valid {
		extra[key] = value.String
	}
}

func putNullInt64(extra map[string]any, key string, value sql.NullInt64) {
	if value.Valid {
		extra[key] = value.Int64
	}
}

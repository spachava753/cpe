package mapstruct

import (
	"encoding/json"
)

// Map2Struct maps m into T using JSON field names and tags.
func Map2Struct[T any](m map[string]any) (T, error) {
	var t T

	b, err := json.Marshal(m)
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return t, err
	}
	return t, nil
}

package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// Raw messages keep absent, null, and incorrectly typed fields distinct. Only
// objects whose schema we implement are inspected; extension contents remain
// opaque. Duplicate keys at these boundaries are rejected, not last-wins.
func jsonObject(data []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("JSON must be UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, fmt.Errorf("expected a JSON object")
	}
	object := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("expected a string object key")
		}
		if _, exists := object[key]; exists {
			return nil, fmt.Errorf("duplicate JSON key %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		object[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected trailing JSON data")
	}
	return object, nil
}

func jsonString(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", fmt.Errorf("expected a JSON string")
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return "", err
	}
	return value, nil
}

func jsonStrings(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("expected an array of strings")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, err
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		value, err := jsonString(item)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func jsonStringMap(raw json.RawMessage) (map[string]string, error) {
	object, err := jsonObject(raw)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(object))
	for key, raw := range object {
		value, err := jsonString(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		values[key] = value
	}
	return values, nil
}

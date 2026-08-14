package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/spachava753/starlarkx/starlark"
)

func decodeToolCall(content string) (string, starlark.Value, error) {
	var input struct {
		Name       string         `json:"name"`
		Parameters map[string]any `json:"parameters"`
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil {
		return "", nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return "", nil, errors.New("unexpected trailing JSON value")
		}
		return "", nil, err
	}
	if input.Name == "" {
		return "", nil, errors.New("tool name must not be empty")
	}
	if input.Parameters == nil {
		input.Parameters = map[string]any{}
	}
	arguments, err := jsonCompatibleToStarlark(input.Parameters)
	if err != nil {
		return "", nil, fmt.Errorf("convert parameters: %w", err)
	}
	arguments.Freeze()
	return input.Name, arguments, nil
}

func jsonCompatibleToStarlark(value any) (starlark.Value, error) {
	switch value := value.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(value), nil
	case string:
		return starlark.String(value), nil
	case json.Number:
		text := string(value)
		if !strings.ContainsAny(text, ".eE") {
			integer, ok := new(big.Int).SetString(text, 10)
			if !ok {
				return nil, fmt.Errorf("invalid JSON integer %q", text)
			}
			return starlark.MakeBigInt(integer), nil
		}
		floating, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid JSON number %q: %w", text, err)
		}
		return starlark.Float(floating), nil
	case []any:
		items := make([]starlark.Value, len(value))
		for i, item := range value {
			converted, err := jsonCompatibleToStarlark(item)
			if err != nil {
				return nil, fmt.Errorf("array item %d: %w", i, err)
			}
			items[i] = converted
		}
		return starlark.NewList(items), nil
	case map[string]any:
		dict := starlark.NewDict(len(value))
		for _, key := range slices.Sorted(maps.Keys(value)) {
			converted, err := jsonCompatibleToStarlark(value[key])
			if err != nil {
				return nil, fmt.Errorf("object key %q: %w", key, err)
			}
			if err := dict.SetKey(starlark.String(key), converted); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		return nil, fmt.Errorf("unsupported JSON value %T", value)
	}
}

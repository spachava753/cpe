package acp

import "github.com/spachava753/starlarkx/starlark"

func optionalStarlarkString(value string) starlark.Value {
	if value == "" {
		return starlark.None
	}
	return starlark.String(value)
}

func extraString(fields map[string]any, key string) (string, bool) {
	if fields == nil {
		return "", false
	}
	value, ok := fields[key].(string)
	return value, ok
}

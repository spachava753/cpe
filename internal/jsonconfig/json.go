package jsonconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// Decode reads one JSON object, rejecting duplicate keys, unknown fields, and
// trailing data. Struct field names are case-sensitive; map keys retain their
// original spelling. Target must be a non-nil pointer to a configuration struct.
func Decode(data []byte, target any) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return errors.New("configuration must be a JSON object")
	}
	keys := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueKeys(keys, reflect.TypeOf(target)); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("configuration must contain one JSON object")
	}
	return nil
}
func uniqueKeys(decoder *json.Decoder, shape reflect.Type) error {
	for shape != nil && shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		var child reflect.Type
		if shape != nil && (shape.Kind() == reflect.Map || shape.Kind() == reflect.Slice || shape.Kind() == reflect.Array) {
			child = shape.Elem()
		}
		if delim == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if seen[name] {
				return fmt.Errorf("duplicate JSON key %q", name)
			}
			seen[name] = true
			if shape != nil && shape.Kind() == reflect.Struct {
				for _, field := range reflect.VisibleFields(shape) {
					if !field.IsExported() {
						continue
					}
					tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
					if tag == "-" {
						continue
					}
					if field.Anonymous && tag == "" {
						continue
					}
					if tag == "" {
						tag = field.Name
					}
					if name == tag {
						child = field.Type
						break
					}
				}
				if child == nil {
					return fmt.Errorf("unknown JSON field %q (field names are case-sensitive)", name)
				}
			}
		}
		if err := uniqueKeys(decoder, child); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

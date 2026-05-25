package mapstruct

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type testConfig struct {
	Name     string         `json:"name"`
	Count    int            `json:"count"`
	Enabled  bool           `json:"enabled"`
	Labels   []string       `json:"labels"`
	Nested   testNested     `json:"nested"`
	Metadata map[string]int `json:"metadata"`
}

type testNested struct {
	Path string `json:"path"`
}

func TestMap2Struct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   map[string]any
		want testConfig
	}{
		{
			name: "maps scalar fields by json tags",
			in: map[string]any{
				"name":    "primary",
				"count":   3,
				"enabled": true,
			},
			want: testConfig{
				Name:    "primary",
				Count:   3,
				Enabled: true,
			},
		},
		{
			name: "maps slices nested structs and maps",
			in: map[string]any{
				"name":   "with nested values",
				"labels": []string{"fast", "local"},
				"nested": map[string]any{
					"path": "/tmp/project",
				},
				"metadata": map[string]any{
					"attempts": 2,
				},
			},
			want: testConfig{
				Name:   "with nested values",
				Labels: []string{"fast", "local"},
				Nested: testNested{
					Path: "/tmp/project",
				},
				Metadata: map[string]int{
					"attempts": 2,
				},
			},
		},
		{
			name: "ignores unknown fields",
			in: map[string]any{
				"name":    "known",
				"unknown": "ignored",
			},
			want: testConfig{
				Name: "known",
			},
		},
		{
			name: "nil map returns zero value",
			in:   nil,
			want: testConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Map2Struct[testConfig](tt.in)
			if err != nil {
				t.Fatalf("Map2Struct() error = %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Map2Struct() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestMap2StructErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      map[string]any
		wantErr error
	}{
		{
			name: "returns marshal error for unsupported value",
			in: map[string]any{
				"name": func() {},
			},
			wantErr: &json.UnsupportedTypeError{},
		},
		{
			name: "returns unmarshal error for incompatible field type",
			in: map[string]any{
				"count": "not a number",
			},
			wantErr: &json.UnmarshalTypeError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Map2Struct[testConfig](tt.in)
			if err == nil {
				t.Fatalf("Map2Struct() error = nil, got value %#v", got)
			}

			if !matchesError(err, tt.wantErr) {
				t.Fatalf("Map2Struct() error = %T %[1]v, want %T", err, tt.wantErr)
			}
		})
	}
}

func matchesError(err error, target error) bool {
	switch target.(type) {
	case *json.UnsupportedTypeError:
		var targetErr *json.UnsupportedTypeError
		return errors.As(err, &targetErr)
	case *json.UnmarshalTypeError:
		var targetErr *json.UnmarshalTypeError
		return errors.As(err, &targetErr)
	default:
		return errors.Is(err, target)
	}
}

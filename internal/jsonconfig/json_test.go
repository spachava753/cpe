package jsonconfig_test

import (
	"reflect"
	"testing"

	"github.com/spachava753/cpe/internal/jsonconfig"
)

func TestDecode(t *testing.T) {
	type child struct {
		Name string `json:"name"`
	}
	type configuration struct {
		Name     string         `json:"name"`
		Child    child          `json:"child"`
		Children []child        `json:"children"`
		Labels   map[string]int `json:"labels"`
	}
	for _, test := range []struct {
		name    string
		input   string
		want    configuration
		wantErr bool
	}{
		{name: "empty object", input: `{}`},
		{name: "whitespace", input: " \n {\"name\":\"example\"} \t", want: configuration{Name: "example"}},
		{name: "nested objects and arrays", input: `{"name":"root","child":{"name":"nested"},"children":[{"name":"first"},{"name":"second"}],"labels":{"one":1}}`, want: configuration{Name: "root", Child: child{Name: "nested"}, Children: []child{{Name: "first"}, {Name: "second"}}, Labels: map[string]int{"one": 1}}},
		{name: "empty input", input: "", wantErr: true},
		{name: "whitespace only", input: " \n ", wantErr: true},
		{name: "null", input: `null`, wantErr: true},
		{name: "array", input: `[]`, wantErr: true},
		{name: "scalar", input: `42`, wantErr: true},
		{name: "unknown field", input: `{"typo":1}`, wantErr: true},
		{name: "nested unknown field", input: `{"child":{"typo":1}}`, wantErr: true},
		{name: "wrong type", input: `{"name":1}`, wantErr: true},
		{name: "duplicate key", input: `{"name":"first","name":"second"}`, wantErr: true},
		{name: "escaped duplicate key", input: `{"name":"first","\u006eame":"second"}`, wantErr: true},
		{name: "nested duplicate key", input: `{"child":{"name":"first","name":"second"}}`, wantErr: true},
		{name: "duplicate within array element", input: `{"children":[{"name":"first","name":"second"}]}`, wantErr: true},
		{name: "duplicate map key", input: `{"labels":{"one":1,"one":2}}`, wantErr: true},
		{name: "second object", input: `{} {}`, wantErr: true},
		{name: "trailing null", input: `{} null`, wantErr: true},
		{name: "trailing garbage", input: `{} garbage`, wantErr: true},
		{name: "truncated object", input: `{"child":`, wantErr: true},
		{name: "truncated array", input: `{"children":[`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got configuration
			err := jsonconfig.Decode([]byte(test.input), &got)
			if (err != nil) != test.wantErr {
				t.Fatalf("Decode() error = %v, want error %t", err, test.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Decode() = %#v, want %#v", got, test.want)
			}
		})
	}
}

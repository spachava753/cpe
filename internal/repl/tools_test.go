package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/spachava753/cpe/internal/session"
)

func TestCompileToolSchema(t *testing.T) {
	for _, test := range []struct {
		name    string
		schema  *jsonschema.Schema
		input   string
		wantErr bool
	}{
		{"arbitrary precision integer", &jsonschema.Schema{Type: "integer"}, `99999999999999999999999999999999999999`, false},
		{"fraction is not integer", &jsonschema.Schema{Type: "integer"}, `1.25`, true},
		{"number is not string", &jsonschema.Schema{Type: "string"}, `9007199254740993`, true},
		{"string rules ignore numbers", &jsonschema.Schema{Type: "number", Pattern: "^z$"}, `9007199254740993`, false},
		{"large odd integer", &jsonschema.Schema{Type: "integer", MultipleOf: new(2.0)}, `9007199254740993`, true},
		{"exact enum", &jsonschema.Schema{Enum: []any{json.Number("9007199254740993")}}, `9007199254740993`, false},
		{"nearby enum mismatch", &jsonschema.Schema{Enum: []any{json.Number("9007199254740993")}}, `9007199254740992`, true},
		{"internal reference", &jsonschema.Schema{Defs: map[string]*jsonschema.Schema{"integer": {Type: "integer"}}, Ref: "#/$defs/integer"}, `9007199254740993`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema, err := compileToolSchema(test.schema)
			if err != nil {
				t.Fatal(err)
			}
			var input any
			decoder := json.NewDecoder(strings.NewReader(test.input))
			decoder.UseNumber()
			if err := decoder.Decode(&input); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(input); (err != nil) != test.wantErr {
				t.Fatalf("Validate(%s) = %v, want error %t", test.input, err, test.wantErr)
			}
		})
	}
	for _, ref := range []string{"file:///tmp/schema.json", "https://example.invalid/schema.json"} {
		t.Run("external reference "+ref, func(t *testing.T) {
			if _, err := compileToolSchema(&jsonschema.Schema{Ref: ref}); err == nil || !strings.Contains(err.Error(), "no URLLoader") {
				t.Fatalf("external loader enabled: %v", err)
			}
		})
	}
}

func TestREPLMakeTools(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "new history"
		if legacy {
			name = "mixed legacy and lossless history"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if legacy {
				id, err := store.Append("eval_start", evalStart{CallID: "legacy", Code: `load("tools.star", "echo"); old = echo(integer=9007199254740993)`})
				if err != nil {
					t.Fatal(err)
				}
				call, err := store.Append("host_call", hostCall{Name: "tool.echo", Args: json.RawMessage(`{"integer":9007199254740992}`)})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.Append("host_result", hostResult{CallID: call, Value: json.RawMessage(`{"integer":9007199254740992}`)}); err != nil {
					t.Fatal(err)
				}
				if _, err := store.Append("eval_end", Result{EvalID: id, CallID: "legacy", Committed: true}); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			tool := Tool{Name: "echo", Schema: &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{"integer": {Type: "integer"}, "nested": {Type: "object"}}, Required: []string{"integer"}}, Execute: func(_ context.Context, input map[string]any) (any, error) {
				calls++
				if input["integer"] != json.Number("9007199254740993") {
					return nil, fmt.Errorf("integer lost precision/type: %#v", input["integer"])
				}
				nested, ok := input["nested"].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("nested object = %#v", input["nested"])
				}
				items, ok := nested["items"].([]any)
				if !ok || len(items) != 2 || items[0] != json.Number("-9007199254740993") || items[1] != json.Number("1.25") {
					return nil, fmt.Errorf("nested numbers = %#v", nested)
				}
				return input, nil
			}}
			opts := Options{Store: store, CWD: dir, Tools: []Tool{tool}}
			r, err := New(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			if calls != 0 {
				t.Fatal("legacy host call replayed")
			}
			code := `load("tools.star", "echo")
value = echo(integer=9007199254740993, nested={"items": [-9007199254740993, 1.25]})
print(value["integer"], value["nested"]["items"][0])`
			for _, phase := range []string{"first", "after rollback", "after reopen", "after branch"} {
				if phase == "after rollback" {
					result, err := r.Eval(t.Context(), "failed", `fail("rollback")`)
					if err != nil || result.Error == "" {
						t.Fatalf("rollback: %+v, %v", result, err)
					}
				}
				if phase == "after reopen" || phase == "after branch" {
					if err := r.Close(); err != nil {
						t.Fatal(err)
					}
					if phase == "after branch" {
						for _, entry := range store.Entries() {
							if entry.Type == "checkpoint" {
								if err := store.Branch(entry.ID); err != nil {
									t.Fatal(err)
								}
								break
							}
						}
					}
					before := calls
					r, err = New(t.Context(), opts)
					if err != nil {
						t.Fatal(err)
					}
					if calls != before {
						t.Fatal("reopen repeated host calls")
					}
				}
				result, err := r.Eval(t.Context(), phase, code)
				if err != nil || result.Error != "" || result.Output != "9007199254740993 -9007199254740993\n" {
					t.Fatalf("%s: %+v, %v", phase, result, err)
				}
				if phase == "first" {
					if _, err := store.Append("checkpoint", struct{}{}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if calls != 4 {
				t.Fatalf("host calls = %d, want 4", calls)
			}
			for _, entry := range store.Entries() {
				if entry.Type != "eval_start" {
					continue
				}
				var start evalStart
				if err := json.Unmarshal(entry.Data, &start); err != nil {
					t.Fatal(err)
				}
				if start.CallID != "legacy" && start.ToolArguments != losslessToolArguments {
					t.Fatalf("new evaluation used legacy encoding: %+v", start)
				}
			}
		})
	}
	t.Run("unknown argument format fails before execution", func(t *testing.T) {
		dir := t.TempDir()
		store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, Runtime)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if _, err := store.Append("eval_start", evalStart{CallID: "unknown", Code: `load("tools.star", "effect"); effect()`, ToolArguments: 99}); err != nil {
			t.Fatal(err)
		}
		before := len(store.Entries())
		tool := Tool{Name: "effect", Execute: func(context.Context, map[string]any) (any, error) {
			t.Error("unknown format executed host effect")
			return nil, nil
		}}
		if r, err := New(t.Context(), Options{Store: store, CWD: dir, Tools: []Tool{tool}}); err == nil {
			r.Close()
			t.Fatal("unknown format accepted")
		}
		if len(store.Entries()) != before {
			t.Fatal("unknown format modified history")
		}
	})
}

package opencodego

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/testutil/testgate"
)

const fixtureCatalog = `{"opencode-go":{"npm":"@ai-sdk/openai-compatible","models":{
 "chat":{"tool_call":true,"limit":{"context":200000,"output":32768}},
 "messages":{"provider":{"npm":"@ai-sdk/anthropic"},"tool_call":true,"limit":{"context":200000,"input":100000,"output":8192}},
 "responses":{"provider":{"npm":"@ai-sdk/openai"},"tool_call":true,"limit":{"context":32000,"output":16384}},
 "unknown":{"provider":{"npm":"unknown"},"tool_call":true,"limit":{"context":100000,"output":8000}},
 "no-tools":{"tool_call":false,"limit":{"context":100000,"output":8000}},
 "no-limits":{"tool_call":true},
 "unavailable":{"tool_call":true,"limit":{"context":200000,"output":32000}},
 "retired":{"status":"deprecated","tool_call":true,"limit":{"context":200000,"output":32000}}
 }},
 "opencode":{"npm":"@ai-sdk/anthropic","models":{
   "chat":{"tool_call":true,"limit":{"context":1000000,"output":64000}},
   "uncatalogued":{"tool_call":true,"limit":{"context":1000000,"output":64000}}
 }},
 "openai":{"npm":"@ai-sdk/openai","models":{
   "uncatalogued":{"tool_call":true,"limit":{"context":1000000,"output":64000}}
 }}}`
const fixtureModels = `{"data":[{"id":"chat"},{"id":"messages"},{"id":"responses"},{"id":"unknown"},{"id":"no-tools"},{"id":"no-limits"},{"id":"uncatalogued"},{"id":"retired"}]}`
const fixtureConfig = `{"default_model":"mine","models":{"mine":{"provider":"codex","id":"fixture"}},"agent":{"tool_timeout":"2m"}}`

func TestDiscover(t *testing.T) {
	standard := map[string]config.Model{
		"opencode-go/chat":      {Provider: "openai", ID: "chat", Credential: "opencode-go", ContextWindow: 128000, MaxOutputTokens: 16384},
		"opencode-go/messages":  {Provider: "anthropic", ID: "messages", Credential: "opencode-go", ContextWindow: 100000, MaxOutputTokens: 8192},
		"opencode-go/responses": {Provider: "responses", ID: "responses", Credential: "opencode-go", ContextWindow: 16000, MaxOutputTokens: 16000},
	}
	type discoverCase struct {
		name, models, catalog string
		status                int
		wantErr               bool
		want                  map[string]config.Model
	}
	tests := []discoverCase{
		{name: "Go intersection excludes retired and other-provider models", models: fixtureModels, catalog: fixtureCatalog, want: standard},
		{name: "irrelevant provider data is not decoded", models: fixtureModels, catalog: strings.Replace(fixtureCatalog, `"opencode":{`, `"future-provider":{"models":false},"opencode":{`, 1), want: standard},
		{name: "empty models", models: `{"data":[]}`, catalog: fixtureCatalog, wantErr: true},
		{name: "missing Go section never falls back to Zen", models: fixtureModels, catalog: strings.Replace(fixtureCatalog, `"opencode-go"`, `"another-provider"`, 1), wantErr: true},
		{name: "other-provider metadata cannot fill a missing Go model", models: `{"data":[{"id":"uncatalogued"}]}`, catalog: fixtureCatalog, wantErr: true},
		{name: "unknown protocol only", models: `{"data":[{"id":"unknown"}]}`, catalog: fixtureCatalog, wantErr: true},
		{name: "deprecated model still in endpoint is excluded", models: `{"data":[{"id":"retired"}]}`, catalog: fixtureCatalog, wantErr: true},
		{name: "bad models JSON", models: `{`, catalog: fixtureCatalog, wantErr: true},
		{name: "bad catalog JSON", models: fixtureModels, catalog: `null trailing`, wantErr: true},
		{name: "catalog unavailable", models: fixtureModels, catalog: fixtureCatalog, status: 503, wantErr: true},
	}
	for _, route := range []struct{ id, provider string }{
		{"qwen3.6-plus", "anthropic"},
		{"qwen3.7-plus", "anthropic"},
		{"qwen3.7-max", "anthropic"},
		{"qwen3.8-max", "anthropic"},
		{"qwen-future-fixture", "openai"}, // Overrides must never match vendor prefixes.
	} {
		tests = append(tests, discoverCase{
			name:    "Go protocol for " + route.id,
			models:  fmt.Sprintf(`{"data":[{"id":%q}]}`, route.id),
			catalog: fmt.Sprintf(`{"opencode-go":{"npm":"@ai-sdk/openai-compatible","models":{%q:{"tool_call":true,"limit":{"context":200000,"output":32000}}}}}`, route.id),
			want:    map[string]config.Model{"opencode-go/" + route.id: {Provider: route.provider, ID: route.id, Credential: "opencode-go", ContextWindow: 128000, MaxOutputTokens: 16384}},
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" {
					t.Error("key sent to public catalog")
				}
				if !strings.HasPrefix(r.Header.Get("User-Agent"), "cpe/") {
					t.Error("missing CPE identity")
				}
				if test.status != 0 {
					w.WriteHeader(test.status)
					return
				}
				if r.URL.Path == "/models" {
					fmt.Fprint(w, test.models)
				} else {
					fmt.Fprint(w, test.catalog)
				}
			}))
			defer server.Close()
			got, err := discover(t.Context(), server.Client(), server.URL+"/models", server.URL+"/catalog")
			if (err != nil) != test.wantErr {
				t.Fatalf("discover error: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %+v want %+v", got, test.want)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	for _, test := range []struct {
		name                                             string
		badCatalog, badConfig, badKey, badSave, canceled bool
	}{
		{name: "success and repeat import"},
		{name: "catalog failure", badCatalog: true},
		{name: "invalid config", badConfig: true},
		{name: "invalid key", badKey: true},
		{name: "key save failure", badSave: true},
		{name: "cancellation", canceled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			body := fixtureConfig
			if test.badConfig {
				body = `{"models":`
			}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("test instructions"), 0600); err != nil {
				t.Fatal(err)
			}
			if test.badSave {
				if err := os.Mkdir(filepath.Join(dir, credentialFile), 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := saveKey(t.Context(), dir, "old-key"); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("login sent a key to public catalog")
				}
				if test.badCatalog {
					w.WriteHeader(503)
					return
				}
				if r.URL.Path == "/models" {
					fmt.Fprint(w, fixtureModels)
				} else {
					fmt.Fprint(w, fixtureCatalog)
				}
			}))
			defer server.Close()
			key := "new-secret-key"
			if test.badKey {
				key = "bad key"
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.canceled {
				cancel()
			}
			got, err := login(ctx, dir, key, server.Client(), server.URL+"/models", server.URL+"/catalog")
			failed := test.badCatalog || test.badConfig || test.badKey || test.badSave || test.canceled
			if (err != nil) != failed {
				t.Fatalf("login error: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), key) {
				t.Fatal("key in login error")
			}
			data, readErr := os.ReadFile(filepath.Join(dir, "config.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if failed {
				if !test.badSave {
					if string(data) != body {
						t.Fatal("failed preparation changed config")
					}
					old, err := readKey(dir)
					if err != nil || old != "old-key" {
						t.Fatal("failed preparation changed credentials")
					}
				} else if !strings.Contains(string(data), "opencode-go/chat") {
					t.Fatal("expected recoverable imported profiles after key save failure")
				}
				return
			}
			if len(got) != 4 || got["mine"].Provider != "codex" || strings.Contains(string(data), key) {
				t.Fatal("unexpected imported profiles or key in config")
			}
			saved, err := readKey(dir)
			if err != nil || saved != key {
				t.Fatal("key not saved", err)
			}
			again, err := login(ctx, dir, "replacement-key", server.Client(), server.URL+"/models", server.URL+"/catalog")
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Fatal("repeat import changed profiles", err)
			}
			after, err := os.ReadFile(filepath.Join(dir, "config.json"))
			if err != nil || string(after) != string(data) {
				t.Fatal("repeat login rewrote config")
			}
		})
	}
}

func TestPublicCatalog(t *testing.T) {
	testgate.RequireLive(t)
	client := &http.Client{Timeout: 30 * time.Second}
	profiles, err := discover(t.Context(), client, modelsURL, catalogURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("discovered %d supported profiles without credentials", len(profiles))
}

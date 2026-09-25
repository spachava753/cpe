package opencodego

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveKey(t *testing.T) {
	for _, test := range []struct {
		name, setup, key  string
		canceled, wantErr bool
	}{
		{name: "new", key: "fixture-key"},
		{name: "replace", setup: "old", key: "fixture-key"},
		{name: "empty", key: "", wantErr: true},
		{name: "whitespace", key: "secret value", wantErr: true},
		{name: "header injection", key: "secret\r\nHeader:value", wantErr: true},
		{name: "cancel", setup: "old", key: "fixture-key", canceled: true, wantErr: true},
		{name: "symlink", setup: "symlink", key: "fixture-key", wantErr: true},
		{name: "directory", setup: "directory", key: "fixture-key", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, credentialFile)
			switch test.setup {
			case "old":
				if err := os.WriteFile(path, []byte(`{"opencode-go":{"key":"old-key"}}`), 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(dir, "target")
				if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skip(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.canceled {
				cancel()
			}
			err := saveKey(ctx, dir, test.key)
			if (err != nil) != test.wantErr {
				t.Fatalf("save error = %v", err)
			}
			if !test.wantErr {
				got, err := readKey(t.Context(), dir)
				if err != nil || got != test.key || !HasKey(dir) {
					t.Fatal("saved key unavailable", err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
					t.Fatalf("permissions %v", info.Mode())
				}
			} else if test.setup == "old" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != `{"opencode-go":{"key":"old-key"}}` {
					t.Fatal("previous key changed", err)
				}
			} else if test.setup == "symlink" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "untouched" {
					t.Fatal("symlink target changed", err)
				}
			}
			matches, err := filepath.Glob(filepath.Join(dir, ".cpe-auth-*"))
			if err != nil || len(matches) > 0 {
				t.Fatal("temporary secret file left behind")
			}
		})
	}
}

func TestReadKey(t *testing.T) {
	for _, test := range []struct {
		name, body, current string
		legacy, wantErr     bool
	}{
		{name: "valid", body: `{"key":"fixture-secret"}`},
		{name: "missing", wantErr: true},
		{name: "null", body: `null`, wantErr: true},
		{name: "empty", body: `{"key":""}`, wantErr: true},
		{name: "whitespace", body: `{"key":"fixture-secret value"}`, wantErr: true},
		{name: "wrong type", body: `{"key":{"fixture-secret":true}}`, wantErr: true},
		{name: "malformed", body: `{"key":"fixture-secret`, wantErr: true},
		{name: "duplicate", body: `{"key":"first","key":"fixture-secret"}`, wantErr: true},
		{name: "migrate standalone key", body: `{"key":"fixture-secret"}`, legacy: true},
		{name: "reject malformed legacy", body: `{"key":"fixture-secret`, legacy: true, wantErr: true},
		{name: "reject invalid legacy", body: `{"key":"fixture-secret value"}`, legacy: true, wantErr: true},
		{name: "current key overrides legacy", body: `{"key":"old-secret"}`, current: `{"key":"fixture-secret"}`, legacy: true},
		{name: "invalid current never falls back", body: `{"key":"fixture-secret"}`, current: `null`, legacy: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			auth := filepath.Join(dir, credentialFile)
			path, body := auth, `{"other":{"key":"keep"},"opencode-go":`+test.body+`}`
			if test.legacy {
				path, body = filepath.Join(dir, "opencode-go.json"), test.body
				document := `{"other":{"key":"keep"}}`
				if test.current != "" {
					document = `{"other":{"key":"keep"},"opencode-go":` + test.current + `}`
				}
				if err := os.WriteFile(auth, []byte(document), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if test.body != "" {
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(auth)
			key, err := readKey(t.Context(), dir)
			if (err != nil) != test.wantErr || HasKey(dir) == test.wantErr {
				t.Fatalf("unexpected credential availability: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "fixture-secret") || key != "") {
				t.Fatal("invalid key exposed")
			}
			if err == nil && key != "fixture-secret" {
				t.Fatal("key changed")
			}
			if test.legacy {
				backup, err := os.ReadFile(path)
				if err != nil || string(backup) != body {
					t.Fatal("legacy backup changed")
				}
				data, err := os.ReadFile(auth)
				if err != nil {
					t.Fatal(err)
				}
				if test.wantErr {
					if string(data) != string(before) {
						t.Fatal("failed migration changed auth.json")
					}
				} else {
					var document map[string]credential
					if err := json.Unmarshal(data, &document); err != nil || document[credentialName].Key != key || document["other"].Key != "keep" {
						t.Fatal("migration did not preserve credentials", err)
					}
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if !HasKey(dir) {
						t.Fatal("still depends on legacy file")
					}
				}
			}
		})
	}
}

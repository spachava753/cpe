package opencodego

import (
	"context"
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
				if err := os.WriteFile(path, []byte(`{"key":"old-key"}`), 0644); err != nil {
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
				got, err := readKey(dir)
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
				if err != nil || string(data) != `{"key":"old-key"}` {
					t.Fatal("previous key changed", err)
				}
			} else if test.setup == "symlink" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "untouched" {
					t.Fatal("symlink target changed", err)
				}
			}
			matches, err := filepath.Glob(filepath.Join(dir, ".cpe-go-key-*"))
			if err != nil || len(matches) > 0 {
				t.Fatal("temporary secret file left behind")
			}
		})
	}
}

func TestReadKey(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantErr    bool
	}{
		{"valid", `{"key":"fixture-secret"}`, false},
		{"missing", "", true},
		{"null", `null`, true},
		{"empty", `{"key":""}`, true},
		{"whitespace", `{"key":"fixture-secret value"}`, true},
		{"wrong type", `{"key":{"fixture-secret":true}}`, true},
		{"malformed", `{"key":"fixture-secret`, true},
		{"duplicate", `{"key":"first","key":"fixture-secret"}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.body != "" {
				if err := os.WriteFile(filepath.Join(dir, credentialFile), []byte(test.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			key, err := readKey(dir)
			if (err != nil) != test.wantErr || HasKey(dir) == test.wantErr {
				t.Fatalf("unexpected credential availability: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "fixture-secret") || key != "") {
				t.Fatal("invalid key exposed")
			}
			if err == nil && key != "fixture-secret" {
				t.Fatal("key changed")
			}
		})
	}
}

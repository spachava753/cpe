package skills_test

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/skills"
)

func TestGitSourceClientFetch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	repository := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet", repository}, {"-C", repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--quiet", "--allow-empty", "-m", "fixture"}} {
		if output, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v, %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, "SKILL.md"), []byte(alphaSkill), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", repository, "add", "SKILL.md"}, {"-C", repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", "skill"}} {
		if output, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v, %s", args, err, output)
		}
	}
	locator := (&url.URL{Scheme: "file", Path: filepath.ToSlash(repository)}).String()
	for _, test := range []struct {
		name, source, errText string
		cancel                bool
	}{
		{name: "read-only checkout and cleanup", source: locator},
		{name: "clone failure", source: locator + "-missing", errText: "clone"},
		{name: "invalid locator", source: "not-a-source", errText: "source"},
		{name: "URL with embedded password rejected", source: "https://user:secret@example.com/skills.git", errText: "password"},
		{name: "cancelled", source: locator, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scratch := t.TempDir()
			t.Setenv("TMPDIR", scratch)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			checkout, err := (skills.GitSourceClient{}).Fetch(ctx, test.source)
			if test.cancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v", err)
				}
			} else if test.errText != "" {
				if err == nil || !strings.Contains(err.Error(), test.errText) {
					t.Fatalf("error = %v, want %q", err, test.errText)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, err := fs.ReadFile(checkout.FS, "SKILL.md")
				if err != nil || string(data) != alphaSkill {
					t.Fatalf("checkout file = %q, %v", data, err)
				}
				if checkout.Cleanup == nil {
					t.Fatal("missing cleanup")
				}
				if err := checkout.Cleanup(); err != nil {
					t.Fatal(err)
				}
				if err := checkout.Cleanup(); err != nil {
					t.Errorf("repeated cleanup = %v", err)
				}
				if data, err := os.ReadFile(filepath.Join(repository, "SKILL.md")); err != nil || string(data) != alphaSkill {
					t.Fatal("cleanup affected original repository")
				}
			}
			entries, readErr := os.ReadDir(scratch)
			if readErr != nil || len(entries) != 0 {
				t.Errorf("temporary source resources remain: %v, %v", entries, readErr)
			}
		})
	}
}

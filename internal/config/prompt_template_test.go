package config

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/cpe/internal/skills"
)

func TestSystemPromptTemplate(t *testing.T) {
	t.Run("exec preserves optional command behavior", func(t *testing.T) {
		t.Parallel()
		if _, err := exec.LookPath("bash"); err != nil {
			t.Skip("bash is required for template exec")
		}
		out, err := systemPromptTemplate(t.Context(), `{{exec "printf '  hello  '"}}/{{exec "printf ignored; exit 1"}}`, templateData{})
		if err != nil || out != "hello/" {
			t.Fatalf("render = %q, %v; want hello/ without error", out, err)
		}
	})
	t.Run("already canceled context rejects even static prompts", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		out, err := systemPromptTemplate(ctx, "static prompt", templateData{})
		if !errors.Is(err, context.Canceled) || out != "" {
			t.Fatalf("render = %q, %v; want cancellation without output", out, err)
		}
	})
	t.Run("exec has a deadline without caller cancellation", func(t *testing.T) {
		t.Parallel()
		if _, err := exec.LookPath("bash"); err != nil {
			t.Skip("bash is required for template exec")
		}
		start := time.Now()
		out, err := systemPromptTemplate(t.Context(), `{{exec "sleep 20 | cat"}}`, templateData{})
		if !errors.Is(err, context.DeadlineExceeded) || out != "" {
			t.Fatalf("render = %q, %v; want command deadline without output", out, err)
		}
		if !strings.Contains(err.Error(), "sleep 20 | cat") {
			t.Fatalf("error does not identify the blocking command: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 15*time.Second {
			t.Fatalf("command deadline took %s; want about 10s", elapsed)
		}
	})
	t.Run("cancellation fails rendering instead of returning a partial prompt", func(t *testing.T) {
		t.Parallel()
		if _, err := exec.LookPath("bash"); err != nil {
			t.Skip("bash is required for template exec")
		}

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		out, err := systemPromptTemplate(ctx, `before {{exec "sleep 2 | cat"}} after`, templateData{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want context deadline exceeded", err)
		}
		if out != "" {
			t.Fatalf("returned partial prompt %q", out)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("cancellation took %s; waited for the pipeline instead of stopping it", elapsed)
		}
	})
	t.Run("supports skill data", func(t *testing.T) {
		t.Parallel()

		tmpl := `{{- range $s := .Skills -}}{{$s.Name}}={{$s.Description}}@{{$s.Path}}:{{$s.Metadata.group}};{{- end -}}`
		out, err := systemPromptTemplate(context.Background(), tmpl, templateData{
			Skills: []skills.Skill{{
				Name:        "alpha-skill",
				Description: "Alpha description",
				Path:        "~/.agents/skills/alpha-skill",
				Metadata: map[string]any{
					"group": "alpha",
				},
			}},
		})
		if err != nil {
			t.Fatalf("SystemPromptTemplate() error = %v", err)
		}

		want := "alpha-skill=Alpha description@~/.agents/skills/alpha-skill:alpha;"
		if out != want {
			t.Fatalf("SystemPromptTemplate() mismatch\nwant: %q\n got: %q", want, out)
		}
	})
	t.Run("does not register skills helper", func(t *testing.T) {
		t.Parallel()

		_, err := systemPromptTemplate(context.Background(), `{{ skills }}`, templateData{})
		if err == nil {
			t.Fatal("SystemPromptTemplate() error is nil, want parse error")
		}
		if !strings.Contains(err.Error(), `function "skills" not defined`) {
			t.Fatalf("SystemPromptTemplate() error = %v, want missing skills function", err)
		}
	})
}

package prompt

import (
	"context"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/skills"
)

func TestSystemPromptTemplateSupportsSkillData(t *testing.T) {
	t.Parallel()

	tmpl := `{{- range $s := .Skills -}}{{$s.Name}}={{$s.Description}}@{{$s.Path}}:{{$s.Metadata.group}};{{- end -}}`
	out, err := SystemPromptTemplate(context.Background(), tmpl, TemplateData{
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
}

func TestSystemPromptTemplateDoesNotRegisterSkillsHelper(t *testing.T) {
	t.Parallel()

	_, err := SystemPromptTemplate(context.Background(), `{{ skills }}`, TemplateData{})
	if err == nil {
		t.Fatal("SystemPromptTemplate() error is nil, want parse error")
	}
	if !strings.Contains(err.Error(), `function "skills" not defined`) {
		t.Fatalf("SystemPromptTemplate() error = %v, want missing skills function", err)
	}
}

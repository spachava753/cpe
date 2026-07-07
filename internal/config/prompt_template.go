package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"github.com/spachava753/cpe/internal/skills"
)

// TemplateData is the input object exposed to system prompt templates.
// It embeds the resolved runtime configuration so templates can reference
// model and MCP settings.
type TemplateData struct {
	Config
	// Skills contains model-visible skill metadata for prompt templates. For
	// example, templates can range over .Skills and render .Name, .Description,
	// .Path, or arbitrary frontmatter through .Metadata.
	Skills []skills.Skill
}

// SystemPromptTemplate renders a template string with system info data.
func SystemPromptTemplate(ctx context.Context, templateStr string, td TemplateData) (string, error) {
	tmpl, err := template.New("sysinfo").Funcs(createTemplateFuncMap(ctx)).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template string: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

func createTemplateFuncMap(ctx context.Context) template.FuncMap {
	fm := sprig.TxtFuncMap()
	fm["fileExists"] = fileExists
	fm["includeFile"] = includeFile
	fm["exec"] = func(command string) string {
		return execCommand(ctx, command)
	}
	return fm
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func includeFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

func execCommand(ctx context.Context, command string) string {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

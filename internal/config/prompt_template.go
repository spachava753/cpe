package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"

	"github.com/spachava753/cpe/internal/skills"
)

// templateData is the input object exposed to system prompt templates.
// It embeds the resolved runtime configuration so templates can reference
// model and MCP settings.
type templateData struct {
	Config
	// Skills contains model-visible skill metadata for prompt templates. For
	// example, templates can range over .Skills and render .Name, .Description,
	// .Path, or arbitrary frontmatter through .Metadata.
	Skills []skills.Skill
}

// systemPromptTemplate renders a template string with system info data.
func systemPromptTemplate(ctx context.Context, templateStr string, td templateData) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	tmpl, err := template.New("sysinfo").Funcs(createTemplateFuncMap(ctx)).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template string: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func createTemplateFuncMap(ctx context.Context) template.FuncMap {
	fm := sprig.TxtFuncMap()
	fm["fileExists"] = fileExists
	fm["includeFile"] = includeFile
	fm["exec"] = func(command string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		cmd := templateCommand(ctx, command)
		// Descendants can inherit stdout even after the shell exits. Bound pipe
		// draining as well as command execution so they cannot stall rendering.
		cmd.WaitDelay = 100 * time.Millisecond
		output, err := cmd.Output()
		if ctx.Err() != nil {
			return "", fmt.Errorf("template command %q: %w", command, ctx.Err())
		}
		if errors.Is(err, exec.ErrWaitDelay) {
			return "", fmt.Errorf("template command %q left output pipes open: %w", command, err)
		}
		if err != nil {
			return "", nil
		}
		return strings.TrimSpace(string(output)), nil
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

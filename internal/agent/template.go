package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/spachava753/cpe/internal/config"
)

type TemplateData struct {
	*config.Config
}

// SystemPromptTemplate renders a template string with system info data
func SystemPromptTemplate(templateStr string, td TemplateData) (string, error) {
	tmpl, err := template.New("sysinfo").Funcs(createTemplateFuncMap()).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template string: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// createTemplateFuncMap returns the FuncMap for system prompt templates
func createTemplateFuncMap() template.FuncMap {
	// Start with sprig's rich set of template functions
	fm := sprig.TxtFuncMap()
	// Add/override with our custom helpers
	fm["fileExists"] = fileExists
	fm["includeFile"] = includeFile
	fm["exec"] = execCommand
	return fm
}

// fileExists checks if a file exists and is readable
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// includeFile reads and returns the contents of a file
func includeFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

// execCommand executes a bash command and returns stdout
func execCommand(command string) string {
	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

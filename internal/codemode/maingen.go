package codemode

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

//go:embed maingen.go.tmpl
var mainTemplateSource string

// GenerateMainGo renders the sandbox runner used to execute model-authored run.go.
// The generated source calls Run(ctx) and serializes optional multimedia output
// to contentOutputPath.
func GenerateMainGo(contentOutputPath string) (string, error) {
	tmpl, err := template.New("main.go").Funcs(template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}).Parse(mainTemplateSource)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ ContentOutputPath string }{ContentOutputPath: contentOutputPath}); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}

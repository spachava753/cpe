package cli

import (
	"regexp"
	"testing"

	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"

	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/toolruntime"
)

// TestSystemPromptExamples runs every Python example in the default prompt
// through the real starlark_exec tool, so the prompt cannot drift from the runtime.
func TestSystemPromptExamples(t *testing.T) {
	examples := regexp.MustCompile("(?s)```python\n(.*?)\n```").FindAllStringSubmatch(
		defaultSystemPrompt,
		-1,
	)
	if len(examples) == 0 {
		t.Fatal("no runnable examples")
	}
	catalog, err := mcpclient.NewCatalog(
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	host, err := toolruntime.New(
		t.Context(),
		toolruntime.Config{
			Cwd:     t.TempDir(),
			TempDir: t.TempDir(),
			Catalog: catalog,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range examples {
		t.Run(
			example[1],
			func(t *testing.T) {
				for _, tool := range host.Tools() {
					if tool.Definition.Name != toolruntime.StarlarkToolName {
						continue
					}
					result, err := tool.Handler.Execute(
						t.Context(),
						gaiagent.ToolRequest{
							Block: gai.Block{
								ID: "example",
							},
							Call: gai.ToolCallInput{
								Name: toolruntime.StarlarkToolName,
								Parameters: map[string]any{
									"code": example[1],
								},
							},
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					if result.ToolResultError {
						t.Fatalf(
							"example failed: %s",
							result.Blocks[0].Content.String(),
						)
					}
				}
			},
		)
	}
}

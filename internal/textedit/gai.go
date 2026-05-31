package textedit

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"
)

const toolDescription = "Create a new text file or replace exactly one occurrence of text in an existing UTF-8 file."

// MakeTool returns the text_edit tool definition and callback for direct gai registration.
func MakeTool() (gai.Tool, gai.ToolCallback) {
	tool := gai.Tool{
		Name:        ToolName,
		Description: toolDescription,
		InputSchema: inputSchema(),
	}
	callback := gai.ToolCallBackFunc[Input](func(ctx context.Context, input Input) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", gai.CallbackExecErr{Err: err}
		}

		output, err := Apply(input)
		if err != nil {
			return "", err
		}
		return output.Message(), nil
	})
	return tool, callback
}

func inputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"path": {
				Type:        "string",
				Description: "Path to the file to edit or create.",
			},
			"old_text": {
				Type:        "string",
				Description: "Exact text to find and replace. If empty, creates a new file instead.",
			},
			"new_text": {
				Type:        "string",
				Description: "Replacement text or content for new file.",
			},
		},
		Required: []string{"path", "new_text"},
	}
}

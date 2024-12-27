package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/generative-ai-go/genai"
	gitignore "github.com/sabhiram/go-gitignore"
	"google.golang.org/api/option"
	"log/slog"
	"time"
)

type geminiExecutor struct {
	model   *genai.GenerativeModel
	logger  *slog.Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
}

func NewGeminiExecutor(baseUrl string, apiKey string, logger *slog.Logger, ignorer *gitignore.GitIgnore, config GenConfig) (Executor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	opts := []option.ClientOption{option.WithAPIKey(apiKey)}
	if baseUrl != "" {
		opts = append(opts, option.WithEndpoint(baseUrl))
	}

	client, err := genai.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("error creating Gemini client: %w", err)
	}

	model := client.GenerativeModel(config.Model)
	model.SetTemperature(config.Temperature)
	model.SetMaxOutputTokens(int32(config.MaxTokens))

	if config.TopK != nil {
		model.SetTopK(int32(*config.TopK))
	}
	if config.TopP != nil {
		model.SetTopP(*config.TopP)
	}

	// Set up tools
	model.Tools = []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        bashTool.Name,
					Description: bashTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"command": {
								Type:        genai.TypeString,
								Description: "The bash command to run.",
							},
						},
						Required: []string{"command"},
					},
				},
				{
					Name:        fileEditor.Name,
					Description: fileEditor.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"command": {
								Type:        genai.TypeString,
								Enum:        []string{"create", "str_replace", "remove"},
								Description: `The commands to run. Allowed options are: "create", "create", "str_replace", "remove".`,
							},
							"file_text": {
								Type:        genai.TypeString,
								Description: `Required parameter of "create" command, with the content of the file to be created.`,
							},
							"new_str": {
								Type:        genai.TypeString,
								Description: `Required parameter of "str_replace" command containing the new string.`,
							},
							"old_str": {
								Type:        genai.TypeString,
								Description: `Required parameter of "str_replace" command containing the string in "path" to replace.`,
							},
							"path": {
								Type:        genai.TypeString,
								Description: `Relative path to file or directory, e.g. "./file.py"`,
							},
						},
						Required: []string{"command", "path"},
					},
				},
				{
					Name:        filesOverviewTool.Name,
					Description: filesOverviewTool.Description,
				},
				{
					Name:        getRelatedFilesTool.Name,
					Description: getRelatedFilesTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"input_files": {
								Type:        genai.TypeArray,
								Description: `An array of input files to retrieve related files, e.g. source code files that have symbol definitions in another file or other files that mention the file's name.'`,
								Items: &genai.Schema{
									Type: genai.TypeString,
								},
							},
						},
						Required: []string{"input_files"},
					},
				},
			},
		},
	}

	// Set system prompt
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(agentInstructions)},
	}

	return &geminiExecutor{
		model:   model,
		logger:  logger,
		ignorer: ignorer,
		config:  config,
	}, nil
}

func (g *geminiExecutor) Execute(input string) error {
	session := g.model.StartChat()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Send initial user message
	resp, err := session.SendMessage(ctx, genai.Text(input))
	if err != nil {
		return fmt.Errorf("error sending message to Gemini: %w", err)
	}

	for {
		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			return fmt.Errorf("no response generated")
		}

		finished := true
		var nextMsg []genai.Part

		for _, part := range resp.Candidates[0].Content.Parts {
			switch v := part.(type) {
			case genai.Text:
				if len(v) == 0 {
					continue
				}
				g.logger.Info(string(v))
			case genai.FunctionCall:
				finished = false
				g.logger.Info(fmt.Sprintf("%+v", v.Args))

				var result *ToolResult
				switch v.Name {
				case bashTool.Name:
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						panic(marshalErr)
					}
					result, err = executeBashTool(jsonInput, g.logger)
				case fileEditor.Name:
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						panic(marshalErr)
					}
					result, err = executeFileEditorTool(jsonInput, g.logger)
				case filesOverviewTool.Name:
					result, err = executeFilesOverviewTool(g.ignorer, g.logger)
				case getRelatedFilesTool.Name:
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						panic(marshalErr)
					}
					result, err = executeGetRelatedFilesTool(jsonInput, g.ignorer, g.logger)
				default:
					return fmt.Errorf("unexpected tool name: %s", v.Name)
				}

				if err != nil {
					return fmt.Errorf("failed to execute tool %s: %w", v.Name, err)
				}

				// Convert tool result to function response
				var response map[string]any
				switch content := result.Content.(type) {
				case string:
					response = map[string]any{"result": content}
				case map[string]interface{}:
					response = content
				default:
					panic("unexpected type")
				}
				if result.IsError {
					response["error"] = true
				}

				nextMsg = append(nextMsg, genai.FunctionResponse{
					Name:     v.Name,
					Response: response,
				})
			}
		}

		if finished {
			break
		}

		// Send next message
		resp, err = session.SendMessage(ctx, nextMsg...)
		if err != nil {
			return fmt.Errorf("error sending message to Gemini: %w", err)
		}
	}

	return nil
}

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/generative-ai-go/genai"
	gitignore "github.com/sabhiram/go-gitignore"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"log/slog"
	"strconv"
	"time"
)

// unescapeString unescapes a string that contains escaped characters like quotes and whitespace
func unescapeString(s string) string {
	// First try to unescape as a JSON string
	unquoted, err := strconv.Unquote(`"` + s + `"`)
	if err == nil {
		return unquoted
	}
	// If that fails, return the original string
	return s
}

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
								Description: `Required parameter of "str_replace" command containing the new string. The contents of this parameter does NOT need to be escaped.`,
							},
							"old_str": {
								Type:        genai.TypeString,
								Description: `Required parameter of "str_replace" command containing the string in "path" to replace. The contents of this parameter does NOT need to be escaped.`,
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

	// Send initial user message with retries
	var resp *genai.GenerateContentResponse
	var err error
	maxRetries := 5
	retryCount := 0
	retryWait := 1 * time.Minute

	for retryCount <= maxRetries {
		resp, err = session.SendMessage(ctx, genai.Text(input))
		if err == nil {
			break
		}

		var gerr *googleapi.Error
		if errors.As(err, &gerr) && (gerr.Code == 429 || gerr.Code >= 500) {
			retryCount++
			if retryCount > maxRetries {
				return fmt.Errorf("exceeded maximum retries (%d) when sending message to Gemini: %w", maxRetries, err)
			}
			g.logger.Info("retrying Gemini API call due to error",
				slog.Int("retry", retryCount),
				slog.Int("status_code", gerr.Code),
				slog.String("error", gerr.Error()),
			)
			// Remove the failed message from session history before retrying
			if len(session.History) > 0 {
				session.History = session.History[:len(session.History)-1]
			}
			time.Sleep(retryWait)
			continue
		}
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
					var params struct {
						Command string `json:"command"`
					}
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal bash tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &params); err != nil {
						return fmt.Errorf("failed to unmarshal bash tool arguments: %w", err)
					}
					result, err = executeBashTool(params.Command, g.logger)
				case fileEditor.Name:
					var params FileEditorParams
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal file editor tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &params); err != nil {
						return fmt.Errorf("failed to unmarshal file editor tool arguments: %w", err)
					}
					// Unescape old_str and new_str parameters
					params.OldStr = unescapeString(params.OldStr)
					params.NewStr = unescapeString(params.NewStr)
					result, err = executeFileEditorTool(params, g.logger)
				case filesOverviewTool.Name:
					result, err = executeFilesOverviewTool(g.ignorer, g.logger)
				case getRelatedFilesTool.Name:
					var params struct {
						InputFiles []string `json:"input_files"`
					}
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal get related files tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &params); err != nil {
						return fmt.Errorf("failed to unmarshal get related files tool arguments: %w", err)
					}
					result, err = executeGetRelatedFilesTool(params.InputFiles, g.ignorer, g.logger)
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

		// Send next message with retries
		retryCount = 0
		for retryCount <= maxRetries {
			resp, err = session.SendMessage(ctx, nextMsg...)
			if err == nil {
				break
			}

			var gerr *googleapi.Error
			if errors.As(err, &gerr) && (gerr.Code == 429 || gerr.Code >= 500) {
				retryCount++
				if retryCount > maxRetries {
					return fmt.Errorf("exceeded maximum retries (%d) when sending message to Gemini: %w", maxRetries, err)
				}
				g.logger.Info("retrying Gemini API call due to error",
					slog.Int("retry", retryCount),
					slog.Int("status_code", gerr.Code),
					slog.String("error", gerr.Error()),
				)
				// Remove the failed message from session history before retrying
				if len(session.History) > 0 {
					session.History = session.History[:len(session.History)-1]
				}
				time.Sleep(retryWait)
				continue
			}
			return fmt.Errorf("error sending message to Gemini: %w", err)
		}
	}

	return nil
}

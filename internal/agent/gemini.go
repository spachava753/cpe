package agent

import (
	"context"
	_ "embed"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/generative-ai-go/genai"
	gitignore "github.com/sabhiram/go-gitignore"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"io"
	"os"
	"strings"
	"time"
)

func init() {
	// Register Gemini types with gob
	gob.Register(&genai.ChatSession{})
	gob.Register([]*genai.Content{})
	gob.Register([]genai.Part{})
	gob.Register(genai.Text(""))
	gob.Register(genai.FunctionCall{})
	gob.Register(genai.FunctionResponse{})
	gob.Register(map[string]interface{}{})
	gob.Register(genai.Blob{}) // Add this line
}

type geminiExecutor struct {
	model   *genai.GenerativeModel
	logger  Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
	session *genai.ChatSession
}

// truncateResult truncates a tool result to fit within maxTokens and returns an error message
func (g *geminiExecutor) truncateResult(result string) (string, error) {
	const maxTokens = 50000

	// Count tokens using model's API
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := g.model.CountTokens(ctx, genai.Text(result))
	if err != nil {
		return result, fmt.Errorf("error counting tokens: %w", err)
	}
	tokens := int(resp.TotalTokens)

	if tokens <= maxTokens {
		return result, nil
	}

	// Truncate by ratio
	ratio := float64(maxTokens) / float64(tokens)
	truncLen := int(float64(len(result)) * ratio)
	truncated := result[:truncLen] + "\n...[truncated]..."

	return truncated, nil
}

func NewGeminiExecutor(baseUrl string, apiKey string, logger Logger, ignorer *gitignore.GitIgnore, config GenConfig) (Executor, error) {
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
				// Basic tools
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
				// Overview and analysis tools
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
				// Navigation tool
				{
					Name:        changeDirectoryTool.Name,
					Description: changeDirectoryTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path": {
								Type:        genai.TypeString,
								Description: "The path to change to, can be relative or absolute",
							},
						},
						Required: []string{"path"},
					},
				},
				// File operation tools
				{
					Name:        createFileTool.Name,
					Description: createFileTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path": {
								Type:        genai.TypeString,
								Description: "Relative path where the file should be created",
							},
							"file_text": {
								Type:        genai.TypeString,
								Description: "Content to write to the new file",
							},
						},
						Required: []string{"path", "file_text"},
					},
				},
				{
					Name:        deleteFileTool.Name,
					Description: deleteFileTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path": {
								Type:        genai.TypeString,
								Description: "Relative path to the file to delete",
							},
						},
						Required: []string{"path"},
					},
				},
				{
					Name:        editFileTool.Name,
					Description: editFileTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path": {
								Type:        genai.TypeString,
								Description: "Relative path to the file to edit",
							},
							"old_str": {
								Type:        genai.TypeString,
								Description: "The exact text segment to replace (must be unique in the file)",
							},
							"new_str": {
								Type:        genai.TypeString,
								Description: "The new text to replace the old text with",
							},
						},
						Required: []string{"path", "old_str", "new_str"},
					},
				},
				{
					Name:        moveFileTool.Name,
					Description: moveFileTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"source_path": {
								Type:        genai.TypeString,
								Description: "Relative path to the file to move/rename",
							},
							"target_path": {
								Type:        genai.TypeString,
								Description: "Relative path where the file should be moved/renamed to",
							},
						},
						Required: []string{"source_path", "target_path"},
					},
				},
				{
					Name:        viewFileTool.Name,
					Description: viewFileTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path": {
								Type:        genai.TypeString,
								Description: "Relative path to the file to view",
							},
						},
						Required: []string{"path"},
					},
				},
				// Folder operation tools
				{
					Name:        createFolderTool.Name,
					Description: createFolderTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path": {
								Type:        genai.TypeString,
								Description: "Relative path where the folder should be created",
							},
						},
						Required: []string{"path"},
					},
				},
				{
					Name:        deleteFolderTool.Name,
					Description: deleteFolderTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path": {
								Type:        genai.TypeString,
								Description: "Relative path to the folder to delete",
							},
							"recursive": {
								Type:        genai.TypeBoolean,
								Description: "Whether to delete non-empty folders (true) or error on non-empty folders (false)",
							},
						},
						Required: []string{"path"},
					},
				},
				{
					Name:        moveFolderTool.Name,
					Description: moveFolderTool.Description,
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"source_path": {
								Type:        genai.TypeString,
								Description: "Relative path to the folder to move/rename",
							},
							"target_path": {
								Type:        genai.TypeString,
								Description: "Relative path where the folder should be moved/renamed to",
							},
						},
						Required: []string{"source_path", "target_path"},
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

func (g *geminiExecutor) Execute(inputs []Input) error {
	if g.session == nil {
		g.session = g.model.StartChat()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Convert inputs into parts
	var parts []genai.Part
	for _, input := range inputs {
		switch input.Type {
		case InputTypeText:
			parts = append(parts, genai.Text(input.Text))
		case InputTypeImage:
			// Read image file
			imgData, err := os.ReadFile(input.FilePath)
			if err != nil {
				return fmt.Errorf("failed to read image file %s: %w", input.FilePath, err)
			}

			// Detect mime type
			mime := mimetype.Detect(imgData)
			if !strings.HasPrefix(mime.String(), "image/") {
				return fmt.Errorf("file %s is not an image", input.FilePath)
			}

			// Verify supported image type
			switch mime.String() {
			case "image/png", "image/jpeg", "image/webp", "image/heic", "image/heif":
				// These are supported
			default:
				return fmt.Errorf("unsupported image type %s for file %s. Supported types: PNG, JPEG, WEBP, HEIC, HEIF", mime.String(), input.FilePath)
			}

			// Get format without the "image/" prefix
			format := strings.TrimPrefix(mime.String(), "image/")

			// Create image part
			parts = append(parts, genai.ImageData(format, imgData))
		case InputTypeAudio:
			// Read audio file
			audioData, err := os.ReadFile(input.FilePath)
			if err != nil {
				return fmt.Errorf("failed to read audio file %s: %w", input.FilePath, err)
			}

			// Detect mime type
			mime := mimetype.Detect(audioData)
			if !strings.HasPrefix(mime.String(), "audio/") {
				return fmt.Errorf("file %s is not an audio file", input.FilePath)
			}

			// Verify supported audio type
			switch mime.String() {
			case "audio/wav", "audio/mp3", "audio/aiff", "audio/aac", "audio/ogg", "audio/flac":
				// These are supported
			default:
				return fmt.Errorf("unsupported audio type %s for file %s. Supported types: WAV, MP3, AIFF, AAC, OGG, FLAC", mime.String(), input.FilePath)
			}

			// Create audio part
			parts = append(parts, genai.Blob{
				MIMEType: mime.String(),
				Data:     audioData,
			})
		case InputTypeVideo:
			return fmt.Errorf("video input is not yet supported by this implementation")
		default:
			return fmt.Errorf("unknown input type: %s", input.Type)
		}
	}

	// Send initial message with retries
	var resp *genai.GenerateContentResponse
	var err error
	maxRetries := 5
	retryCount := 0
	retryWait := 1 * time.Minute

	for retryCount <= maxRetries {
		resp, err = g.session.SendMessage(ctx, parts...)
		if err == nil {
			break
		}

		var gerr *googleapi.Error
		if errors.As(err, &gerr) && (gerr.Code == 429 || gerr.Code >= 500) {
			retryCount++
			if retryCount > maxRetries {
				return fmt.Errorf("exceeded maximum retries (%d) when sending message to Gemini: %w", maxRetries, err)
			}
			g.logger.Printf("retrying Gemini API call due to error; retry: %d, status_code: %d, error: %s",
				retryCount,
				gerr.Code,
				gerr.Error(),
			)
			// Remove the failed message from session history before retrying
			if len(g.session.History) > 0 {
				g.session.History = g.session.History[:len(g.session.History)-1]
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
				g.logger.Println(string(v))
			case genai.FunctionCall:
				finished = false
				g.logger.Printf("Tool: %s", v.Name)

				var result *ToolResult
				switch v.Name {
				case bashTool.Name:
					var bashToolInput struct {
						Command string `json:"command"`
					}
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal bash tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &bashToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal bash tool arguments: %w", err)
					}
					g.logger.Printf("executing bash command: %s", bashToolInput.Command)
					result, err = executeBashTool(bashToolInput.Command)
					if err == nil {
						// Log full output before truncation
						g.logger.Printf("tool result: %+v", result.Content)

						resultStr := fmt.Sprintf("tool result: %+v", result.Content)

						// Check token count and truncate if necessary
						truncatedResult, err := g.truncateResult(resultStr)
						if err != nil {
							return fmt.Errorf("failed to truncate tool result: %w", err)
						}

						if truncatedResult != resultStr {
							g.logger.Println("Warning: bash output exceeded 50,000 tokens and was truncated")
						}

						result.Content = truncatedResult
					}
				// File operation tools
				case createFileTool.Name:
					var createFileToolInput CreateFileParams
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal create file tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &createFileToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal create file tool arguments: %w", err)
					}
					g.logger.Printf(
						"executing create file tool; path: %s\nfile_text:\n%s",
						createFileToolInput.Path,
						createFileToolInput.FileText,
					)
					result, err = CreateFileTool(createFileToolInput)
					if err == nil {
						g.logger.Printf("tool result: %+v", result.Content)
					}
				case editFileTool.Name:
					var editFileToolInput EditFileParams
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal edit file tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &editFileToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal edit file tool arguments: %w", err)
					}
					g.logger.Printf(
						"executing edit file tool; path: %s\nold_str:\n%s\nnew_str:\n%s",
						editFileToolInput.Path,
						editFileToolInput.OldStr,
						editFileToolInput.NewStr,
					)
					result, err = EditFileTool(editFileToolInput)
					if err == nil {
						g.logger.Printf("tool result: %+v", result.Content)
					}
				case deleteFileTool.Name:
					var deleteFileToolInput DeleteFileParams
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal delete file tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &deleteFileToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal delete file tool arguments: %w", err)
					}
					g.logger.Printf(
						"executing delete file tool; path: %s",
						deleteFileToolInput.Path,
					)
					result, err = DeleteFileTool(deleteFileToolInput)
					if err == nil {
						g.logger.Printf("tool result: %+v", result.Content)
					}
				case moveFileTool.Name:
					var moveFileToolInput MoveFileParams
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal move file tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &moveFileToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal move file tool arguments: %w", err)
					}
					g.logger.Printf(
						"executing move file tool; source_path: %s\ntarget_path: %s",
						moveFileToolInput.SourcePath,
						moveFileToolInput.TargetPath,
					)
					result, err = MoveFileTool(moveFileToolInput)
					if err == nil {
						g.logger.Printf("tool result: %+v", result.Content)
					}
				case viewFileTool.Name:
					var viewFileToolInput ViewFileParams
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal view file tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &viewFileToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal view file tool arguments: %w", err)
					}
					g.logger.Printf(
						"executing view file tool; path: %s",
						viewFileToolInput.Path,
					)
					result, err = ViewFileTool(viewFileToolInput)
					if err == nil {
						g.logger.Printf("tool result: %+v", result.Content)
					}
				// Folder operation tools
				case createFolderTool.Name:
					var createFolderToolInput CreateFolderParams
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal create folder tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &createFolderToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal create folder tool arguments: %w", err)
					}
					g.logger.Printf(
						"executing create folder tool; path: %s",
						createFolderToolInput.Path,
					)
					result, err = CreateFolderTool(createFolderToolInput)
					if err == nil {
						g.logger.Printf("tool result: %+v", result.Content)
					}
				case deleteFolderTool.Name:
					var deleteFolderToolInput DeleteFolderParams
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal delete folder tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &deleteFolderToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal delete folder tool arguments: %w", err)
					}
					g.logger.Printf(
						"executing delete folder tool; path: %s, recursive: %v",
						deleteFolderToolInput.Path,
						deleteFolderToolInput.Recursive,
					)
					result, err = DeleteFolderTool(deleteFolderToolInput)
					if err == nil {
						g.logger.Printf("tool result: %+v", result.Content)
					}
				case moveFolderTool.Name:
					var moveFolderToolInput MoveFolderParams
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal move folder tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &moveFolderToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal move folder tool arguments: %w", err)
					}
					g.logger.Printf(
						"executing move folder tool; source_path: %s\ntarget_path: %s",
						moveFolderToolInput.SourcePath,
						moveFolderToolInput.TargetPath,
					)
					result, err = MoveFolderTool(moveFolderToolInput)
					if err == nil {
						g.logger.Printf("tool result: %+v", result.Content)
					}
				case filesOverviewTool.Name:
					g.logger.Println("executing files overview tool")
					result, err = ExecuteFilesOverviewTool(g.ignorer)
				case getRelatedFilesTool.Name:
					var relatedFilesToolInput struct {
						InputFiles []string `json:"input_files"`
					}
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal get related files tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &relatedFilesToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal get related files tool arguments: %w", err)
					}
					g.logger.Printf("getting related files: %s", strings.Join(relatedFilesToolInput.InputFiles, ", "))
					result, err = ExecuteGetRelatedFilesTool(relatedFilesToolInput.InputFiles, g.ignorer)
				case changeDirectoryTool.Name:
					var changeDirToolInput struct {
						Path string `json:"path"`
					}
					jsonInput, marshalErr := json.Marshal(v.Args)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal change directory tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &changeDirToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal change directory tool arguments: %w", err)
					}
					g.logger.Printf("changing directory to: %s", changeDirToolInput.Path)
					result, err = executeChangeDirectoryTool(changeDirToolInput.Path)
					if err == nil {
						g.logger.Printf("tool result: %+v", result.Content)
					}
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
			resp, err = g.session.SendMessage(ctx, nextMsg...)
			if err == nil {
				break
			}

			var gerr *googleapi.Error
			if errors.As(err, &gerr) && (gerr.Code == 429 || gerr.Code >= 500) {
				retryCount++
				if retryCount > maxRetries {
					return fmt.Errorf("exceeded maximum retries (%d) when sending message to Gemini: %w", maxRetries, err)
				}
				g.logger.Printf("retrying Gemini API call due to error; retry: %d, status_code: %d, error: %s",
					retryCount,
					gerr.Code,
					gerr.Error(),
				)
				// Remove the failed message from session history before retrying
				if len(g.session.History) > 0 {
					g.session.History = g.session.History[:len(g.session.History)-1]
				}
				time.Sleep(retryWait)
				continue
			}
			return fmt.Errorf("error sending message to Gemini: %w", err)
		}
	}

	return nil
}

func (g *geminiExecutor) LoadMessages(r io.Reader) error {
	var convo Conversation[*genai.ChatSession]
	dec := gob.NewDecoder(r)
	if err := dec.Decode(&convo); err != nil {
		return fmt.Errorf("failed to decode conversation: %w", err)
	}
	if convo.Messages == nil {
		return fmt.Errorf("loaded conversation has nil session")
	}
	if g.model == nil {
		return fmt.Errorf("model is not initialized")
	}
	g.session = g.model.StartChat()
	g.session.History = convo.Messages.History
	return nil
}

func (g *geminiExecutor) SaveMessages(w io.Writer) error {
	convo := Conversation[*genai.ChatSession]{
		Type:     "gemini",
		Messages: g.session,
	}
	enc := gob.NewEncoder(w)
	if err := enc.Encode(convo); err != nil {
		return fmt.Errorf("failed to encode conversation: %w", err)
	}
	return nil
}

func (g *geminiExecutor) PrintMessages() string {
	if g.session == nil || len(g.session.History) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, content := range g.session.History {
		switch content.Role {
		case "user":
			sb.WriteString("USER:\n")
		case "model":
			sb.WriteString("ASSISTANT:\n")
		default:
			continue // Skip other roles
		}

		for _, part := range content.Parts {
			switch p := part.(type) {
			case genai.Text:
				sb.WriteString(string(p))
				sb.WriteString("\n")
			case genai.FunctionCall:
				sb.WriteString(fmt.Sprintf("Tool Call: %s\n", p.Name))
				jsonInput, _ := json.MarshalIndent(p.Args, "", "  ")
				sb.WriteString(fmt.Sprintf("Input: %s\n", string(jsonInput)))
			case genai.FunctionResponse:
				sb.WriteString("Tool Result:\n")
				jsonResp, _ := json.MarshalIndent(p.Response, "", "  ")
				sb.WriteString(fmt.Sprintf("%s\n", string(jsonResp)))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

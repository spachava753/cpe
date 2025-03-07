package agent

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	a "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/gabriel-vasile/mimetype"
	gitignore "github.com/sabhiram/go-gitignore"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func init() {
	// Register Anthropic types with gob
	gob.Register(&a.BetaTextBlockParam{})
	gob.Register(&a.BetaToolUseBlockParam{})
	gob.Register(&a.BetaToolResultBlockParam{})
	gob.Register(&a.BetaImageBlockParam{})
	gob.Register(&a.BetaBase64PDFBlockParam{})
	gob.Register(&a.BetaContentBlockParam{})
	gob.Register(&a.BetaThinkingBlockParam{})
	gob.Register(&a.BetaRedactedThinkingBlockParam{})
	gob.Register(a.BetaToolResultBlockParamContent{})
	gob.Register([]a.BetaToolResultBlockParamContentUnion{})
	gob.Register([]a.BetaContentBlockParamUnion{})
	gob.Register([]a.BetaMessageParam{})
	gob.Register(map[string]interface{}{})
	gob.Register([]interface{}{})
	gob.Register(a.BetaMessageNewParams{})
}

type anthropicExecutor struct {
	client          *a.Client
	logger          Logger
	ignorer         *gitignore.GitIgnore
	config          GenConfig
	params          *a.BetaMessageNewParams
	thinkingEnabled bool
}

func NewAnthropicExecutor(baseUrl string, apiKey string, logger Logger, ignorer *gitignore.GitIgnore, config GenConfig) (Executor, error) {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(5),
		option.WithRequestTimeout(5 * time.Minute),
	}
	if baseUrl != "" {
		// Ensure baseURL ends with a trailing "/"
		if !strings.HasSuffix(baseUrl, "/") {
			baseUrl = baseUrl + "/"
		}
		opts = append(opts, option.WithBaseURL(baseUrl))
	}

	// Add beta header for >64k tokens
	if config.MaxTokens > 64000 {
		opts = append(opts, option.WithHeader("anthropic-beta", string(a.AnthropicBetaOutput128k2025_02_19)))
	}
	client := a.NewClient(opts...)

	// Set initial parameters
	params := &a.BetaMessageNewParams{
		Model:       a.F(config.Model),
		MaxTokens:   a.F(int64(config.MaxTokens)),
		Temperature: a.F(float64(config.Temperature)),
		System: a.F([]a.BetaTextBlockParam{
			{
				Text: a.String(agentInstructions),
				Type: a.F(a.BetaTextBlockParamTypeText),
				CacheControl: a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				}),
			},
		}),
		Tools: a.F([]a.BetaToolUnionUnionParam{
			// Basic tools
			&a.BetaToolParam{
				Name:        a.String(bashTool.Name),
				Description: a.String(bashTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](bashTool.InputSchema["properties"]),
				}),
			},
			// Overview and analysis tools
			&a.BetaToolParam{
				Name:        a.String(filesOverviewTool.Name),
				Description: a.String(filesOverviewTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type: a.F(a.BetaToolInputSchemaTypeObject),
				}),
			},
			&a.BetaToolParam{
				Name:        a.String(getRelatedFilesTool.Name),
				Description: a.String(getRelatedFilesTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](getRelatedFilesTool.InputSchema["properties"]),
				}),
			},
			// Navigation tool
			&a.BetaToolParam{
				Name:        a.String(changeDirectoryTool.Name),
				Description: a.String(changeDirectoryTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](changeDirectoryTool.InputSchema["properties"]),
				}),
				CacheControl: a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				}),
			},
			// File operation tools
			&a.BetaToolParam{
				Name:        a.String(createFileTool.Name),
				Description: a.String(createFileTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](createFileTool.InputSchema["properties"]),
				}),
			},
			&a.BetaToolParam{
				Name:        a.String(editFileTool.Name),
				Description: a.String(editFileTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](editFileTool.InputSchema["properties"]),
				}),
			},
			&a.BetaToolParam{
				Name:        a.String(deleteFileTool.Name),
				Description: a.String(deleteFileTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](deleteFileTool.InputSchema["properties"]),
				}),
			},
			&a.BetaToolParam{
				Name:        a.String(moveFileTool.Name),
				Description: a.String(moveFileTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](moveFileTool.InputSchema["properties"]),
				}),
			},
			&a.BetaToolParam{
				Name:        a.String(viewFileTool.Name),
				Description: a.String(viewFileTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](viewFileTool.InputSchema["properties"]),
				}),
			},
			// Folder operation tools
			&a.BetaToolParam{
				Name:        a.String(createFolderTool.Name),
				Description: a.String(createFolderTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](createFolderTool.InputSchema["properties"]),
				}),
			},
			&a.BetaToolParam{
				Name:        a.String(deleteFolderTool.Name),
				Description: a.String(deleteFolderTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](deleteFolderTool.InputSchema["properties"]),
				}),
			},
			&a.BetaToolParam{
				Name:        a.String(moveFolderTool.Name),
				Description: a.String(moveFolderTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](moveFolderTool.InputSchema["properties"]),
				}),
			},
		}),
	}

	// Add extended thinking configuration if thinking budget is provided and this is Claude 3.7
	var thinkingEnabled bool
	if strings.HasPrefix(config.Model, "claude-3-7") && config.ThinkingBudget != "" && config.ThinkingBudget != "0" {
		// Parse thinking budget as a number
		thinkingBudget, err := strconv.Atoi(config.ThinkingBudget)
		if err != nil {
			return nil, fmt.Errorf("thinking budget must be a numerical value for Anthropic models, got %q", config.ThinkingBudget)
		}

		if thinkingBudget < 1024 {
			return nil, fmt.Errorf("thinking budget value must be at least 1024 tokens, got %d", thinkingBudget)
		}
		if thinkingBudget >= config.MaxTokens {
			return nil, fmt.Errorf("thinking budget value must be less than max_tokens (%d), got %d", config.MaxTokens, thinkingBudget)
		}

		// Only set thinking config if we have a valid budget
		params.Thinking = a.F(a.BetaThinkingConfigParamUnion(&a.BetaThinkingConfigEnabledParam{
			Type:         a.F(a.BetaThinkingConfigEnabledTypeEnabled),
			BudgetTokens: a.F(int64(thinkingBudget)),
		}))
		thinkingEnabled = true

		// When thinking is enabled, temperature must be 1.0 and other params must be unset
		params.Temperature = a.F(1.0)
	}

	// Set optional parameters if provided and thinking is not enabled
	if !thinkingEnabled {
		if config.TopP != nil {
			params.TopP = a.F(float64(*config.TopP))
		}
		if config.TopK != nil {
			params.TopK = a.F(int64(*config.TopK))
		}
	}

	// Set stop sequences if provided
	if config.Stop != nil {
		params.StopSequences = a.F(config.Stop)
	}

	return &anthropicExecutor{
		client:          client,
		logger:          logger,
		ignorer:         ignorer,
		config:          config,
		params:          params,
		thinkingEnabled: thinkingEnabled,
	}, nil
}

func (s *anthropicExecutor) Execute(inputs []Input) error {
	// Convert inputs into content blocks
	var contentBlocks []a.BetaContentBlockParamUnion
	for _, input := range inputs {
		switch input.Type {
		case InputTypeText:
			if len(strings.TrimSpace(input.Text)) > 0 {
				contentBlocks = append(contentBlocks, &a.BetaTextBlockParam{
					Text: a.F(input.Text),
					Type: a.F(a.BetaTextBlockParamTypeText),
				})
			}
		case InputTypeImage:
			// Read and base64 encode the image file
			imgData, err := os.ReadFile(input.FilePath)
			if err != nil {
				return fmt.Errorf("failed to read image file %s: %w", input.FilePath, err)
			}

			// Detect mime type
			mime := mimetype.Detect(imgData)
			if !strings.HasPrefix(mime.String(), "image/") {
				return fmt.Errorf("file %s is not an image", input.FilePath)
			}

			// Base64 encode the image data
			encodedData := base64.StdEncoding.EncodeToString(imgData)

			var imageBlock a.BetaImageBlockParamSourceUnion = &a.BetaImageBlockParamSource{
				Type:      a.F(a.BetaImageBlockParamSourceTypeBase64),
				MediaType: a.F(a.BetaImageBlockParamSourceMediaType(mime.String())),
				Data:      a.F(encodedData),
			}

			// Create image block
			contentBlocks = append(contentBlocks, &a.BetaImageBlockParam{
				Type:   a.F(a.BetaImageBlockParamTypeImage),
				Source: a.F(imageBlock),
			})
		case InputTypeVideo:
			return fmt.Errorf("video input is not supported by Claude models")
		case InputTypeAudio:
			return fmt.Errorf("audio input is not supported by Claude models")
		default:
			return fmt.Errorf("unknown input type: %s", input.Type)
		}
	}

	if !s.params.Messages.Present {
		s.params.Messages = a.F([]a.BetaMessageParam{})
	}

	// If we have no content blocks, create one with an empty prompt
	if len(contentBlocks) == 0 {
		contentBlocks = append(contentBlocks, &a.BetaTextBlockParam{
			Text: a.F("Please analyze these files."),
			Type: a.F(a.BetaTextBlockParamTypeText),
		})
	}

	s.params.Messages = a.F(append(s.params.Messages.Value, a.BetaMessageParam{
		Content: a.F(contentBlocks),
		Role:    a.F(a.BetaMessageParamRoleUser),
	}))

	for {
		// Remove all cache control values from messages
		emptyVal := a.Null[a.BetaCacheControlEphemeralParam]()
		emptyVal.Present = false
		emptyVal.Null = false
		for i := range s.params.Messages.Value {
			for j := range s.params.Messages.Value[i].Content.Value {
				switch block := s.params.Messages.Value[i].Content.Value[j].(type) {
				case *a.BetaTextBlockParam:
					block.CacheControl = emptyVal
				case *a.BetaImageBlockParam:
					block.CacheControl = emptyVal
				case *a.BetaToolUseBlockParam:
					block.CacheControl = emptyVal
				case *a.BetaToolResultBlockParam:
					block.CacheControl = emptyVal
				case *a.BetaBase64PDFBlockParam:
					block.CacheControl = emptyVal
				case *a.BetaContentBlockParam:
					block.CacheControl = emptyVal
				}
			}
		}

		// Add cache control to the last message if thinking is not enabled
		if !s.thinkingEnabled && len(s.params.Messages.Value) > 0 {
			msgIndex := len(s.params.Messages.Value) - 1
			contentBlockIdx := len(s.params.Messages.Value[msgIndex].Content.Value) - 1
			switch block := s.params.Messages.Value[msgIndex].Content.Value[contentBlockIdx].(type) {
			case *a.BetaTextBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaImageBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaToolUseBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaToolResultBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaBase64PDFBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaContentBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			}
		}

		// Add cache control to the third to last message if thinking is not enabled
		if !s.thinkingEnabled && len(s.params.Messages.Value) >= 3 {
			msgIndex := len(s.params.Messages.Value) - 3
			contentBlockIdx := len(s.params.Messages.Value[msgIndex].Content.Value) - 1
			switch block := s.params.Messages.Value[msgIndex].Content.Value[contentBlockIdx].(type) {
			case *a.BetaTextBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaImageBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaToolUseBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaToolResultBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaBase64PDFBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			case *a.BetaContentBlockParam:
				block.CacheControl = a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				})
			}
		}

		// Get model response
		resp, respErr := s.client.Beta.Messages.New(context.Background(),
			*s.params,
		)
		if respErr != nil {
			return fmt.Errorf("failed to create message stream: %w", respErr)
		}

		finished := true
		assistantMsgContentBlocks := make([]a.BetaContentBlockParamUnion, 0, len(resp.Content))
		toolResultContentBlocks := make([]a.BetaContentBlockParamUnion, 0, len(resp.Content))
		var toolUseId string
		for _, block := range resp.Content {
			switch block.Type {
			case a.BetaContentBlockTypeText:
				s.logger.Println(block.Text)
				assistantMsgContentBlocks = append(assistantMsgContentBlocks, &a.BetaTextBlockParam{
					Text: a.F(block.Text),
					Type: a.F(a.BetaTextBlockParamTypeText),
				})
			case a.BetaContentBlockTypeThinking:
				s.logger.Printf("Thinking: %s\n", block.Thinking)
				assistantMsgContentBlocks = append(assistantMsgContentBlocks, &a.BetaThinkingBlockParam{
					Type:      a.F(a.BetaThinkingBlockParamTypeThinking),
					Thinking:  a.F(block.Thinking),
					Signature: a.F(block.Signature),
				})
			case a.BetaContentBlockTypeRedactedThinking:
				s.logger.Println("Received redacted thinking block")
				assistantMsgContentBlocks = append(assistantMsgContentBlocks, &a.BetaRedactedThinkingBlockParam{
					Type: a.F(a.BetaRedactedThinkingBlockParamTypeRedactedThinking),
					Data: a.F(block.Data),
				})
			case a.BetaContentBlockTypeToolUse:
				finished = false
				toolUseId = block.ID
				assistantMsgContentBlocks = append(assistantMsgContentBlocks, &a.BetaToolUseBlockParam{
					ID:    a.F(toolUseId),
					Input: a.F(block.Input),
					Name:  a.F(block.Name),
					Type:  a.F(a.BetaToolUseBlockParamTypeToolUse),
				})
				var result *ToolResult
				var err error
				switch block.Name {
				case bashTool.Name:
					bashToolInput := struct {
						Command string `json:"command"`
					}{}
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal bash tool input: %w", marshalErr)
					}
					
					if err := json.Unmarshal(jsonInput, &bashToolInput); err != nil {
						// Instead of failing, return the error as a tool result to the model
						s.logger.Printf("JSON parsing error for bash tool: %v", err)
						errorMessage := fmt.Sprintf("Error parsing JSON for bash tool: %v\nReceived input: %s\n\nPlease provide input in the correct format with a string for 'command', e.g. {\"command\": \"ls -la\"}", err, string(jsonInput))
						
						result = &ToolResult{
							Content: errorMessage,
							IsError: true,
						}
					} else {
						s.logger.Printf("executing bash command: %s", bashToolInput.Command)
						result, err = executeBashTool(bashToolInput.Command)
						if err != nil {
							return fmt.Errorf("failed to execute bash tool: %w", err)
						}
						
						// Log full output before truncation
						s.logger.Printf("tool result: %+v", result.Content)

						// Count tokens in the tool result
						countTokensRes, err := s.client.Beta.Messages.CountTokens(context.Background(), a.BetaMessageCountTokensParams{
							Model: a.F(s.config.Model),
							Messages: a.F([]a.BetaMessageParam{
								{
									Role: a.F(a.BetaMessageParamRoleUser),
									Content: a.F([]a.BetaContentBlockParamUnion{
										&a.BetaTextBlockParam{
											Text: a.F(fmt.Sprintf("%+v", result.Content)),
											Type: a.F(a.BetaTextBlockParamTypeText),
										},
									}),
								},
							}),
						})

						if err == nil && countTokensRes.InputTokens > 50000 {
							// Truncate output to 150,000 characters
							truncatedOutput := fmt.Sprintf("%+v", result.Content)
							if len(truncatedOutput) > 150000 {
								truncatedOutput = truncatedOutput[:150000]
							}
							s.logger.Println("Warning: bash output exceeded 50,000 tokens and was truncated")
							result = &ToolResult{
								Content: fmt.Sprintf("Warning: Output exceeded 50,000 tokens and was truncated.\n\n%s", truncatedOutput),
								IsError: true,
							}
						}
					}
				// File operation tools
				case createFileTool.Name:
					var createFileToolInput CreateFileParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal create file tool input: %w", marshalErr)
					}
					
					// First try standard unmarshaling
					unmarshalErr := json.Unmarshal(jsonInput, &createFileToolInput)
					if unmarshalErr != nil {
						// If that fails, try to autocorrect the JSON
						s.logger.Printf("JSON parsing error for create_file tool: %v. Attempting autocorrection.", unmarshalErr)
						
						// Convert to string for autocorrection
						jsonStr := string(jsonInput)
						correctedJSON, autocorrectErr := AutoCorrectJSON(jsonStr, &createFileToolInput)
						if autocorrectErr != nil {
							s.logger.Printf("JSON autocorrection failed: %v", autocorrectErr)
							return fmt.Errorf("failed to unmarshal create file tool arguments: %w", unmarshalErr)
						}
						
						// Try again with the corrected JSON
						if err := json.Unmarshal([]byte(correctedJSON), &createFileToolInput); err != nil {
							return fmt.Errorf("failed to unmarshal create file tool arguments even after correction: %w", err)
						}
						
						s.logger.Printf("JSON autocorrection succeeded. Original: %s, Corrected: %s", jsonStr, correctedJSON)
					}
					
					s.logger.Printf(
						"executing create file tool; path: %s\nfile_text:\n%s",
						createFileToolInput.Path,
						createFileToolInput.FileText,
					)
					result, err = CreateFileTool(createFileToolInput)
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				case editFileTool.Name:
					var editFileToolInput EditFileParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal edit file tool input: %w", marshalErr)
					}
					
					// First try standard unmarshaling
					unmarshalErr := json.Unmarshal(jsonInput, &editFileToolInput)
					if unmarshalErr != nil {
						// If that fails, try to autocorrect the JSON
						s.logger.Printf("JSON parsing error for edit_file tool: %v. Attempting autocorrection.", unmarshalErr)
						
						// Convert to string for autocorrection
						jsonStr := string(jsonInput)
						correctedJSON, autocorrectErr := AutoCorrectJSON(jsonStr, &editFileToolInput)
						if autocorrectErr != nil {
							s.logger.Printf("JSON autocorrection failed: %v", autocorrectErr)
							return fmt.Errorf("failed to unmarshal edit file tool arguments: %w", unmarshalErr)
						}
						
						// Try again with the corrected JSON
						if err := json.Unmarshal([]byte(correctedJSON), &editFileToolInput); err != nil {
							return fmt.Errorf("failed to unmarshal edit file tool arguments even after correction: %w", err)
						}
						
						s.logger.Printf("JSON autocorrection succeeded. Original: %s, Corrected: %s", jsonStr, correctedJSON)
					}
					
					s.logger.Printf(
						"executing edit file tool; path: %s\nold_str:\n%s\nnew_str:\n%s",
						editFileToolInput.Path,
						editFileToolInput.OldStr,
						editFileToolInput.NewStr,
					)
					result, err = EditFileTool(editFileToolInput)
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				case deleteFileTool.Name:
					var deleteFileToolInput DeleteFileParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal delete file tool input: %w", marshalErr)
					}
					
					// First try standard unmarshaling
					unmarshalErr := json.Unmarshal(jsonInput, &deleteFileToolInput)
					if unmarshalErr != nil {
						// If that fails, try to autocorrect the JSON
						s.logger.Printf("JSON parsing error for delete_file tool: %v. Attempting autocorrection.", unmarshalErr)
						
						// Convert to string for autocorrection
						jsonStr := string(jsonInput)
						correctedJSON, autocorrectErr := AutoCorrectJSON(jsonStr, &deleteFileToolInput)
						if autocorrectErr != nil {
							s.logger.Printf("JSON autocorrection failed: %v", autocorrectErr)
							return fmt.Errorf("failed to unmarshal delete file tool arguments: %w", unmarshalErr)
						}
						
						// Try again with the corrected JSON
						if err := json.Unmarshal([]byte(correctedJSON), &deleteFileToolInput); err != nil {
							return fmt.Errorf("failed to unmarshal delete file tool arguments even after correction: %w", err)
						}
						
						s.logger.Printf("JSON autocorrection succeeded. Original: %s, Corrected: %s", jsonStr, correctedJSON)
					}
					
					s.logger.Printf(
						"executing delete file tool; path: %s",
						deleteFileToolInput.Path,
					)
					result, err = DeleteFileTool(deleteFileToolInput)
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				case moveFileTool.Name:
					var moveFileToolInput MoveFileParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal move file tool input: %w", marshalErr)
					}
					
					// First try standard unmarshaling
					unmarshalErr := json.Unmarshal(jsonInput, &moveFileToolInput)
					if unmarshalErr != nil {
						// If that fails, try to autocorrect the JSON
						s.logger.Printf("JSON parsing error for move_file tool: %v. Attempting autocorrection.", unmarshalErr)
						
						// Convert to string for autocorrection
						jsonStr := string(jsonInput)
						correctedJSON, autocorrectErr := AutoCorrectJSON(jsonStr, &moveFileToolInput)
						if autocorrectErr != nil {
							s.logger.Printf("JSON autocorrection failed: %v", autocorrectErr)
							return fmt.Errorf("failed to unmarshal move file tool arguments: %w", unmarshalErr)
						}
						
						// Try again with the corrected JSON
						if err := json.Unmarshal([]byte(correctedJSON), &moveFileToolInput); err != nil {
							return fmt.Errorf("failed to unmarshal move file tool arguments even after correction: %w", err)
						}
						
						s.logger.Printf("JSON autocorrection succeeded. Original: %s, Corrected: %s", jsonStr, correctedJSON)
					}
					
					s.logger.Printf(
						"executing move file tool; source_path: %s\ntarget_path: %s",
						moveFileToolInput.SourcePath,
						moveFileToolInput.TargetPath,
					)
					result, err = MoveFileTool(moveFileToolInput)
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				case viewFileTool.Name:
					var viewFileToolInput ViewFileParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal view file tool input: %w", marshalErr)
					}
					
					// First try standard unmarshaling
					unmarshalErr := json.Unmarshal(jsonInput, &viewFileToolInput)
					if unmarshalErr != nil {
						// If that fails, try to autocorrect the JSON
						s.logger.Printf("JSON parsing error for view_file tool: %v. Attempting autocorrection.", unmarshalErr)
						
						// Convert to string for autocorrection
						jsonStr := string(jsonInput)
						correctedJSON, autocorrectErr := AutoCorrectJSON(jsonStr, &viewFileToolInput)
						if autocorrectErr != nil {
							s.logger.Printf("JSON autocorrection failed: %v", autocorrectErr)
							return fmt.Errorf("failed to unmarshal view file tool arguments: %w", unmarshalErr)
						}
						
						// Try again with the corrected JSON
						if err := json.Unmarshal([]byte(correctedJSON), &viewFileToolInput); err != nil {
							return fmt.Errorf("failed to unmarshal view file tool arguments even after correction: %w", err)
						}
						
						s.logger.Printf("JSON autocorrection succeeded. Original: %s, Corrected: %s", jsonStr, correctedJSON)
					}
					
					s.logger.Printf(
						"executing view file tool; path: %s",
						viewFileToolInput.Path,
					)
					result, err = ViewFileTool(viewFileToolInput)
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				// Folder operation tools
				case createFolderTool.Name:
					var createFolderToolInput CreateFolderParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal create folder tool input: %w", marshalErr)
					}
					
					// First try standard unmarshaling
					unmarshalErr := json.Unmarshal(jsonInput, &createFolderToolInput)
					if unmarshalErr != nil {
						// If that fails, try to autocorrect the JSON
						s.logger.Printf("JSON parsing error for create_folder tool: %v. Attempting autocorrection.", unmarshalErr)
						
						// Convert to string for autocorrection
						jsonStr := string(jsonInput)
						correctedJSON, autocorrectErr := AutoCorrectJSON(jsonStr, &createFolderToolInput)
						if autocorrectErr != nil {
							s.logger.Printf("JSON autocorrection failed: %v", autocorrectErr)
							return fmt.Errorf("failed to unmarshal create folder tool arguments: %w", unmarshalErr)
						}
						
						// Try again with the corrected JSON
						if err := json.Unmarshal([]byte(correctedJSON), &createFolderToolInput); err != nil {
							return fmt.Errorf("failed to unmarshal create folder tool arguments even after correction: %w", err)
						}
						
						s.logger.Printf("JSON autocorrection succeeded. Original: %s, Corrected: %s", jsonStr, correctedJSON)
					}
					
					s.logger.Printf(
						"executing create folder tool; path: %s",
						createFolderToolInput.Path,
					)
					result, err = CreateFolderTool(createFolderToolInput)
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				case deleteFolderTool.Name:
					var deleteFolderToolInput DeleteFolderParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal delete folder tool input: %w", marshalErr)
					}
					
					// First try standard unmarshaling
					unmarshalErr := json.Unmarshal(jsonInput, &deleteFolderToolInput)
					if unmarshalErr != nil {
						// If that fails, try to autocorrect the JSON
						s.logger.Printf("JSON parsing error for delete_folder tool: %v. Attempting autocorrection.", unmarshalErr)
						
						// Convert to string for autocorrection
						jsonStr := string(jsonInput)
						correctedJSON, autocorrectErr := AutoCorrectJSON(jsonStr, &deleteFolderToolInput)
						if autocorrectErr != nil {
							s.logger.Printf("JSON autocorrection failed: %v", autocorrectErr)
							return fmt.Errorf("failed to unmarshal delete folder tool arguments: %w", unmarshalErr)
						}
						
						// Try again with the corrected JSON
						if err := json.Unmarshal([]byte(correctedJSON), &deleteFolderToolInput); err != nil {
							return fmt.Errorf("failed to unmarshal delete folder tool arguments even after correction: %w", err)
						}
						
						s.logger.Printf("JSON autocorrection succeeded. Original: %s, Corrected: %s", jsonStr, correctedJSON)
					}
					
					s.logger.Printf(
						"executing delete folder tool; path: %s, recursive: %v",
						deleteFolderToolInput.Path,
						deleteFolderToolInput.Recursive,
					)
					result, err = DeleteFolderTool(deleteFolderToolInput)
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				case moveFolderTool.Name:
					var moveFolderToolInput MoveFolderParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal move folder tool input: %w", marshalErr)
					}
					
					// First try standard unmarshaling
					unmarshalErr := json.Unmarshal(jsonInput, &moveFolderToolInput)
					if unmarshalErr != nil {
						// If that fails, try to autocorrect the JSON
						s.logger.Printf("JSON parsing error for move_folder tool: %v. Attempting autocorrection.", unmarshalErr)
						
						// Convert to string for autocorrection
						jsonStr := string(jsonInput)
						correctedJSON, autocorrectErr := AutoCorrectJSON(jsonStr, &moveFolderToolInput)
						if autocorrectErr != nil {
							s.logger.Printf("JSON autocorrection failed: %v", autocorrectErr)
							return fmt.Errorf("failed to unmarshal move folder tool arguments: %w", unmarshalErr)
						}
						
						// Try again with the corrected JSON
						if err := json.Unmarshal([]byte(correctedJSON), &moveFolderToolInput); err != nil {
							return fmt.Errorf("failed to unmarshal move folder tool arguments even after correction: %w", err)
						}
						
						s.logger.Printf("JSON autocorrection succeeded. Original: %s, Corrected: %s", jsonStr, correctedJSON)
					}
					
					s.logger.Printf(
						"executing move folder tool; source_path: %s\ntarget_path: %s",
						moveFolderToolInput.SourcePath,
						moveFolderToolInput.TargetPath,
					)
					result, err = MoveFolderTool(moveFolderToolInput)
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				case filesOverviewTool.Name:
					s.logger.Println("executing files overview tool")
					result, err = ExecuteFilesOverviewTool(s.ignorer)
					if err != nil {
						return fmt.Errorf("failed to execute files_overview tool: %w", err)
					}
				case getRelatedFilesTool.Name:
					relatedFilesToolInput := struct {
						InputFiles []string `json:"input_files"`
					}{}
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal get related files tool input: %w", marshalErr)
					}
					
					if err := json.Unmarshal(jsonInput, &relatedFilesToolInput); err != nil {
						// Instead of failing, return the error as a tool result to the model
						s.logger.Printf("JSON parsing error for get_related_files: %v", err)
						errorMessage := fmt.Sprintf("Error parsing JSON for get_related_files tool: %v\nReceived input: %s\n\nPlease provide input in the correct format with an array of strings for 'input_files', e.g. {\"input_files\": [\"file1.go\", \"file2.go\"]}", err, string(jsonInput))
						
						result = &ToolResult{
							Content: errorMessage,
							IsError: true,
						}
					} else {
						s.logger.Printf("getting related files: %s", strings.Join(relatedFilesToolInput.InputFiles, ", "))
						result, err = ExecuteGetRelatedFilesTool(relatedFilesToolInput.InputFiles, s.ignorer)
						if err != nil {
							return fmt.Errorf("failed to execute get_related_files tool: %w", err)
						}
					}
				case changeDirectoryTool.Name:
					changeDirToolInput := struct {
						Path string `json:"path"`
					}{}
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal change directory tool input: %w", marshalErr)
					}
					
					if err := json.Unmarshal(jsonInput, &changeDirToolInput); err != nil {
						// Instead of failing, return the error as a tool result to the model
						s.logger.Printf("JSON parsing error for change_directory tool: %v", err)
						errorMessage := fmt.Sprintf("Error parsing JSON for change_directory tool: %v\nReceived input: %s\n\nPlease provide input in the correct format with a string for 'path', e.g. {\"path\": \"../some/directory\"}", err, string(jsonInput))
						
						result = &ToolResult{
							Content: errorMessage,
							IsError: true,
						}
					} else {
						s.logger.Printf("changing directory to: %s", changeDirToolInput.Path)
						result, err = executeChangeDirectoryTool(changeDirToolInput.Path)
						if err != nil {
							return fmt.Errorf("failed to execute change_directory tool: %w", err)
						}
					}
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				default:
					return fmt.Errorf("unexpected tool use block type: %s", block.Name)
				}

				if err != nil {
					return fmt.Errorf("failed to execute tool %s: %w", block.Name, err)
				}

				result.ToolUseID = block.ID

				toolResultContentBlocks = append(toolResultContentBlocks, &a.BetaToolResultBlockParam{
					ToolUseID: a.F(toolUseId),
					Type:      a.F(a.BetaToolResultBlockParamTypeToolResult),
					Content: a.F([]a.BetaToolResultBlockParamContentUnion{
						a.BetaToolResultBlockParamContent{
							Type: a.F(a.BetaToolResultBlockParamContentTypeText),
							Text: a.F[string](fmt.Sprintf("%+v", result.Content)),
						},
					}),
					IsError: a.F(result.IsError),
				})
			default:
				return fmt.Errorf("unexpected content block type: %s", block.Type)
			}
		}

		// Only add assistant message if it has content
		// This prevents empty assistant messages from being added to the conversation history
		// which would cause API errors when continuing the conversation
		if len(assistantMsgContentBlocks) > 0 {
			s.params.Messages = a.F(
				append(
					s.params.Messages.Value,
					a.BetaMessageParam{
						Role:    a.F(a.BetaMessageParamRoleAssistant),
						Content: a.F(assistantMsgContentBlocks),
					},
				),
			)
		}

		if len(toolResultContentBlocks) > 0 {
			s.params.Messages = a.F(
				append(
					s.params.Messages.Value,
					a.BetaMessageParam{
						Role:    a.F(a.BetaMessageParamRoleUser),
						Content: a.F(toolResultContentBlocks),
					},
				),
			)
		}
		if finished {
			break
		}
	}

	return nil
}

func (s *anthropicExecutor) LoadMessages(r io.Reader) error {
	var convo Conversation[[]a.BetaMessageParam]
	dec := gob.NewDecoder(r)
	if err := dec.Decode(&convo); err != nil {
		return fmt.Errorf("failed to decode conversation: %w", err)
	}

	// Filter out any empty assistant messages
	// This prevents API errors when continuing the conversation
	// The Anthropic API requires that all messages have non-empty content
	// except for the optional final assistant message
	var filteredMessages []a.BetaMessageParam
	for _, msg := range convo.Messages {
		// Skip empty assistant messages (those with no content blocks)
		if msg.Role.Value == a.BetaMessageParamRoleAssistant &&
			(len(msg.Content.Value) == 0 ||
				(len(msg.Content.Value) == 1 &&
					isEmptyTextBlock(msg.Content.Value[0]))) {
			continue
		}
		filteredMessages = append(filteredMessages, msg)
	}

	s.params.Messages = a.F(filteredMessages)
	return nil
}

func (s *anthropicExecutor) PrintMessages() string {
	if !s.params.Messages.Present {
		return ""
	}

	var sb strings.Builder
	for _, msg := range s.params.Messages.Value {
		// Skip system messages
		if msg.Role.Value == "system" {
			continue
		}

		// Print role header
		switch msg.Role.Value {
		case a.BetaMessageParamRoleUser:
			sb.WriteString("USER:\n")
		case a.BetaMessageParamRoleAssistant:
			sb.WriteString("ASSISTANT:\n")
		}

		// Print message content
		for _, block := range msg.Content.Value {
			switch b := block.(type) {
			case *a.BetaTextBlockParam:
				sb.WriteString(b.Text.Value)
				sb.WriteString("\n")
			case *a.BetaToolUseBlockParam:
				sb.WriteString(fmt.Sprintf("Tool Call: %s\n", b.Name.Value))
				jsonInput, _ := json.MarshalIndent(b.Input.Value, "", "  ")
				sb.WriteString(fmt.Sprintf("Input: %s\n", string(jsonInput)))
			case *a.BetaToolResultBlockParam:
				sb.WriteString("Tool Result:\n")
				for _, content := range b.Content.Value {
					switch c := content.(type) {
					case a.BetaToolResultBlockParamContent:
						if c.Type.Value == "text" {
							sb.WriteString(c.Text.Value)
							sb.WriteString("\n")
						}
					}
				}
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (s *anthropicExecutor) SaveMessages(w io.Writer) error {
	// Filter out any empty assistant messages before saving
	// This prevents API errors when continuing the conversation
	// The Anthropic API requires that all messages have non-empty content
	// except for the optional final assistant message
	var filteredMessages []a.BetaMessageParam
	for _, msg := range s.params.Messages.Value {
		// Skip empty assistant messages (those with no content blocks)
		if msg.Role.Value == a.BetaMessageParamRoleAssistant &&
			(len(msg.Content.Value) == 0 ||
				(len(msg.Content.Value) == 1 &&
					isEmptyTextBlock(msg.Content.Value[0]))) {
			continue
		}
		filteredMessages = append(filteredMessages, msg)
	}

	convo := Conversation[[]a.BetaMessageParam]{
		Type:     "anthropic",
		Messages: filteredMessages,
	}
	enc := gob.NewEncoder(w)
	if err := enc.Encode(convo); err != nil {
		return fmt.Errorf("failed to encode conversation: %w", err)
	}
	return nil
}

// isEmptyTextBlock checks if a content block is an empty text block
func isEmptyTextBlock(block a.BetaContentBlockParamUnion) bool {
	if textBlock, ok := block.(*a.BetaTextBlockParam); ok {
		return textBlock.Text.Value == ""
	}
	return false
}

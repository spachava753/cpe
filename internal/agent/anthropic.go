package agent

import (
	"context"
	_ "embed"
	"encoding/gob"
	"encoding/json"
	"fmt"
	a "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"io"
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
	gob.Register(a.BetaToolResultBlockParamContent{})
	gob.Register([]a.BetaToolResultBlockParamContentUnion{})
	gob.Register([]a.BetaContentBlockParamUnion{})
	gob.Register([]a.BetaMessageParam{})
	gob.Register(map[string]interface{}{})
	gob.Register([]interface{}{})
	gob.Register(a.BetaMessageNewParams{})
}

type anthropicExecutor struct {
	client  *a.Client
	logger  Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
	params  *a.BetaMessageNewParams
}

func NewAnthropicExecutor(baseUrl string, apiKey string, logger Logger, ignorer *gitignore.GitIgnore, config GenConfig) Executor {
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
	client := a.NewClient(opts...)

	params := &a.BetaMessageNewParams{
		Model:       a.F(config.Model),
		MaxTokens:   a.F(int64(config.MaxTokens)),
		Temperature: a.F(float64(config.Temperature)),
		System: a.F([]a.BetaTextBlockParam{
			{
				Text: a.String(agentInstructions),
				Type: a.F(a.BetaTextBlockParamTypeText),
			},
		}),
		Tools: a.F([]a.BetaToolUnionUnionParam{
			&a.BetaToolParam{
				Name:        a.String(bashTool.Name),
				Description: a.String(bashTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](bashTool.InputSchema["properties"]),
				}),
			},
			&a.BetaToolParam{
				Name:        a.String(fileEditor.Name),
				Description: a.String(fileEditor.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](fileEditor.InputSchema["properties"]),
				}),
			},
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
			&a.BetaToolParam{
				Name:        a.String(changeDirectoryTool.Name),
				Description: a.String(changeDirectoryTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type:       a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[any](changeDirectoryTool.InputSchema["properties"]),
				}),
			},
		}),
	}

	if config.TopP != nil {
		params.TopP = a.F(float64(*config.TopP))
	}
	if config.TopK != nil {
		params.TopK = a.F(int64(*config.TopK))
	}
	if config.Stop != nil {
		params.StopSequences = a.F(config.Stop)
	}

	return &anthropicExecutor{
		client:  client,
		logger:  logger,
		ignorer: ignorer,
		config:  config,
		params:  params,
	}
}

func (s *anthropicExecutor) Execute(input string) error {
	if !s.params.Messages.Present {
		s.params.Messages = a.F([]a.BetaMessageParam{})
	}

	s.params.Messages = a.F(append(s.params.Messages.Value, a.BetaMessageParam{
		Content: a.F([]a.BetaContentBlockParamUnion{
			&a.BetaTextBlockParam{
				Text: a.F(input),
				Type: a.F(a.BetaTextBlockParamTypeText),
			},
		}),
		Role: a.F(a.BetaMessageParamRoleUser),
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
				default:
					return fmt.Errorf("unhandled content block type %T when removing cache control", block)
				}
			}
		}

		// Add cache control to the last message
		if len(s.params.Messages.Value) > 0 {
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
			default:
				return fmt.Errorf("unhandled content block type %T when setting cache control for last message", block)
			}
		}

		// Add cache control to the third to last message if it exists
		if len(s.params.Messages.Value) >= 3 {
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
			default:
				return fmt.Errorf("unhandled content block type %T when setting cache control for last message", block)
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
		assistantMsgContentBlocks := make([]a.BetaContentBlockParamUnion, len(resp.Content))
		var toolUseId string
		for i, block := range resp.Content {
			switch block.Type {
			case a.BetaContentBlockTypeText:
				s.logger.Println(block.Text)
				assistantMsgContentBlocks[i] = &a.BetaTextBlockParam{
					Text: a.F(block.Text),
					Type: a.F(a.BetaTextBlockParamTypeText),
				}
			case a.BetaContentBlockTypeToolUse:
				finished = false
				toolUseId = block.ID
				assistantMsgContentBlocks[i] = &a.BetaToolUseBlockParam{
					ID:    a.F(toolUseId),
					Input: a.F(block.Input),
					Name:  a.F(block.Name),
					Type:  a.F(a.BetaToolUseBlockParamTypeToolUse),
				}
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
						return fmt.Errorf("failed to unmarshal bash tool arguments: %w", err)
					}
					s.logger.Printf("executing bash command: %s", bashToolInput.Command)
					result, err = executeBashTool(bashToolInput.Command)
					if err == nil {
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
				case fileEditor.Name:
					var fileEditorToolInput FileEditorParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal file editor tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &fileEditorToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal file editor tool arguments: %w", err)
					}
					s.logger.Printf(
						"executing file editor tool; command: %s\npath: %s\nfile_text: %s\nold_str: %s\nnew_str: %s",
						fileEditorToolInput.Command,
						fileEditorToolInput.Path,
						fileEditorToolInput.FileText,
						fileEditorToolInput.OldStr,
						fileEditorToolInput.NewStr,
					)
					result, err = executeFileEditorTool(fileEditorToolInput)
					if err == nil {
						s.logger.Printf("tool result: %+v", result.Content)
					}
				case filesOverviewTool.Name:
					s.logger.Println("executing files overview tool")
					result, err = ExecuteFilesOverviewTool(s.ignorer)
				case getRelatedFilesTool.Name:
					relatedFilesToolInput := struct {
						InputFiles []string `json:"input_files"`
					}{}
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal get related files tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &relatedFilesToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal get related files tool arguments: %w", err)
					}
					s.logger.Printf("getting related files: %s", strings.Join(relatedFilesToolInput.InputFiles, ", "))
					result, err = ExecuteGetRelatedFilesTool(relatedFilesToolInput.InputFiles, s.ignorer)
				case changeDirectoryTool.Name:
					changeDirToolInput := struct {
						Path string `json:"path"`
					}{}
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal change directory tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &changeDirToolInput); err != nil {
						return fmt.Errorf("failed to unmarshal change directory tool arguments: %w", err)
					}
					s.logger.Printf("changing directory to: %s", changeDirToolInput.Path)
					result, err = executeChangeDirectoryTool(changeDirToolInput.Path)
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
				s.params.Messages = a.F(append(s.params.Messages.Value, a.BetaMessageParam{
					Role:    a.F(a.BetaMessageParamRoleAssistant),
					Content: a.F(assistantMsgContentBlocks),
				}))
				s.params.Messages = a.F(append(s.params.Messages.Value, a.BetaMessageParam{
					Content: a.F([]a.BetaContentBlockParamUnion{
						&a.BetaToolResultBlockParam{
							ToolUseID: a.F(toolUseId),
							Type:      a.F(a.BetaToolResultBlockParamTypeToolResult),
							Content: a.F([]a.BetaToolResultBlockParamContentUnion{
								a.BetaToolResultBlockParamContent{
									Type: a.F(a.BetaToolResultBlockParamContentTypeText),
									Text: a.F[string](fmt.Sprintf("%+v", result.Content)),
								},
							}),
							IsError: a.F(result.IsError),
						},
					}),
					Role: a.F(a.BetaMessageParamRoleUser),
				}))
			default:
				return fmt.Errorf("unexpected content block type: %s", block.Type)
			}
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
	s.params.Messages = a.F(convo.Messages)
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
		case "user":
			sb.WriteString("USER:\n")
		case "assistant":
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
	convo := Conversation[[]a.BetaMessageParam]{
		Type:     "anthropic",
		Messages: s.params.Messages.Value,
	}
	enc := gob.NewEncoder(w)
	if err := enc.Encode(convo); err != nil {
		return fmt.Errorf("failed to encode conversation: %w", err)
	}
	return nil
}

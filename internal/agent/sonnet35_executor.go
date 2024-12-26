package agent

import (
	"context"
	"encoding/json"
	"fmt"
	a "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/llm"
	"log/slog"
	"strings"
	"time"
)

type sonnet35Executor struct {
	client  *a.Client
	logger  *slog.Logger
	ignorer *gitignore.GitIgnore
	config  llm.GenConfig
}

func NewSonnet35Executor(baseUrl string, apiKey string, logger *slog.Logger, ignorer *gitignore.GitIgnore, config llm.GenConfig) Executor {
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
	return &sonnet35Executor{
		client:  client,
		logger:  logger,
		ignorer: ignorer,
		config:  config,
	}
}

func (s *sonnet35Executor) Execute(input string) error {
	params := a.BetaMessageNewParams{
		Model:       a.F(s.config.Model),
		MaxTokens:   a.F(int64(s.config.MaxTokens)),
		Temperature: a.F(float64(s.config.Temperature)),
		System: a.F([]a.BetaTextBlockParam{
			{
				Text: a.F(agentInstructions),
				Type: a.F(a.BetaTextBlockParamTypeText),
				CacheControl: a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				}),
			},
		}),
		Tools: a.F([]a.BetaToolUnionUnionParam{
			&a.BetaToolBash20241022Param{
				Name: a.F(a.BetaToolBash20241022NameBash),
				Type: a.F(a.BetaToolBash20241022TypeBash20241022),
				CacheControl: a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				}),
			},
			&a.BetaToolTextEditor20241022Param{
				Name: a.F(a.BetaToolTextEditor20241022NameStrReplaceEditor),
				Type: a.F(a.BetaToolTextEditor20241022TypeTextEditor20241022),
				CacheControl: a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				}),
			},
			&a.BetaToolParam{
				Name:        a.F(filesOverviewTool.Name),
				Description: a.F(filesOverviewTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type: a.F(a.BetaToolInputSchemaTypeObject),
				}),
				CacheControl: a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				}),
			},
			&a.BetaToolParam{
				Name:        a.F(getRelatedFilesTool.Name),
				Description: a.F(getRelatedFilesTool.Description),
				InputSchema: a.F(a.BetaToolInputSchemaParam{
					Type: a.F(a.BetaToolInputSchemaTypeObject),
					Properties: a.F[interface{}](map[string]interface{}{
						"input_files": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "string",
							},
							"description": `An array of input files to retrieve related files, e.g. source code files that have symbol definitions in another file or other files that mention the file's name.'`,
						},
					}),
				}),
				CacheControl: a.F(a.BetaCacheControlEphemeralParam{
					Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
				}),
			},
		}),
	}

	if s.config.TopP != nil {
		params.TopP = a.F(float64(*s.config.TopP))
	}
	if s.config.TopK != nil {
		params.TopK = a.F(int64(*s.config.TopK))
	}
	if s.config.Stop != nil {
		params.StopSequences = a.F(s.config.Stop)
	}

	params.Messages = a.F([]a.BetaMessageParam{
		{
			Content: a.F([]a.BetaContentBlockParamUnion{
				a.BetaTextBlockParam{
					Text: a.F(input),
					Type: a.F(a.BetaTextBlockParamTypeText),
					CacheControl: a.F(a.BetaCacheControlEphemeralParam{
						Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
					}),
				},
			}),
			Role: a.F(a.BetaMessageParamRoleUser),
		},
	})

	for {
		// Create message
		resp, respErr := s.client.Beta.Messages.New(context.Background(),
			params,
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
				s.logger.Info(block.Text)
				assistantMsgContentBlocks[i] = &a.BetaTextBlockParam{
					Text: a.F(block.Text),
					Type: a.F(a.BetaTextBlockParamTypeText),
					CacheControl: a.F(a.BetaCacheControlEphemeralParam{
						Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
					}),
				}
			case a.BetaContentBlockTypeToolUse:
				finished = false
				s.logger.Info(fmt.Sprintf("%+v", block.Input))
				toolUseId = block.ID
				assistantMsgContentBlocks[i] = &a.BetaToolUseBlockParam{
					ID:    a.F(toolUseId),
					Input: a.F(block.Input),
					Name:  a.F(block.Name),
					Type:  a.F(a.BetaToolUseBlockParamTypeToolUse),
					CacheControl: a.F(a.BetaCacheControlEphemeralParam{
						Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
					}),
				}
				var result *llm.ToolResult
				var err error
				switch block.Name {
				case string(a.BetaToolBash20241022NameBash):
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						panic(marshalErr)
					}
					var toolInput json.RawMessage
					if unmarshallErr := json.Unmarshal(jsonInput, &toolInput); unmarshallErr != nil {
						panic(unmarshallErr)
					}
					result, err = executeBashTool(toolInput, s.logger)
				case string(a.BetaToolTextEditor20241022NameStrReplaceEditor):
					panic("not implemented")
				case filesOverviewTool.Name:
					result, err = executeFilesOverviewTool(s.ignorer, s.logger)
				case getRelatedFilesTool.Name:
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						panic(marshalErr)
					}
					var toolInput json.RawMessage
					if unmarshallErr := json.Unmarshal(jsonInput, &toolInput); unmarshallErr != nil {
						panic(unmarshallErr)
					}
					result, err = executeGetRelatedFilesTool(toolInput, s.ignorer, s.logger)
				default:
					return fmt.Errorf("unexpected tool use block type: %s", block.Name)
				}

				if err != nil {
					return fmt.Errorf("failed to execute tool %s: %w", block.Name, err)
				}

				result.ToolUseID = block.ID
				params.Messages = a.F(append(params.Messages.Value, a.BetaMessageParam{
					Role:    a.F(a.BetaMessageParamRoleAssistant),
					Content: a.F(assistantMsgContentBlocks),
				}))
				params.Messages = a.F(append(params.Messages.Value, a.BetaMessageParam{
					Content: a.F([]a.BetaContentBlockParamUnion{
						a.BetaToolResultBlockParam{
							ToolUseID: a.F(toolUseId),
							Type:      a.F(a.BetaToolResultBlockParamTypeToolResult),
							Content: a.F([]a.BetaToolResultBlockParamContentUnion{
								a.BetaToolResultBlockParamContent{
									Type: a.F(a.BetaToolResultBlockParamContentTypeText),
									CacheControl: a.F(a.BetaCacheControlEphemeralParam{
										Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
									}),
									Text: a.F[string](fmt.Sprintf("%+v", result.Content)),
								},
							}),
							IsError: a.F(result.IsError),
							CacheControl: a.F(a.BetaCacheControlEphemeralParam{
								Type: a.F(a.BetaCacheControlEphemeralTypeEphemeral),
							}),
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

package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	a "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"log/slog"
	"strings"
	"time"
)

type anthropicExecutor struct {
	client  *a.Client
	logger  *slog.Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
}

func NewAnthropicExecutor(baseUrl string, apiKey string, logger *slog.Logger, ignorer *gitignore.GitIgnore, config GenConfig) Executor {
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
	return &anthropicExecutor{
		client:  client,
		logger:  logger,
		ignorer: ignorer,
		config:  config,
	}
}

func (s *anthropicExecutor) Execute(input string) error {
	params := a.BetaMessageNewParams{
		Model:       a.F(s.config.Model),
		MaxTokens:   a.F(int64(s.config.MaxTokens)),
		Temperature: a.F(float64(s.config.Temperature)),
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
				}
				var result *ToolResult
				var err error
				switch block.Name {
				case bashTool.Name:
					var params struct {
						Command string `json:"command"`
					}
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal bash tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &params); err != nil {
						return fmt.Errorf("failed to unmarshal bash tool arguments: %w", err)
					}
					result, err = executeBashTool(params.Command, s.logger)
				case fileEditor.Name:
					var params FileEditorParams
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal file editor tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &params); err != nil {
						return fmt.Errorf("failed to unmarshal file editor tool arguments: %w", err)
					}
					result, err = executeFileEditorTool(params, s.logger)
				case filesOverviewTool.Name:
					result, err = executeFilesOverviewTool(s.ignorer, s.logger)
				case getRelatedFilesTool.Name:
					var params struct {
						InputFiles []string `json:"input_files"`
					}
					jsonInput, marshalErr := json.Marshal(block.Input)
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal get related files tool input: %w", marshalErr)
					}
					if err := json.Unmarshal(jsonInput, &params); err != nil {
						return fmt.Errorf("failed to unmarshal get related files tool arguments: %w", err)
					}
					result, err = executeGetRelatedFilesTool(params.InputFiles, s.ignorer, s.logger)
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

package agent

import (
	"context"
	"encoding/gob"
	"fmt"
	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"io"
	"strings"
	"time"
)

type deepseekR1Executor struct {
	client  *oai.Client
	logger  Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
	params  *oai.ChatCompletionNewParams
}

func NewDeepSeekR1Executor(baseUrl string, apiKey string, logger Logger, ignorer *gitignore.GitIgnore, config GenConfig) Executor {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(3),
		option.WithRequestTimeout(10 * time.Minute),
	}
	if baseUrl != "" {
		if !strings.HasSuffix(baseUrl, "/") {
			baseUrl += "/"
		}
	} else {
		baseUrl = "https://api.deepseek.com/"
	}
	opts = append(opts, option.WithBaseURL(baseUrl))

	return &deepseekR1Executor{
		client:  oai.NewClient(opts...),
		logger:  logger,
		ignorer: ignorer,
		config:  config,
		params: &oai.ChatCompletionNewParams{
			Model:               oai.F(config.Model),
			MaxCompletionTokens: oai.Int(int64(config.MaxTokens)),
			Temperature:         oai.Float(float64(config.Temperature)),
			Messages:            oai.F([]oai.ChatCompletionMessageParamUnion{}),
		},
	}
}

func (d *deepseekR1Executor) Execute(input string) error {
	// Add messages to conversation
	d.params.Messages = oai.F(append(d.params.Messages.Value,
		oai.UserMessage(input),
	))

	// Get model response
	resp, err := d.client.Chat.Completions.New(context.Background(), *d.params)
	if err != nil {
		return fmt.Errorf("failed to create completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return fmt.Errorf("no completion choices returned")
	}

	// Log and store response
	response := resp.Choices[0].Message.Content
	d.logger.Println(response)

	// Add assistant response to message history
	d.params.Messages = oai.F(append(d.params.Messages.Value,
		oai.AssistantMessage(response),
	))

	return nil
}

func (d *deepseekR1Executor) LoadMessages(r io.Reader) error {
	var convo Conversation[[]oai.ChatCompletionMessageParamUnion]
	dec := gob.NewDecoder(r)
	if err := dec.Decode(&convo); err != nil {
		return fmt.Errorf("failed to decode conversation: %w", err)
	}
	d.params.Messages = oai.F(convo.Messages)
	return nil
}

func (d *deepseekR1Executor) SaveMessages(w io.Writer) error {
	convo := Conversation[[]oai.ChatCompletionMessageParamUnion]{
		Type:     "deepseek-r1",
		Messages: d.params.Messages.Value,
	}
	enc := gob.NewEncoder(w)
	return enc.Encode(convo)
}

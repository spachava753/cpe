package agent

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

type deepseekR1Executor struct {
	client  *oai.Client
	logger  *slog.Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
	params  *oai.ChatCompletionNewParams
}

func NewDeepSeekR1Executor(baseUrl string, apiKey string, logger *slog.Logger, ignorer *gitignore.GitIgnore, config GenConfig) Executor {
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

func (d *deepseekR1Executor) gatherContext(originalInput string) (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}

	ctxPrompt := `You are a context-gathering sub-agent. Your task is to:
1. Analyze the user's request and determine what information is needed to complete it
2. Gather relevant context WITHOUT making any actual changes
3. This may include:
   - Getting high-level view of the project files using files_overview
   - Retrieving related files with get_related_files
   - Running read-only bash commands to inspect system state
   - Checking package versions or dependencies
4. Return the gathered context in a concise, organized format`

	cmd := exec.Command(exePath,
		"Context gathering for:\n"+originalInput,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(ctxPrompt)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sub-agent failed: %v\nStderr: %s\nStdout: %s", err, stderr.String(), stdout.String())
	}

	return fmt.Sprintf("## Gathered Context\n%s\n\n## Original Task\n%s",
		strings.TrimSpace(stderr.String()), originalInput), nil
}

func (d *deepseekR1Executor) Execute(input string) error {
	// Gather context using sub-agent
	augmentedInput, err := d.gatherContext(input)
	if err != nil {
		return fmt.Errorf("context gathering failed: %w", err)
	}

	// Add messages to conversation
	d.params.Messages = oai.F(append(d.params.Messages.Value,
		oai.UserMessage(augmentedInput),
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
	d.logger.Info(response)

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

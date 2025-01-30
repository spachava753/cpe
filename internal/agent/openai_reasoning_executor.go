package agent

import (
	"context"
	_ "embed"
	"encoding/gob"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
)

type Actions struct {
	XMLName xml.Name       `xml:"actions"`
	Create  []CreateAction `xml:"create"`
	Delete  []DeleteAction `xml:"delete"`
	Modify  []ModifyAction `xml:"modify"`
}

type CreateAction struct {
	Path    string `xml:"path,attr"`
	Content string `xml:",chardata"`
}

type DeleteAction struct {
	Path string `xml:"path,attr"`
}

type ModifyAction struct {
	Path    string `xml:"path,attr"`
	Search  string `xml:"search"`
	Replace string `xml:"replace"`
}

type openAiReasoningExecutor struct {
	client  *oai.Client
	logger  Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
	params  *oai.ChatCompletionNewParams
}

func NewOpenAiReasoningExecutor(baseUrl string, apiKey string, logger Logger, ignorer *gitignore.GitIgnore, config GenConfig) Executor {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(3),
		option.WithRequestTimeout(10 * time.Minute),
	}
	if baseUrl != "" {
		if !strings.HasSuffix(baseUrl, "/") {
			baseUrl += "/"
		}
		opts = append(opts, option.WithBaseURL(baseUrl))
	}

	return &openAiReasoningExecutor{
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

func (o *openAiReasoningExecutor) Execute(input string) error {
	// Add messages to conversation
	o.params.Messages = oai.F(append(o.params.Messages.Value,
		oai.UserMessage(fmt.Sprintf("%s\n\n<input>\n%s\n</input>", reasoningAgentInstructions, input)),
	))

	// Get model response
	resp, err := o.client.Chat.Completions.New(context.Background(), *o.params)
	if err != nil {
		return fmt.Errorf("failed to create completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return fmt.Errorf("no completion choices returned")
	}

	// Log and store response
	response := resp.Choices[0].Message.Content
	o.logger.Println(response)

	// Parse and execute actions if present
	if err := o.parseAndExecuteActions(response); err != nil {
		return fmt.Errorf("failed to parse and execute actions: %w", err)
	}

	// Add assistant response to message history
	o.params.Messages = oai.F(append(o.params.Messages.Value,
		oai.AssistantMessage(response),
	))

	return nil
}

func (o *openAiReasoningExecutor) parseAndExecuteActions(response string) error {
	startTag := "<actions>"
	endTag := "</actions>"
	start := strings.Index(response, startTag)
	end := strings.Index(response, endTag)
	if start == -1 || end == -1 || start > end {
		// No actions found
		return nil
	}

	xmlContent := response[start : end+len(endTag)]

	var actions Actions
	if err := xml.Unmarshal([]byte(xmlContent), &actions); err != nil {
		return fmt.Errorf("error unmarshaling actions XML: %w", err)
	}

	// Execute create actions
	for _, create := range actions.Create {
		if err := createFile(create.Path, create.Content); err != nil {
			o.logger.Println(fmt.Sprintf("Error creating file %s: %v", create.Path, err))
		}
	}

	// Execute delete actions
	for _, del := range actions.Delete {
		if err := deleteFile(del.Path); err != nil {
			o.logger.Println(fmt.Sprintf("Error deleting file %s: %v", del.Path, err))
		}
	}

	// Execute modify actions
	for _, mod := range actions.Modify {
		if err := modifyFile(mod.Path, mod.Search, mod.Replace); err != nil {
			o.logger.Println(fmt.Sprintf("Error modifying file %s: %v", mod.Path, err))
		}
	}

	return nil
}

func createFile(path string, content string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directories for %s: %w", path, err)
	}

	// Write content to file
	return os.WriteFile(path, []byte(content), 0644)
}

func deleteFile(path string) error {
	return os.Remove(path)
}

func modifyFile(path string, search string, replace string) error {
	input, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", path, err)
	}

	updatedContent := strings.ReplaceAll(string(input), search, replace)
	return os.WriteFile(path, []byte(updatedContent), 0644)
}

func (o *openAiReasoningExecutor) LoadMessages(r io.Reader) error {
	var convo Conversation[[]oai.ChatCompletionMessageParamUnion]
	dec := gob.NewDecoder(r)
	if err := dec.Decode(&convo); err != nil {
		return fmt.Errorf("failed to decode conversation: %w", err)
	}
	o.params.Messages = oai.F(convo.Messages)
	return nil
}

func (o *openAiReasoningExecutor) SaveMessages(w io.Writer) error {
	convo := Conversation[[]oai.ChatCompletionMessageParamUnion]{
		Type:     "openai",
		Messages: o.params.Messages.Value,
	}
	enc := gob.NewEncoder(w)
	return enc.Encode(convo)
}

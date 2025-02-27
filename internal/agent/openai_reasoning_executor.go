package agent

import (
	"context"
	_ "embed"
	"encoding/gob"
	"encoding/xml"
	"fmt"
	"io"
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

	// Prepare the system prompt, potentially with thinking directive
	systemPrompt := reasoningAgentInstructions
	if config.ThinkingBudget != "" && config.ThinkingBudget != "0" {
		// Add thinking directive based on budget level
		var thinkingDirective string
		switch strings.ToLower(config.ThinkingBudget) {
		case "low":
			thinkingDirective = "Use minimal thinking for simple tasks."
		case "medium":
			thinkingDirective = "Use moderate thinking for complex analysis and planning."
		case "high":
			thinkingDirective = "Use extensive thinking for very complex tasks requiring deep analysis."
		default:
			// If a numeric value is provided, treat it as medium
			thinkingDirective = "Use moderate thinking for complex analysis and planning."
		}
		
		systemPrompt = fmt.Sprintf("%s\n\nThinking directive: %s", systemPrompt, thinkingDirective)
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

func (o *openAiReasoningExecutor) Execute(inputs []Input) error {
	// Only text input is supported
	var textInputs []string
	for _, input := range inputs {
		if input.Type != InputTypeText {
			return fmt.Errorf("input type %s is not supported by OpenAI Reasoning models, only text input is supported", input.Type)
		}
		textInputs = append(textInputs, input.Text)
	}
	input := strings.Join(textInputs, "\n")
	
	// Prepare the system prompt, potentially with thinking directive
	systemPrompt := reasoningAgentInstructions
	if o.config.ThinkingBudget != "" && o.config.ThinkingBudget != "0" {
		// Add thinking directive based on budget level
		var thinkingDirective string
		switch strings.ToLower(o.config.ThinkingBudget) {
		case "low":
			thinkingDirective = "Use minimal thinking for simple tasks."
		case "medium":
			thinkingDirective = "Use moderate thinking for complex analysis and planning."
		case "high":
			thinkingDirective = "Use extensive thinking for very complex tasks requiring deep analysis."
		default:
			// If a numeric value is provided, treat it as medium
			thinkingDirective = "Use moderate thinking for complex analysis and planning."
		}
		
		systemPrompt = fmt.Sprintf("%s\n\nThinking directive: %s", systemPrompt, thinkingDirective)
	}
	
	// Add system message first if none exists
	if len(o.params.Messages.Value) == 0 {
		o.params.Messages = oai.F([]oai.ChatCompletionMessageParamUnion{
			oai.SystemMessage(systemPrompt),
		})
	}
	
	// Add user message with input
	o.params.Messages = oai.F(append(o.params.Messages.Value,
		oai.UserMessage(fmt.Sprintf("<input>\n%s\n</input>", input)),
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

	// Convert create actions to FileEditorParams
	for _, create := range actions.Create {
		params := FileEditorParams{
			Command:  "create",
			Path:     create.Path,
			FileText: create.Content,
		}
		result, err := executeFileEditorTool(params)
		if err != nil {
			return fmt.Errorf("failed to execute create action: %w", err)
		}
		if result.IsError {
			o.logger.Println(fmt.Sprintf("Error creating file %s: %v", create.Path, result.Content))
		}
	}

	// Convert delete actions to FileEditorParams
	for _, del := range actions.Delete {
		params := FileEditorParams{
			Command: "remove",
			Path:    del.Path,
		}
		result, err := executeFileEditorTool(params)
		if err != nil {
			return fmt.Errorf("failed to execute delete action: %w", err)
		}
		if result.IsError {
			o.logger.Println(fmt.Sprintf("Error deleting file %s: %v", del.Path, result.Content))
		}
	}

	// Convert modify actions to FileEditorParams
	for _, mod := range actions.Modify {
		params := FileEditorParams{
			Command: "str_replace",
			Path:    mod.Path,
			OldStr:  mod.Search,
			NewStr:  mod.Replace,
		}
		result, err := executeFileEditorTool(params)
		if err != nil {
			return fmt.Errorf("failed to execute modify action: %w", err)
		}
		if result.IsError {
			o.logger.Println(fmt.Sprintf("Error modifying file %s: %v", mod.Path, result.Content))
		}
	}

	return nil
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

func (o *openAiReasoningExecutor) PrintMessages() string {
	if !o.params.Messages.Present {
		return ""
	}

	var sb strings.Builder
	for _, msg := range o.params.Messages.Value {
		switch m := msg.(type) {
		case oai.ChatCompletionMessageParam:
			if m.Role.Value == oai.ChatCompletionMessageParamRoleSystem {
				continue
			}
			switch m.Role.Value {
			case oai.ChatCompletionMessageParamRoleUser:
				sb.WriteString("USER:\n")
			case oai.ChatCompletionMessageParamRoleAssistant:
				sb.WriteString("ASSISTANT:\n")
			}
			if m.Content.Present {
				// For user messages, strip out the XML wrapper if present
				content := m.Content.Value.(string)
				if m.Role.Value == oai.ChatCompletionMessageParamRoleUser {
					if strings.Contains(content, reasoningAgentInstructions) {
						content = strings.TrimPrefix(content, reasoningAgentInstructions)
						content = strings.TrimSpace(content)
						if strings.HasPrefix(content, "<input>") && strings.HasSuffix(content, "</input>") {
							content = strings.TrimPrefix(content, "<input>")
							content = strings.TrimSuffix(content, "</input>")
						}
					}
				}
				sb.WriteString(content)
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

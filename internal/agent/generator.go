package agent

import (
	"context"
	_ "embed"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go"
	oaiopt "github.com/openai/openai-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/gai"
	"slices"
	"time"
)

var anthropicModels = []string{
	anthropic.ModelClaude3_7SonnetLatest,
	anthropic.ModelClaude3_7Sonnet20250219,
	anthropic.ModelClaude3_5HaikuLatest,
	anthropic.ModelClaude3_5Haiku20241022,
	anthropic.ModelClaude3_5SonnetLatest,
	anthropic.ModelClaude3_5Sonnet20241022,
	anthropic.ModelClaude_3_5_Sonnet_20240620,
	anthropic.ModelClaude3OpusLatest,
	anthropic.ModelClaude_3_Opus_20240229,
}

var openAiModels = []string{
	"o3",
	"o4-mini",
	"gpt-4.1",
	"gpt-4.1-mini",
	"gpt-4.1-nano",
	"gpt-4.1-2025-04-14",
	"gpt-4.1-mini-2025-04-14",
	"gpt-4.1-nano--2025-04-14",
	openai.ChatModelO3Mini,
	openai.ChatModelO3Mini2025_01_31,
	openai.ChatModelO1,
	openai.ChatModelO1_2024_12_17,
	openai.ChatModelO1Preview,
	openai.ChatModelO1Preview2024_09_12,
	openai.ChatModelO1Mini,
	openai.ChatModelO1Mini2024_09_12,
	openai.ChatModelGPT4o,
	openai.ChatModelGPT4o2024_11_20,
	openai.ChatModelGPT4o2024_08_06,
	openai.ChatModelGPT4o2024_05_13,
	openai.ChatModelGPT4oAudioPreview,
	openai.ChatModelGPT4oAudioPreview2024_10_01,
	openai.ChatModelGPT4oAudioPreview2024_12_17,
	openai.ChatModelGPT4oMiniAudioPreview,
	openai.ChatModelGPT4oMiniAudioPreview2024_12_17,
	openai.ChatModelChatgpt4oLatest,
	openai.ChatModelGPT4oMini,
	openai.ChatModelGPT4oMini2024_07_18,
	openai.ChatModelGPT4Turbo,
	openai.ChatModelGPT4Turbo2024_04_09,
	openai.ChatModelGPT4_0125Preview,
	openai.ChatModelGPT4TurboPreview,
	openai.ChatModelGPT4_1106Preview,
	openai.ChatModelGPT4VisionPreview,
	openai.ChatModelGPT4,
	openai.ChatModelGPT4_0314,
	openai.ChatModelGPT4_0613,
	openai.ChatModelGPT4_32k,
	openai.ChatModelGPT4_32k0314,
	openai.ChatModelGPT4_32k0613,
	openai.ChatModelGPT3_5Turbo,
	openai.ChatModelGPT3_5Turbo16k,
	openai.ChatModelGPT3_5Turbo0301,
	openai.ChatModelGPT3_5Turbo0613,
	openai.ChatModelGPT3_5Turbo1106,
	openai.ChatModelGPT3_5Turbo0125,
	openai.ChatModelGPT3_5Turbo16k0613,
}

var KnownModels = append(anthropicModels, openAiModels...)

//go:embed agent_instructions.txt
var agentInstructions string

// InitGenerator creates the appropriate generator based on the model name
func InitGenerator(model, baseURL string) (gai.ToolCapableGenerator, error) {
	// Handle OpenAI models
	if slices.Contains(openAiModels, model) {
		generator, err := createOpenAIGenerator(model, baseURL)
		if err != nil {
			return nil, err
		}
		// Convert to ToolCapableGenerator
		return generator.(gai.ToolCapableGenerator), nil
	}

	// Handle Anthropic models
	if slices.Contains(anthropicModels, model) {
		generator, err := createAnthropicGenerator(model, baseURL)
		if err != nil {
			return nil, err
		}
		// Convert to ToolCapableGenerator
		return generator.(gai.ToolCapableGenerator), nil
	}

	// Custom openai-compatible models require a base url
	if baseURL == "" {
		return nil, fmt.Errorf("unknown model: %s, must specfiy a custom url", model)
	}

	// custom model
	generator, err := createOpenAIGenerator(model, baseURL)
	if err != nil {
		return nil, err
	}
	// Convert to ToolCapableGenerator
	return generator.(gai.ToolCapableGenerator), nil
}

// createOpenAIGenerator creates and configures an OpenAI generator
func createOpenAIGenerator(model, baseURL string) (gai.Generator, error) {
	// Create OpenAI client
	var client openai.Client
	if baseURL != "" {
		client = openai.NewClient(oaiopt.WithBaseURL(baseURL))
	} else {
		client = openai.NewClient()
	}

	// Get system instructions
	sysInfo, err := GetSystemInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	// Get agent instructions with system info
	systemPrompt := fmt.Sprintf(agentInstructions, sysInfo.String())

	// Create the OpenAI generator
	generator := gai.NewOpenAiGenerator(
		&client.Chat.Completions,
		model,
		systemPrompt,
	)

	return &generator, nil
}

// createAnthropicGenerator creates and configures an Anthropic generator
func createAnthropicGenerator(model, baseURL string) (gai.Generator, error) {
	// Create Anthropic client
	var client anthropic.Client
	opts := []aopts.RequestOption{
		// Add a custom timeout to disable to the error returned if a
		// non-streaming request is expected to be above roughly 10 minutes long
		aopts.WithRequestTimeout(25 * time.Minute),
	}
	if baseURL != "" {
		opts = append(opts, aopts.WithBaseURL(baseURL))
	}

	client = anthropic.NewClient(opts...)

	// Get system instructions
	sysInfo, err := GetSystemInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	// Get agent instructions with system info
	systemPrompt := fmt.Sprintf(agentInstructions, sysInfo.String())

	// Create and return the Anthropic generator
	generator := gai.NewAnthropicGenerator(
		&client.Messages,
		model,
		systemPrompt,
	)

	return &generator, nil
}

// BashToolCallback is a callback for the bash tool
type BashToolCallback struct{}

// Call executes a bash command
func (t BashToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	command, ok := input["command"].(string)
	if !ok {
		return nil, fmt.Errorf("command parameter is required")
	}
	result, err := executeBashTool(command)
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// FilesOverviewToolCallback is a callback for the files overview tool
type FilesOverviewToolCallback struct {
	ignorer *gitignore.GitIgnore
}

// Call executes the files overview tool
func (t FilesOverviewToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	result, err := ExecuteFilesOverviewTool(t.ignorer)
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// GetRelatedFilesToolCallback is a callback for the get related files tool
type GetRelatedFilesToolCallback struct {
	ignorer *gitignore.GitIgnore
}

// Call executes the get related files tool
func (t GetRelatedFilesToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	inputFiles, ok := input["input_files"].([]any)
	if !ok {
		return nil, fmt.Errorf("input_files parameter is required")
	}

	// Convert to []string
	files := make([]string, len(inputFiles))
	for i, file := range inputFiles {
		files[i], ok = file.(string)
		if !ok {
			return nil, fmt.Errorf("input_files must be an array of strings")
		}
	}

	result, err := ExecuteGetRelatedFilesTool(files, t.ignorer)
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// CreateFileToolCallback is a callback for the create file tool
type CreateFileToolCallback struct{}

// Call executes the create file tool
func (t CreateFileToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	path, ok := input["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	fileText, ok := input["file_text"].(string)
	if !ok {
		return nil, fmt.Errorf("file_text parameter is required")
	}
	result, err := CreateFileTool(CreateFileParams{
		Path:     path,
		FileText: fileText,
	})
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// DeleteFileToolCallback is a callback for the delete file tool
type DeleteFileToolCallback struct{}

// Call executes the delete file tool
func (t DeleteFileToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	path, ok := input["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	result, err := DeleteFileTool(DeleteFileParams{
		Path: path,
	})
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// EditFileToolCallback is a callback for the edit file tool
type EditFileToolCallback struct{}

// Call executes the edit file tool
func (t EditFileToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	path, ok := input["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	oldStr, ok := input["old_str"].(string)
	if !ok {
		return nil, fmt.Errorf("old_str parameter is required")
	}
	newStr, ok := input["new_str"].(string)
	if !ok {
		return nil, fmt.Errorf("new_str parameter is required")
	}
	result, err := EditFileTool(EditFileParams{
		Path:   path,
		OldStr: oldStr,
		NewStr: newStr,
	})
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// MoveFileToolCallback is a callback for the move file tool
type MoveFileToolCallback struct{}

// Call executes the move file tool
func (t MoveFileToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	sourcePath, ok := input["source_path"].(string)
	if !ok {
		return nil, fmt.Errorf("source_path parameter is required")
	}
	targetPath, ok := input["target_path"].(string)
	if !ok {
		return nil, fmt.Errorf("target_path parameter is required")
	}
	result, err := MoveFileTool(MoveFileParams{
		SourcePath: sourcePath,
		TargetPath: targetPath,
	})
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// ViewFileToolCallback is a callback for the view file tool
type ViewFileToolCallback struct{}

// Call executes the view file tool
func (t ViewFileToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	path, ok := input["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	result, err := ViewFileTool(ViewFileParams{
		Path: path,
	})
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// CreateFolderToolCallback is a callback for the create folder tool
type CreateFolderToolCallback struct{}

// Call executes the create folder tool
func (t CreateFolderToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	path, ok := input["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	result, err := CreateFolderTool(CreateFolderParams{
		Path: path,
	})
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// DeleteFolderToolCallback is a callback for the delete folder tool
type DeleteFolderToolCallback struct{}

// Call executes the delete folder tool
func (t DeleteFolderToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	path, ok := input["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	// recursive is optional with a default value of false
	recursive := false
	if recursiveVal, ok := input["recursive"].(bool); ok {
		recursive = recursiveVal
	}

	result, err := DeleteFolderTool(DeleteFolderParams{
		Path:      path,
		Recursive: recursive,
	})
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// MoveFolderToolCallback is a callback for the move folder tool
type MoveFolderToolCallback struct{}

// Call executes the move folder tool
func (t MoveFolderToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	sourcePath, ok := input["source_path"].(string)
	if !ok {
		return nil, fmt.Errorf("source_path parameter is required")
	}
	targetPath, ok := input["target_path"].(string)
	if !ok {
		return nil, fmt.Errorf("target_path parameter is required")
	}
	result, err := MoveFolderTool(MoveFolderParams{
		SourcePath: sourcePath,
		TargetPath: targetPath,
	})
	if err != nil {
		return nil, err
	}
	return result.Content, nil
}

// RegisterTools registers all the necessary tools with the gai.ToolGenerator
func RegisterTools(toolGen *gai.ToolGenerator) error {
	// Create ignorer for file system tools
	ignorer, err := createIgnorer()
	if err != nil {
		return fmt.Errorf("failed to create ignorer: %w", err)
	}

	// Register bash tool
	if err := registerTool(toolGen, bashTool, &BashToolCallback{}); err != nil {
		return err
	}

	// Register files overview tool
	if err := registerTool(toolGen, filesOverviewTool, &FilesOverviewToolCallback{ignorer: ignorer}); err != nil {
		return err
	}

	// Register get related files tool
	if err := registerTool(toolGen, getRelatedFilesTool, &GetRelatedFilesToolCallback{ignorer: ignorer}); err != nil {
		return err
	}

	// Register file operation tools
	if err := registerTool(toolGen, createFileTool, &CreateFileToolCallback{}); err != nil {
		return err
	}
	if err := registerTool(toolGen, deleteFileTool, &DeleteFileToolCallback{}); err != nil {
		return err
	}
	if err := registerTool(toolGen, editFileTool, &EditFileToolCallback{}); err != nil {
		return err
	}
	if err := registerTool(toolGen, moveFileTool, &MoveFileToolCallback{}); err != nil {
		return err
	}
	if err := registerTool(toolGen, viewFileTool, &ViewFileToolCallback{}); err != nil {
		return err
	}

	// Register folder operation tools
	if err := registerTool(toolGen, createFolderTool, &CreateFolderToolCallback{}); err != nil {
		return err
	}
	if err := registerTool(toolGen, deleteFolderTool, &DeleteFolderToolCallback{}); err != nil {
		return err
	}
	if err := registerTool(toolGen, moveFolderTool, &MoveFolderToolCallback{}); err != nil {
		return err
	}

	return nil
}

// createIgnorer creates an ignorer for file system operations
func createIgnorer() (*gitignore.GitIgnore, error) {
	// Use the existing ignore.LoadIgnoreFiles function to create an ignorer
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		return nil, fmt.Errorf("failed to load ignore files: %w", err)
	}
	if ignorer == nil {
		return nil, fmt.Errorf("git ignorer was nil")
	}
	return ignorer, nil
}

// registerTool registers a tool with the tool generator
func registerTool(toolGen *gai.ToolGenerator, tool gai.Tool, callback gai.ToolCallback) error {
	// Register the tool with the callback
	if err := toolGen.Register(tool, callback); err != nil {
		return fmt.Errorf("failed to register %s tool: %w", tool.Name, err)
	}

	return nil
}

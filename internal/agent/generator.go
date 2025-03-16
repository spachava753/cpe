package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/gai"
)

// GetToolGenerator creates and returns a gai.ToolGenerator with all necessary tools registered
// based on the specified model, base URL, and generation options.
func GetToolGenerator(model, baseURL string, genOpts *gai.GenOpts) (*gai.ToolGenerator, error) {
	// Create the underlying generator based on the model name
	baseGenerator, err := getBaseGenerator(model, baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create base generator: %w", err)
	}

	// Create the tool generator
	toolGen := &gai.ToolGenerator{
		G: baseGenerator,
	}

	// Register all the necessary tools
	if err := registerTools(toolGen); err != nil {
		return nil, fmt.Errorf("failed to register tools: %w", err)
	}

	return toolGen, nil
}

// getBaseGenerator creates the appropriate generator based on the model name
func getBaseGenerator(model, baseURL string) (gai.ToolCapableGenerator, error) {
	// Handle OpenAI models
	if isOpenAIModel(model) {
		generator, err := createOpenAIGenerator(model, baseURL)
		if err != nil {
			return nil, err
		}
		// Convert to ToolCapableGenerator
		return generator.(gai.ToolCapableGenerator), nil
	}

	// Handle Anthropic models
	if isAnthropicModel(model) {
		generator, err := createAnthropicGenerator(model, baseURL)
		if err != nil {
			return nil, err
		}
		// Convert to ToolCapableGenerator
		return generator.(gai.ToolCapableGenerator), nil
	}

	return nil, fmt.Errorf("unsupported model: %s", model)
}

// isOpenAIModel checks if the model is an OpenAI model
func isOpenAIModel(model string) bool {
	openAIModels := []string{"gpt-4o", "gpt-4o-mini", "o3-mini"}
	for _, m := range openAIModels {
		if strings.Contains(model, m) {
			return true
		}
	}
	return false
}

// isAnthropicModel checks if the model is an Anthropic model
func isAnthropicModel(model string) bool {
	anthropicModels := []string{"claude"}
	for _, m := range anthropicModels {
		if strings.Contains(model, m) {
			return true
		}
	}
	return false
}

// createOpenAIGenerator creates and configures an OpenAI generator
func createOpenAIGenerator(model, baseURL string) (gai.Generator, error) {
	// Create OpenAI client 
	var client *openai.Client
	if baseURL != "" {
		client = openai.NewClient()
		// Can't use WithBaseURL directly, would need to set client options
	} else {
		client = openai.NewClient()
	}

	// Get system instructions
	sysInfo, err := GetSystemInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	// Get agent instructions with system info
	systemPrompt, err := formatAgentInstructions(sysInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to format agent instructions: %w", err)
	}

	// Create the OpenAI generator
	generator := gai.NewOpenAiGenerator(
		client.Chat.Completions,
		model,
		systemPrompt,
	)

	return &generator, nil
}

// createAnthropicGenerator creates and configures an Anthropic generator
func createAnthropicGenerator(model, baseURL string) (gai.Generator, error) {
	// Create Anthropic client
	var client *anthropic.Client
	if baseURL != "" {
		client = anthropic.NewClient()
		// Can't use WithBaseURL directly, would need to set client options
	} else {
		client = anthropic.NewClient()
	}

	// Get system instructions
	sysInfo, err := GetSystemInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	// Get agent instructions with system info
	systemPrompt, err := formatAgentInstructions(sysInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to format agent instructions: %w", err)
	}

	// Create and return the Anthropic generator
	generator := gai.NewAnthropicGenerator(
		client.Messages,
		model,
		systemPrompt,
	)

	return &generator, nil
}

// formatAgentInstructions formats the agent instructions with system info
func formatAgentInstructions(sysInfo *SystemInfo) (string, error) {
	// Read agent instructions template from agent_instructions.txt
	// The template contains a %s placeholder for system info
	instructions, err := getAgentInstructions()
	if err != nil {
		return "", fmt.Errorf("failed to get agent instructions: %w", err)
	}

	// Format the instructions with the system info
	return fmt.Sprintf(instructions, sysInfo.String()), nil
}

// getAgentInstructions returns the agent instructions template
func getAgentInstructions() (string, error) {
	// Read the agent instructions from the agent_instructions.txt file
	data, err := os.ReadFile("internal/agent/agent_instructions.txt")
	if err != nil {
		return "", fmt.Errorf("failed to read agent instructions: %w", err)
	}
	return string(data), nil
}

// init initializes the package
func init() {
	// Nothing needed here now that we're reading from the file directly
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

// ChangeDirectoryToolCallback is a callback for the change directory tool
type ChangeDirectoryToolCallback struct{}

// Call executes the change directory tool
func (t ChangeDirectoryToolCallback) Call(ctx context.Context, input map[string]any) (any, error) {
	path, ok := input["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	result, err := executeChangeDirectoryTool(path)
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

// registerTools registers all the necessary tools with the tool generator
func registerTools(toolGen *gai.ToolGenerator) error {
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

	// Register change directory tool
	if err := registerTool(toolGen, changeDirectoryTool, &ChangeDirectoryToolCallback{}); err != nil {
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

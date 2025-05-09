package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	ignore "github.com/sabhiram/go-gitignore"
)

// bashToolHandler implements the logic for the bash tool.
func bashToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, ok := request.Params.Arguments["command"].(string)
	if !ok {
		return mcp.NewToolResultError("command parameter must be a string"), nil
	}

	// Re-implement the bash tool logic since executeBashTool is not exported
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Env = os.Environ()

	combined, err := cmd.CombinedOutput()
	// Print the combined output
	if len(combined) > 0 {
		os.Stdout.Write(combined)
	}

	// Handle exit code
	exitCode := 0
	if err != nil {
		// Try to extract the exit code from the error
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(interface{ ExitStatus() int }); ok {
				exitCode = status.ExitStatus()
			} else {
				exitCode = 1 // fallback if we can't extract
			}
		} else {
			exitCode = 1 // fallback
		}
	}

	if exitCode != 0 {
		return mcp.NewToolResultError(fmt.Sprintf("command failed with exit code %d; output:\n%s", exitCode, string(combined))), nil
	}

	return mcp.NewToolResultText(string(combined)), nil
}

// filesOverviewToolHandler creates a handler for the files_overview tool.
func filesOverviewToolHandler(ignorer *ignore.GitIgnore) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, _ := request.Params.Arguments["path"].(string) // Optional

		// Get the callback function that returns filesOverview functionality
		callback := CreateExecuteFilesOverviewFunc(ignorer)
		// Call the function with our input
		output, err := callback(ctx, FileOverviewInput{Path: path})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("files_overview failed: %v", err)), nil
		}
		return mcp.NewToolResultText(output), nil
	}
}

// getRelatedFilesToolHandler creates a handler for the get_related_files tool.
func getRelatedFilesToolHandler(ignorer *ignore.GitIgnore) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		inputFilesArg, ok := request.Params.Arguments["input_files"].([]interface{})
		if !ok {
			return mcp.NewToolResultError("input_files parameter must be an array"), nil
		}
		var inputFiles []string
		for _, item := range inputFilesArg {
			if strItem, ok := item.(string); ok {
				inputFiles = append(inputFiles, strItem)
			} else {
				return mcp.NewToolResultError("input_files array must contain only strings"), nil
			}
		}
		if len(inputFiles) == 0 {
			return mcp.NewToolResultError("input_files must not be empty"), nil
		}

		// Get the callback function that returns related files functionality
		callback := CreateExecuteGetRelatedFilesFunc(ignorer)
		// Call the function with our input
		output, err := callback(ctx, GetRelatedFilesInput{InputFiles: inputFiles})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("get_related_files failed: %v", err)), nil
		}
		return mcp.NewToolResultText(output), nil
	}
}

// createFileToolHandler implements the logic for the create_file tool.
func createFileToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, okPath := request.Params.Arguments["path"].(string)
	fileText, okFileText := request.Params.Arguments["file_text"].(string)

	if !okPath {
		return mcp.NewToolResultError("path parameter must be a string"), nil
	}
	if !okFileText {
		return mcp.NewToolResultError("file_text parameter must be a string"), nil
	}

	result, err := ExecuteCreateFile(ctx, CreateFileInput{
		Path:     path,
		FileText: fileText,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create_file failed: %v", err)), nil
	}
	return mcp.NewToolResultText(result), nil
}

// deleteFileToolHandler implements the logic for the delete_file tool.
func deleteFileToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return mcp.NewToolResultError("path parameter must be a string"), nil
	}

	result, err := ExecuteDeleteFile(ctx, DeleteFileInput{
		Path: path,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("delete_file failed: %v", err)), nil
	}
	return mcp.NewToolResultText(result), nil
}

// editFileToolHandler implements the logic for the edit_file tool.
func editFileToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return mcp.NewToolResultError("path parameter must be a string"), nil
	}
	oldStr, _ := request.Params.Arguments["old_str"].(string) // Optional
	newStr, _ := request.Params.Arguments["new_str"].(string) // Optional

	result, err := ExecuteEditFile(ctx, EditFileInput{
		Path:   path,
		OldStr: oldStr,
		NewStr: newStr,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("edit_file failed: %v", err)), nil
	}
	return mcp.NewToolResultText(result), nil
}

// moveFileToolHandler implements the logic for the move_file tool.
func moveFileToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sourcePath, okSource := request.Params.Arguments["source_path"].(string)
	targetPath, okTarget := request.Params.Arguments["target_path"].(string)

	if !okSource {
		return mcp.NewToolResultError("source_path parameter must be a string"), nil
	}
	if !okTarget {
		return mcp.NewToolResultError("target_path parameter must be a string"), nil
	}

	result, err := ExecuteMoveFile(ctx, MoveFileInput{
		SourcePath: sourcePath,
		TargetPath: targetPath,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("move_file failed: %v", err)), nil
	}
	return mcp.NewToolResultText(result), nil
}

// viewFileToolHandler implements the logic for the view_file tool.
func viewFileToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return mcp.NewToolResultError("path parameter must be a string"), nil
	}

	result, err := ExecuteViewFile(ctx, ViewFileInput{
		Path: path,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("view_file failed: %v", err)), nil
	}
	return mcp.NewToolResultText(result), nil
}

// createFolderToolHandler implements the logic for the create_folder tool.
func createFolderToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return mcp.NewToolResultError("path parameter must be a string"), nil
	}

	result, err := ExecuteCreateFolder(ctx, CreateFolderInput{
		Path: path,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create_folder failed: %v", err)), nil
	}
	return mcp.NewToolResultText(result), nil
}

// deleteFolderToolHandler implements the logic for the delete_folder tool.
func deleteFolderToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return mcp.NewToolResultError("path parameter must be a string"), nil
	}
	recursive, _ := request.Params.Arguments["recursive"].(bool) // Optional, defaults to false

	result, err := ExecuteDeleteFolder(ctx, DeleteFolderInput{
		Path:      path,
		Recursive: recursive,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("delete_folder failed: %v", err)), nil
	}
	return mcp.NewToolResultText(result), nil
}

// moveFolderToolHandler implements the logic for the move_folder tool.
func moveFolderToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sourcePath, okSource := request.Params.Arguments["source_path"].(string)
	targetPath, okTarget := request.Params.Arguments["target_path"].(string)

	if !okSource {
		return mcp.NewToolResultError("source_path parameter must be a string"), nil
	}
	if !okTarget {
		return mcp.NewToolResultError("target_path parameter must be a string"), nil
	}

	result, err := ExecuteMoveFolder(ctx, MoveFolderInput{
		SourcePath: sourcePath,
		TargetPath: targetPath,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("move_folder failed: %v", err)), nil
	}
	return mcp.NewToolResultText(result), nil
}

// NewStdioMCPServer creates a new MCP server instance and registers all native tools.
func NewStdioMCPServer(ignorer *ignore.GitIgnore) *server.MCPServer {
	s := server.NewMCPServer(
		"CPE Native Tools", // Server name
		"1.0.0",            // Server version - consider making this dynamic from build info
	)

	// Register tools with their handlers
	// Tool definitions (bashTool, filesOverviewTool, etc.) are in tools.go
	s.AddTool(bashTool, bashToolHandler)
	s.AddTool(filesOverviewTool, filesOverviewToolHandler(ignorer))
	s.AddTool(getRelatedFilesTool, getRelatedFilesToolHandler(ignorer))

	s.AddTool(createFileTool, createFileToolHandler)
	s.AddTool(deleteFileTool, deleteFileToolHandler)
	s.AddTool(editFileTool, editFileToolHandler)
	s.AddTool(moveFileTool, moveFileToolHandler)
	s.AddTool(viewFileTool, viewFileToolHandler)

	s.AddTool(createFolderTool, createFolderToolHandler)
	s.AddTool(deleteFolderTool, deleteFolderToolHandler)
	s.AddTool(moveFolderTool, moveFolderToolHandler)

	return s
}

// ServeStdio starts the MCP server on stdio.
func ServeStdio(s *server.MCPServer) error {
	if s == nil {
		return fmt.Errorf("server instance is nil, cannot serve")
	}
	return server.ServeStdio(s)
}

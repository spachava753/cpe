package mcp

import (
	"fmt"
	"github.com/spachava753/cpe/internal/version"

	"github.com/mark3labs/mcp-go/server"
	ignore "github.com/sabhiram/go-gitignore"
)

// NewStdioMCPServer creates a new MCP server instance and registers all native tools.
func NewStdioMCPServer(ignorer *ignore.GitIgnore) *server.MCPServer {
	s := server.NewMCPServer(
		"CPE Native Tools", // Server name
		version.Get(),      // Server version
	)

	// Create tool instance structs
	filesOverviewToolImpl := &FilesOverviewTool{Ignorer: ignorer}
	getRelatedFilesToolImpl := &GetRelatedFilesTool{Ignorer: ignorer}

	// Register all tools with the generic handler
	s.AddTool(bashTool, createToolHandler(executeBashTool))
	s.AddTool(filesOverviewTool, createToolHandler(filesOverviewToolImpl.Execute))
	s.AddTool(getRelatedFilesTool, createToolHandler(getRelatedFilesToolImpl.Execute))
	s.AddTool(createFileTool, createToolHandler(ExecuteCreateFile))
	s.AddTool(deleteFileTool, createToolHandler(ExecuteDeleteFile))
	s.AddTool(editFileTool, createToolHandler(ExecuteEditFile))
	s.AddTool(moveFileTool, createToolHandler(ExecuteMoveFile))
	s.AddTool(viewFileTool, createToolHandler(ExecuteViewFile))
	s.AddTool(createFolderTool, createToolHandler(ExecuteCreateFolder))
	s.AddTool(deleteFolderTool, createToolHandler(ExecuteDeleteFolder))
	s.AddTool(moveFolderTool, createToolHandler(ExecuteMoveFolder))

	return s
}

// ServeStdio starts the MCP server on stdio.
func ServeStdio(s *server.MCPServer) error {
	if s == nil {
		return fmt.Errorf("server instance is nil, cannot serve")
	}
	return server.ServeStdio(s)
}

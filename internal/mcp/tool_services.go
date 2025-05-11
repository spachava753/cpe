package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/codemap"
	"github.com/spachava753/cpe/internal/symbolresolver"
)

// FileOverviewService handles file overview operations with its dependencies
type FileOverviewService struct {
	ignorer *ignore.GitIgnore
}

type FileOverviewInput struct {
	Path string `json:"path"`
}

func (f FileOverviewInput) Validate() error {
	if f.Path == "" {
		return nil
	}

	// Check if the path exists
	fileInfo, err := os.Stat(f.Path)
	if err != nil {
		return fmt.Errorf("error: the specified path '%s' does not exist or is not accessible", f.Path)
	}

	// Check if the path is a file instead of a directory
	if !fileInfo.IsDir() {
		return fmt.Errorf("error: the specified path '%s' is a file, not a directory. The path should be a relative file path to a folder. If you want to view a single file, you should use the view_file tool instead", f.Path)
	}

	return nil
}

// Execute implements the tool functionality for files_overview
func (s *FileOverviewService) Execute(ctx context.Context, input FileOverviewInput) (string, error) {
	if input.Path == "" {
		input.Path = "."
	}

	// Continue with the directory processing
	fsys := os.DirFS(input.Path)
	files, err := codemap.GenerateOutput(fsys, 100, s.ignorer)
	if err != nil {
		return "", fmt.Errorf("error: failed to generate code map for '%s': %v", input.Path, err)
	}

	var sb strings.Builder
	for _, file := range files {
		sb.WriteString(fmt.Sprintf("File: %s\nContent:\n```%s```\n\n", file.Path, file.Content))
	}

	return sb.String(), nil
}

// RelatedFilesService handles file relations operations with its dependencies
type RelatedFilesService struct {
	ignorer *ignore.GitIgnore
}

// NewRelatedFilesService creates a new RelatedFilesService
func NewRelatedFilesService(ignorer *ignore.GitIgnore) *RelatedFilesService {
	return &RelatedFilesService{
		ignorer: ignorer,
	}
}

type GetRelatedFilesInput struct {
	InputFiles []string `json:"input_files"`
}

func (g GetRelatedFilesInput) Validate() error {
	if len(g.InputFiles) == 0 {
		return errors.New("input_files is required and must not be empty")
	}
	return nil
}

// Execute implements the tool functionality for get_related_files
func (s *RelatedFilesService) Execute(ctx context.Context, input GetRelatedFilesInput) (string, error) {
	// Check all input files exist before continuing.
	var missing []string
	for _, file := range input.InputFiles {
		if _, err := os.Stat(file); err != nil {
			missing = append(missing, file)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("the following input files do not exist or are not accessible: %s", strings.Join(missing, ", "))
	}

	relatedFiles, err := symbolresolver.ResolveTypeAndFunctionFiles(input.InputFiles, os.DirFS("."), s.ignorer)
	if err != nil {
		return "", fmt.Errorf("failed to resolve related files: %v", err)
	}

	// Convert map to sorted slice for consistent output
	var files []string
	for file := range relatedFiles {
		files = append(files, file)
	}
	sort.Strings(files)

	var sb strings.Builder
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %v", file, err)
		}
		sb.WriteString(fmt.Sprintf("File: %s\nContent:\n```%s```\n\n", file, string(content)))
	}

	return sb.String(), nil
}

// FilesOverviewTool encapsulates the files_overview tool with its dependencies
type FilesOverviewTool struct {
	Ignorer *ignore.GitIgnore
}

// Execute implements the files_overview tool logic
func (t *FilesOverviewTool) Execute(ctx context.Context, input FileOverviewInput) (string, error) {
	if input.Path == "" {
		input.Path = "."
	}

	// Continue with the directory processing
	fsys := os.DirFS(input.Path)
	files, err := codemap.GenerateOutput(fsys, 100, t.Ignorer)
	if err != nil {
		return "", fmt.Errorf("error: failed to generate code map for '%s': %v", input.Path, err)
	}

	var sb strings.Builder
	for _, file := range files {
		sb.WriteString(fmt.Sprintf("File: %s\nContent:\n```%s```\n\n", file.Path, file.Content))
	}

	return sb.String(), nil
}

// GetRelatedFilesTool encapsulates the get_related_files tool with its dependencies
type GetRelatedFilesTool struct {
	Ignorer *ignore.GitIgnore
}

// Execute implements the get_related_files tool logic
func (t *GetRelatedFilesTool) Execute(ctx context.Context, input GetRelatedFilesInput) (string, error) {
	// Check all input files exist before continuing.
	var missing []string
	for _, file := range input.InputFiles {
		if _, err := os.Stat(file); err != nil {
			missing = append(missing, file)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("the following input files do not exist or are not accessible: %s", strings.Join(missing, ", "))
	}

	relatedFiles, err := symbolresolver.ResolveTypeAndFunctionFiles(input.InputFiles, os.DirFS("."), t.Ignorer)
	if err != nil {
		return "", fmt.Errorf("failed to resolve related files: %v", err)
	}

	// Convert map to sorted slice for consistent output
	var files []string
	for file := range relatedFiles {
		files = append(files, file)
	}
	sort.Strings(files)

	var sb strings.Builder
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %v", file, err)
		}
		sb.WriteString(fmt.Sprintf("File: %s\nContent:\n```%s```\n\n", file, string(content)))
	}

	return sb.String(), nil
}

// BashToolInput represents the parameters for the bash tool
type BashToolInput struct {
	Command string `json:"command"`
}

func (b BashToolInput) Validate() error {
	if b.Command == "" {
		return errors.New("command is required")
	}
	return nil
}

// executeBashTool implements bash command execution
func executeBashTool(ctx context.Context, input BashToolInput) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", input.Command)
	cmd.Env = os.Environ()

	combined, err := cmd.CombinedOutput()
	// Print the combined output EXACTLY as bash would (no color, no splitting)
	if len(combined) > 0 {
		os.Stdout.Write(combined)
	}

	// Print exit code at the end similar to shell style
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
		fmt.Println(errStyle.Render(fmt.Sprintf("exit code: %d", exitCode)))
		return "", fmt.Errorf("command failed with exit code %d; output:\n%s", exitCode, string(combined))
	}

	fmt.Println(outStyle.Render("exit code: 0"))
	return string(combined), nil
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Output struct {
	Args    string `json:"args"`    // The arguments to pass to cpe
	Comment string `json:"comment"` // The comment body to pass to cpe (may be empty)
}

type GitHubPayload struct {
	Comment struct {
		Body string `json:"body"`
	} `json:"comment"`
}

func parseComment(comment string) Output {
	// Normalize line endings and trim trailing whitespace
	comment = strings.TrimRight(strings.ReplaceAll(comment, "\r\n", "\n"), "\n")
	lines := strings.Split(comment, "\n")

	// Check if .cpeconvo exists to determine if this is the first conversation
	_, err := os.Stat(".cpeconvo")
	isFirstConversation := os.IsNotExist(err)

	// Check if comment starts with "---"
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "---") {
		// No header found, return default
		if isFirstConversation {
			return Output{
				Args:    "", // No args for first conversation
				Comment: comment,
			}
		}
		return Output{
			Args:    "-continue last",
			Comment: comment,
		}
	}

	// Find the end of the header
	var headerEnd int
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "---") {
			headerEnd = i
			break
		}
	}

	// If we didn't find the end of the header,
	// treat the whole thing as a regular comment
	if headerEnd == 0 {
		if isFirstConversation {
			return Output{
				Args:    "", // No args for first conversation
				Comment: comment,
			}
		}
		return Output{
			Args:    "-continue last",
			Comment: comment,
		}
	}

	// Extract header content (everything between the --- markers)
	header := strings.TrimSpace(strings.Join(lines[1:headerEnd], "\n"))
	
	// If header is empty, treat as regular comment
	if header == "" {
		if isFirstConversation {
			return Output{
				Args:    "", // No args for first conversation
				Comment: strings.TrimSpace(strings.Join(lines[headerEnd+1:], "\n")),
			}
		}
		return Output{
			Args:    "-continue last",
			Comment: strings.TrimSpace(strings.Join(lines[headerEnd+1:], "\n")),
		}
	}

	// Extract comment body (everything after the second ---)
	commentBody := strings.TrimSpace(strings.Join(lines[headerEnd+1:], "\n"))

	return Output{
		Args:    header,
		Comment: commentBody,
	}
}

func readGitHubEvent(eventPath string) (*GitHubPayload, error) {
	f, err := os.Open(eventPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open GitHub event file: %w", err)
	}
	defer f.Close()

	var payload GitHubPayload
	if err := json.NewDecoder(f).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub event: %w", err)
	}

	return &payload, nil
}

func executeCPE(args string, input string, outputFile string) error {
	// Split the args string into individual arguments
	var argSlice []string
	if args != "" {
		argSlice = strings.Fields(args)
	}

	// Prepare the command
	cmd := exec.Command("cpe", argSlice...)

	// Create pipes for stdin and combined stdout/stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// Create the output file
	out, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	// Write the header
	if _, err := fmt.Fprintln(out, "### CPE Response"); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return fmt.Errorf("failed to write header newline: %w", err)
	}

	// Set up command output to write to both the file and a buffer
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(out, &buf)
	cmd.Stderr = io.MultiWriter(out, &buf)

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cpe: %w", err)
	}

	// Write input to stdin in a goroutine
	go func() {
		if input != "" {
			if _, err := io.WriteString(stdin, input); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write to stdin: %v\n", err)
			}
		}
		stdin.Close()
	}()

	// Wait for the command to complete
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("cpe execution failed: %w", err)
	}

	return nil
}

func main() {
	// Get GitHub event path from environment
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		fmt.Fprintln(os.Stderr, "GITHUB_EVENT_PATH environment variable not set")
		os.Exit(1)
	}

	// Get output file path from arguments
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: process_pr_comment <output_file>")
		os.Exit(1)
	}
	outputFile := os.Args[1]

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputFile), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	// Read GitHub event
	payload, err := readGitHubEvent(eventPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read GitHub event: %v\n", err)
		os.Exit(1)
	}

	// Parse the comment
	output := parseComment(payload.Comment.Body)

	// Execute cpe with the parsed arguments and comment
	if err := executeCPE(output.Args, output.Comment, outputFile); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to execute cpe: %v\n", err)
		os.Exit(1)
	}
}
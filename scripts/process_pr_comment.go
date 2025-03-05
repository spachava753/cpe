package main

import (
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
			Args:    "", // No args needed since continue is default behavior
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
			Args:    "", // No args needed since continue is default behavior
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
			Args:    "", // No args needed since continue is default behavior
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

	// Set up command output to write to both the file and stderr/stdout
	cmd.Stdout = io.MultiWriter(out, os.Stdout)
	cmd.Stderr = io.MultiWriter(out, os.Stderr)

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cpe: %w", err)
	}

	// Check if this is the first conversation
	_, err = os.Stat(".cpeconvo")
	isFirstConversation := os.IsNotExist(err)

	// Write input to stdin in a goroutine
	go func() {
		// If this is the first conversation and there's existing input, add the GitHub Actions preamble
		if isFirstConversation && input != "" {
			preamble := `You are running inside a GitHub Actions workflow. Note that while you can help analyze and modify code, you cannot directly make changes to GitHub Actions workflow files.
In particular, this means you can view the workflows files in .github/workflows/*, but cannot edit, or remove them.

If you edit any files, make sure the commit those changes towards the end, as once the Github Actions workflow you are running inside of finishes its execution, all local filesystem state is wiped. So ensure that any changes you wish to keep is commited.
Also, ensure you clean up any temporary files (test files, summary files, etc.) unless I specifically ask you to keep them.

---

`
			if _, err := io.WriteString(stdin, preamble+input); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write to stdin: %v\n", err)
			}
		} else if input != "" {
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

package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/gobwas/glob"
	"github.com/spachava753/cpe/parser"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type RequestBody struct {
	Model         string    `json:"model"`
	MaxTokens     int       `json:"max_tokens"`
	Messages      []Message `json:"messages"`
	SystemMessage string    `json:"system"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type ResponseBody struct {
	ID           string        `json:"id"`
	Type         string        `json:"type"`
	Role         string        `json:"role"`
	Model        string        `json:"model"`
	Content      []ContentItem `json:"content"`
	StopReason   string        `json:"stop_reason"`
	StopSequence interface{}   `json:"stop_sequence"`
	Usage        Usage         `json:"usage"`
}

//go:embed system_prompt.txt
var systemPromptTemplate string

func readIgnorePatterns(filename string) ([]glob.Glob, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var patterns []glob.Glob
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			pattern, err := glob.Compile(line)
			if err != nil {
				return nil, fmt.Errorf("invalid pattern %q: %v", line, err)
			}
			patterns = append(patterns, pattern)
		}
	}
	return patterns, nil
}

var ignoreFolders = []string{
	".git",
	"vendor",
	"node_modules",
	".idea",
	".vscode",
	"bin",
	"obj",
	"dist",
	"build",
	"target",
}

func buildSystemMessage() (string, error) {
	var systemMessage strings.Builder
	systemMessage.WriteString(systemPromptTemplate)

	ignorePatterns, err := readIgnorePatterns(".cpeignore")
	if err != nil {
		return "", fmt.Errorf("error reading .cpeignore: %v", err)
	}

	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if the current path is in the ignore list
		for _, folder := range ignoreFolders {
			if strings.HasPrefix(path, folder) || strings.Contains(path, "/"+folder+"/") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if !info.IsDir() {
			// Check if the file should be ignored based on .cpeignore patterns
			for _, pattern := range ignorePatterns {
				if pattern.Match(path) {
					return nil // Skip this file
				}
			}

			content, readFileErr := os.ReadFile(path)
			if readFileErr != nil {
				return readFileErr
			}
			systemMessage.WriteString(fmt.Sprintf(`<file>
<path>%s</path>
<code>
%s
</code>
</file>
`, path, string(content)))
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	return systemMessage.String(), nil
}

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Println("ANTHROPIC_API_KEY environment variable is not set")
		return
	}

	url := "https://api.anthropic.com/v1/messages"

	// Build system message
	systemMessage, err := buildSystemMessage()
	if err != nil {
		fmt.Println("Error building system message:", err)
		return
	}

	// Write system message to system_prompt.md
	err = os.WriteFile("system_prompt.md", []byte(systemMessage), 0644)
	if err != nil {
		fmt.Println("Error writing to system_prompt.md:", err)
		return
	}
	fmt.Println("System prompt written to system_prompt.md")

	// Read content from stdin
	reader := bufio.NewReader(os.Stdin)
	contentBytes, readErr := io.ReadAll(reader)
	if readErr != nil {
		fmt.Println("No input provided")
		os.Exit(1)
	}

	content := string(contentBytes)

	if len(content) == 0 {
		fmt.Println("Error: No input provided. Please provide input via stdin.")
		return
	}

	requestBody := RequestBody{
		Model:     "claude-3-5-sonnet-20240620",
		MaxTokens: 8192,
		Messages: []Message{
			{Role: "user", Content: content},
		},
		SystemMessage: systemMessage,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		fmt.Println("Error creating request:", err)
		return
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "max-tokens-3-5-sonnet-2024-07-15")
	req.Header.Set("content-type", "application/json")

	client := &http.Client{
		Timeout: 2 * time.Minute,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: Status code %d\n", resp.StatusCode)
		fmt.Println("Response body:", string(body))
		return
	}

	var responseBody ResponseBody
	err = json.Unmarshal(body, &responseBody)
	if err != nil {
		fmt.Println("Error parsing response JSON:", err)
		return
	}

	fmt.Println("Response parsed successfully:")
	fmt.Printf("ID: %s\n", responseBody.ID)
	fmt.Printf("Role: %s\n", responseBody.Role)
	fmt.Printf("Model: %s\n", responseBody.Model)

	var fullContent strings.Builder
	for _, item := range responseBody.Content {
		if item.Type == "text" {
			fullContent.WriteString(item.Text)
			if len(item.Text) > 50 {
				fmt.Printf("Content (truncated): %s...\n", item.Text[:50])
			} else {
				fmt.Printf("Content: %s\n", item.Text)
			}
		}
	}

	fmt.Printf("Stop Reason: %s\n", responseBody.StopReason)
	fmt.Printf("Input Tokens: %d\n", responseBody.Usage.InputTokens)
	fmt.Printf("Output Tokens: %d\n", responseBody.Usage.OutputTokens)

	// Print full content to stdout
	fmt.Println("\n--- Full Content ---")
	fmt.Println(fullContent.String())
	fmt.Println("--- End of Content ---")

	// Parse modifications
	modifications, err := parser.ParseModifications(fullContent.String())
	if err != nil {
		fmt.Printf("Error parsing modifications: %v\n", err)
		return
	}

	// Print parsed modifications
	fmt.Println("\n--- Parsed Modifications ---")
	for _, mod := range modifications {
		fmt.Printf("Type: %s\n", mod.Type())
		switch m := mod.(type) {
		case parser.ModifyCode:
			fmt.Printf("  Path: %s\n", m.Path)
			fmt.Printf("  Modifications: %d\n", len(m.Modifications))
			fmt.Printf("  Explanation: %s\n", m.Explanation)
		case parser.RemoveFile:
			fmt.Printf("  Path: %s\n", m.Path)
			fmt.Printf("  Explanation: %s\n", m.Explanation)
		case parser.CreateFile:
			fmt.Printf("  Path: %s\n", m.Path)
			fmt.Printf("  Content length: %d\n", len(m.Content))
			fmt.Printf("  Explanation: %s\n", m.Explanation)
		case parser.RenameFile:
			fmt.Printf("  Old Path: %s\n", m.OldPath)
			fmt.Printf("  New Path: %s\n", m.NewPath)
			fmt.Printf("  Explanation: %s\n", m.Explanation)
		case parser.MoveFile:
			fmt.Printf("  Old Path: %s\n", m.OldPath)
			fmt.Printf("  New Path: %s\n", m.NewPath)
			fmt.Printf("  Explanation: %s\n", m.Explanation)
		case parser.CreateDirectory:
			fmt.Printf("  Path: %s\n", m.Path)
			fmt.Printf("  Explanation: %s\n", m.Explanation)
		}
		fmt.Println()
	}
	fmt.Println("--- End of Parsed Modifications ---")
}

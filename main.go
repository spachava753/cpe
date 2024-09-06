package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
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

func buildSystemMessage() (string, error) {
	var systemMessage strings.Builder
	systemMessage.WriteString(systemPromptTemplate)

	err := filepath.Walk(
		".", func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && (strings.HasSuffix(path, ".go") || path == "go.mod" || path == "go.sum") {
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
		},
	)

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

	// Read content from prompt.txt
	content, err := os.ReadFile("prompt.txt")
	if err != nil {
		fmt.Println("Error reading prompt.txt:", err)
		return
	}

	requestBody := RequestBody{
		Model:     "claude-3-5-sonnet-20240620",
		MaxTokens: 8192,
		Messages: []Message{
			{Role: "user", Content: string(content)},
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

	// Write full content to output.md
	err = os.WriteFile("output.md", []byte(fullContent.String()), 0644)
	if err != nil {
		fmt.Println("Error writing to output.md:", err)
		return
	}
	fmt.Println("Full content written to output.md")
}

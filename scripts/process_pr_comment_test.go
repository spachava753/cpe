package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseComment(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Output
	}{
		{
			name: "regular comment without header",
			input: "Regular comment without header",
			want: Output{
				Args:    "-continue last",
				Comment: "Regular comment without header",
			},
		},
		{
			name: "comment with header",
			input: "---\n-continue fdsF34\n---\nI want you to...",
			want: Output{
				Args:    "-continue fdsF34",
				Comment: "I want you to...",
			},
		},
		{
			name: "only header",
			input: "---\n-continue fdsF34\n---\n",
			want: Output{
				Args:    "-continue fdsF34",
				Comment: "",
			},
		},
		{
			name: "incomplete header",
			input: "---\n-continue fdsF34",
			want: Output{
				Args:    "-continue last",
				Comment: "---\n-continue fdsF34",
			},
		},
		{
			name: "empty header",
			input: "---\n\n---\nI want you to...",
			want: Output{
				Args:    "-continue last",
				Comment: "I want you to...",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseComment(tt.input)
			if got.Args != tt.want.Args || got.Comment != tt.want.Comment {
				t.Errorf("parseComment()\ngot  = %#v\nwant = %#v", got, tt.want)
			}
		})
	}
}

func TestExecuteCPE(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "cpe-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	outputFile := filepath.Join(tmpDir, "output.md")

	// Test with empty input
	err = executeCPE("-version", "", outputFile)
	if err != nil {
		t.Errorf("executeCPE() error = %v", err)
	}

	// Verify file exists and has content
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Errorf("Failed to read output file: %v", err)
	}
	if len(content) == 0 {
		t.Error("Output file is empty")
	}
}

func TestReadGitHubEvent(t *testing.T) {
	// Create a temporary file with test event data
	tmpFile, err := os.CreateTemp("", "github-event-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write test data
	testData := `{"comment":{"body":"Test comment"}}`
	if _, err := tmpFile.WriteString(testData); err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}
	tmpFile.Close()

	// Test reading the event
	payload, err := readGitHubEvent(tmpFile.Name())
	if err != nil {
		t.Errorf("readGitHubEvent() error = %v", err)
		return
	}

	if payload.Comment.Body != "Test comment" {
		t.Errorf("readGitHubEvent() got = %v, want %v", payload.Comment.Body, "Test comment")
	}
}
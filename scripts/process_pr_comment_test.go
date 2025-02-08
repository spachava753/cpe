package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestParseComment(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)

	// Change to temp directory for relative path operations
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create .cpeconvo in some tests to simulate existing conversation
	tests := []struct {
		name           string
		input         string
		createCpeConvo bool
		want          Output
	}{
		{
			name: "regular comment without header (first conversation)",
			input: "Regular comment without header",
			createCpeConvo: false,
			want: Output{
				Args:    "",
				Comment: "Regular comment without header",
			},
		},
		{
			name: "regular comment without header (existing conversation)",
			input: "Regular comment without header",
			createCpeConvo: true,
			want: Output{
				Args:    "", // No args needed since continue is default behavior
				Comment: "Regular comment without header",
			},
		},
		{
			name: "comment with header (first conversation)",
			input: "---\n-continue fdsF34\n---\nI want you to...",
			createCpeConvo: false,
			want: Output{
				Args:    "-continue fdsF34",
				Comment: "I want you to...",
			},
		},
		{
			name: "comment with header (existing conversation)",
			input: "---\n-continue fdsF34\n---\nI want you to...",
			createCpeConvo: true,
			want: Output{
				Args:    "-continue fdsF34",
				Comment: "I want you to...",
			},
		},
		{
			name: "only header",
			input: "---\n-continue fdsF34\n---\n",
			createCpeConvo: true,
			want: Output{
				Args:    "-continue fdsF34",
				Comment: "",
			},
		},
		{
			name: "incomplete header (first conversation)",
			input: "---\n-continue fdsF34",
			createCpeConvo: false,
			want: Output{
				Args:    "",
				Comment: "---\n-continue fdsF34",
			},
		},
		{
			name: "incomplete header (existing conversation)",
			input: "---\n-continue fdsF34",
			createCpeConvo: true,
			want: Output{
				Args:    "", // No args needed since continue is default behavior
				Comment: "---\n-continue fdsF34",
			},
		},
		{
			name: "empty header (first conversation)",
			input: "---\n\n---\nI want you to...",
			createCpeConvo: false,
			want: Output{
				Args:    "",
				Comment: "I want you to...",
			},
		},
		{
			name: "empty header (existing conversation)",
			input: "---\n\n---\nI want you to...",
			createCpeConvo: true,
			want: Output{
				Args:    "", // No args needed since continue is default behavior
				Comment: "I want you to...",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Remove any existing .cpeconvo
			os.Remove(".cpeconvo")

			// Create .cpeconvo if test requires it
			if tt.createCpeConvo {
				if err := os.WriteFile(".cpeconvo", []byte("test"), 0644); err != nil {
					t.Fatal(err)
				}
			}

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

	// Capture stdout and stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	// Create pipes for stdout and stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	// Set stdout and stderr to our pipes
	os.Stdout = wOut
	os.Stderr = wErr

	outputFile := filepath.Join(tmpDir, "output.md")

	// Run command in a goroutine
	go func() {
		err := executeCPE("-version", "", outputFile)
		if err != nil {
			t.Errorf("executeCPE() error = %v", err)
		}
		wOut.Close()
		wErr.Close()
	}()

	// Read stdout and stderr
	outC := make(chan string)
	errC := make(chan string)
	go func() {
		var b bytes.Buffer
		io.Copy(&b, rOut)
		outC <- b.String()
	}()
	go func() {
		var b bytes.Buffer
		io.Copy(&b, rErr)
		errC <- b.String()
	}()

	// Get the output
	stdout := <-outC
	stderr := <-errC

	// Verify stdout/stderr were captured
	if stdout == "" && stderr == "" {
		t.Error("No output captured from command")
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
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/cpe/internal/urlhandler"
)

// TestProcessUserInputWithURL tests the integration of URL handling in processUserInput
func TestProcessUserInputWithURL(t *testing.T) {
	// This is a unit test of URL functionality separate from the security restrictions
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Hello from test server!")
	}))
	defer server.Close()

	// Test the URL handler directly with test configuration
	config := &urlhandler.DownloadConfig{
		Timeout:       30 * time.Second,
		MaxSize:       50 * 1024 * 1024,
		UserAgent:     "CPE-Test/1.0",
		RetryAttempts: 3,
		Client:        &http.Client{Timeout: 30 * time.Second},
	}

	ctx := context.Background()
	result, err := urlhandler.DownloadContent(ctx, server.URL, config)
	if err != nil {
		t.Fatalf("DownloadContent failed: %v", err)
	}

	if !strings.Contains(string(result.Data), "Hello from test server!") {
		t.Errorf("Expected content to contain 'Hello from test server!', got: %s", string(result.Data))
	}

	if result.ContentType != "text/plain" {
		t.Errorf("Expected content type 'text/plain', got: %s", result.ContentType)
	}
}

// TestProcessUserInputWithMixedInputs tests URL detection logic
func TestProcessUserInputWithMixedInputs(t *testing.T) {
	// Test URL detection
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"HTTP URL", "http://example.com/file.txt", true},
		{"HTTPS URL", "https://github.com/user/repo/blob/main/README.md", true},
		{"Local file", "./local-file.txt", false},
		{"Absolute path", "/home/user/file.txt", false},
		{"Relative path", "../file.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := urlhandler.IsURL(tt.input)
			if result != tt.expected {
				t.Errorf("IsURL(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

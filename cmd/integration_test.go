package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bradleyjkemp/cupaloy/v2"

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

	// Use cupaloy to snapshot the content and content type
	// We snapshot a struct with deterministic fields (excluding URL which has dynamic port)
	cupaloy.SnapshotT(t, struct {
		Data        string
		ContentType string
	}{
		Data:        string(result.Data),
		ContentType: result.ContentType,
	})
}

// TestProcessUserInputWithMixedInputs tests URL detection logic
func TestProcessUserInputWithMixedInputs(t *testing.T) {
	// Test URL detection
	tests := []struct {
		name  string
		input string
	}{
		{"HTTP URL", "http://example.com/file.txt"},
		{"HTTPS URL", "https://github.com/user/repo/blob/main/README.md"},
		{"Local file", "./local-file.txt"},
		{"Absolute path", "/home/user/file.txt"},
		{"Relative path", "../file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := urlhandler.IsURL(tt.input)
			cupaloy.SnapshotT(t, result)
		})
	}
}

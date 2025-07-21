package urlhandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testConfig returns a configuration suitable for testing with localhost access
func testConfig() *DownloadConfig {
	return &DownloadConfig{
		Timeout:       DefaultTimeout,
		MaxSize:       MaxContentSize,
		UserAgent:     DefaultUserAgent,
		RetryAttempts: 3,
		Client:        &http.Client{Timeout: DefaultTimeout},
	}
}

func TestIsURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "valid HTTP URL",
			input: "http://example.com/file.txt",
			want:  true,
		},
		{
			name:  "valid HTTPS URL",
			input: "https://example.com/file.txt",
			want:  true,
		},
		{
			name:  "URL with path and query",
			input: "https://api.github.com/repos/user/repo/contents/README.md?ref=main",
			want:  true,
		},
		{
			name:  "local file path",
			input: "./local/file.txt",
			want:  false,
		},
		{
			name:  "absolute file path",
			input: "/home/user/file.txt",
			want:  false,
		},
		{
			name:  "file URL",
			input: "file:///home/user/file.txt",
			want:  false,
		},
		{
			name:  "FTP URL",
			input: "ftp://ftp.example.com/file.txt",
			want:  false,
		},
		{
			name:  "malformed URL",
			input: "not-a-url",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsURL(tt.input); got != tt.want {
				t.Errorf("IsURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDownloadContent(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		config         *DownloadConfig
		wantErr        bool
		errMsg         string
		expectContent  string
		expectMimeType string
	}{
		{
			name: "successful download",
			serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Hello, World!"))
			}),
			config:         testConfig(),
			wantErr:        false,
			expectContent:  "Hello, World!",
			expectMimeType: "text/plain",
		},
		{
			name: "404 not found",
			serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}),
			config:  testConfig(),
			wantErr: true,
			errMsg:  "HTTP 404",
		},
		{
			name: "content too large (via content-length)",
			serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "100000000") // 100MB
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("small content"))
			}),
			config:  testConfig(),
			wantErr: true,
			errMsg:  "exceeds maximum limit",
		},
		{
			name: "server timeout",
			serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(100 * time.Millisecond) // Short delay for testing
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("delayed response"))
			}),
			config: &DownloadConfig{
				Timeout:       10 * time.Millisecond, // Very short timeout for testing
				MaxSize:       MaxContentSize,
				UserAgent:     DefaultUserAgent,
				RetryAttempts: 1,
				Client: &http.Client{
					Timeout: 10 * time.Millisecond,
				},
			},
			wantErr: true,
			errMsg:  "context deadline exceeded",
		},
		{
			name: "successful JSON download",
			serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"message": "hello"}`))
			}),
			config:         testConfig(),
			wantErr:        false,
			expectContent:  `{"message": "hello"}`,
			expectMimeType: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			ctx := context.Background()
			result, err := DownloadContent(ctx, server.URL, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("DownloadContent() expected error containing %q, got nil", tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("DownloadContent() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("DownloadContent() unexpected error = %v", err)
					return
				}

				if result == nil {
					t.Errorf("DownloadContent() returned nil result")
					return
				}

				if string(result.Data) != tt.expectContent {
					t.Errorf("DownloadContent() content = %q, want %q", string(result.Data), tt.expectContent)
				}

				if result.ContentType != tt.expectMimeType {
					t.Errorf("DownloadContent() content type = %q, want %q", result.ContentType, tt.expectMimeType)
				}

				if result.URL != server.URL {
					t.Errorf("DownloadContent() URL = %q, want %q", result.URL, server.URL)
				}

				if result.Size != int64(len(tt.expectContent)) {
					t.Errorf("DownloadContent() size = %d, want %d", result.Size, len(tt.expectContent))
				}
			}
		})
	}
}

func TestDownloadContentWithContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("delayed response"))
	}))
	defer server.Close()

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	config := testConfig()
	config.Client.Timeout = time.Second // Client timeout longer than context timeout

	_, err := DownloadContent(ctx, server.URL, config)
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Errorf("DownloadContent() expected context cancellation error, got %v", err)
	}
}

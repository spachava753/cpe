package urlhandler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "valid https URL",
			input: "https://example.com",
			want:  true,
		},
		{
			name:  "valid http URL",
			input: "http://example.com",
			want:  true,
		},
		{
			name:  "https URL with path",
			input: "https://example.com/path/to/resource",
			want:  true,
		},
		{
			name:  "no scheme",
			input: "example.com",
			want:  false,
		},
		{
			name:  "ftp scheme",
			input: "ftp://example.com",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "just http prefix no host",
			input: "http://",
			want:  false,
		},
		{
			name:  "just https prefix no host",
			input: "https://",
			want:  false,
		},
		{
			name:  "relative path",
			input: "/path/to/file",
			want:  false,
		},
		{
			name:  "file path",
			input: "file:///tmp/test",
			want:  false,
		},
		{
			name:  "valid URL with port",
			input: "https://example.com:8080/api",
			want:  true,
		},
		{
			name:  "valid URL with query",
			input: "https://example.com/search?q=test",
			want:  true,
		},
		{
			name:  "http URL with control character causes parse error",
			input: "http://\x00invalid",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsURL(tt.input)
			if got != tt.want {
				t.Errorf("IsURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", config.Timeout, DefaultTimeout)
	}
	if config.MaxSize != MaxContentSize {
		t.Errorf("MaxSize = %d, want %d", config.MaxSize, MaxContentSize)
	}
	if config.UserAgent != DefaultUserAgent {
		t.Errorf("UserAgent = %q, want %q", config.UserAgent, DefaultUserAgent)
	}
	if config.RetryAttempts != 3 {
		t.Errorf("RetryAttempts = %d, want 3", config.RetryAttempts)
	}
	if config.Client == nil {
		t.Error("Client should not be nil")
	}
}

func TestDownloadContent(t *testing.T) {
	t.Run("successful download", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "hello world")
		}))
		defer server.Close()

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       1024,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client:        server.Client(),
		}

		result, err := DownloadContent(context.Background(), server.URL, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if string(result.Data) != "hello world" {
			t.Errorf("Data = %q, want %q", string(result.Data), "hello world")
		}
		if result.ContentType != "text/plain" {
			t.Errorf("ContentType = %q, want %q", result.ContentType, "text/plain")
		}
		if result.URL != server.URL {
			t.Errorf("URL = %q, want %q", result.URL, server.URL)
		}
		if result.Size != 11 {
			t.Errorf("Size = %d, want 11", result.Size)
		}
	})

	t.Run("nil config uses default", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("User-Agent") != DefaultUserAgent {
				t.Errorf("User-Agent = %q, want %q", r.Header.Get("User-Agent"), DefaultUserAgent)
			}
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "ok")
		}))
		defer server.Close()

		result, err := DownloadContent(context.Background(), server.URL, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if string(result.Data) != "ok" {
			t.Errorf("Data = %q, want %q", string(result.Data), "ok")
		}
	})

	t.Run("non-2xx status code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       1024,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client:        server.Client(),
		}

		_, err := DownloadContent(context.Background(), server.URL, config)
		if err == nil {
			t.Fatal("expected error for 404 status")
		}
		if !strings.Contains(err.Error(), "HTTP 404") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "HTTP 404")
		}
	})

	t.Run("content-length exceeds max size", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "999999999")
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "data")
		}))
		defer server.Close()

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       100,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client:        server.Client(),
		}

		_, err := DownloadContent(context.Background(), server.URL, config)
		if err == nil {
			t.Fatal("expected error for oversized content")
		}
		if !strings.Contains(err.Error(), "exceeds maximum limit") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "exceeds maximum limit")
		}
	})

	t.Run("actual data exceeds max size via limited reader", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			// Use Flusher to force chunked transfer encoding, avoiding Content-Length header
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("expected http.Flusher")
			}
			// Write data in chunks so Content-Length is not set
			for i := 0; i < 20; i++ {
				w.Write([]byte(strings.Repeat("x", 10)))
				flusher.Flush()
			}
		}))
		defer server.Close()

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       100,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client:        server.Client(),
		}

		_, err := DownloadContent(context.Background(), server.URL, config)
		if err == nil {
			t.Fatal("expected error for data exceeding max size")
		}
		if !strings.Contains(err.Error(), "exceeds maximum limit") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "exceeds maximum limit")
		}
	})

	t.Run("empty content type defaults to octet-stream", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Explicitly set empty Content-Type; we must set it after WriteHeader
			// to override Go's auto-detection. Use Hijack or set header explicitly.
			w.Header()["Content-Type"] = nil
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("binary data"))
		}))
		defer server.Close()

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       1024,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client:        server.Client(),
		}

		result, err := DownloadContent(context.Background(), server.URL, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.ContentType != "application/octet-stream" {
			t.Errorf("ContentType = %q, want %q", result.ContentType, "application/octet-stream")
		}
	})

	t.Run("invalid URL causes request creation error", func(t *testing.T) {
		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       1024,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client:        http.DefaultClient,
		}

		_, err := DownloadContent(context.Background(), "://invalid-url", config)
		if err == nil {
			t.Fatal("expected error for invalid URL")
		}
	})

	t.Run("request failure due to unreachable server", func(t *testing.T) {
		config := &DownloadConfig{
			Timeout:       2 * time.Second,
			MaxSize:       1024,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client: &http.Client{
				Timeout: 1 * time.Second,
			},
		}

		_, err := DownloadContent(context.Background(), "http://192.0.2.1:1", config)
		if err == nil {
			t.Fatal("expected error for unreachable server")
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(5 * time.Second)
			fmt.Fprint(w, "delayed")
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       1024,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client:        server.Client(),
		}

		_, err := DownloadContent(ctx, server.URL, config)
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})

	t.Run("server error status 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       1024,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client:        server.Client(),
		}

		_, err := DownloadContent(context.Background(), server.URL, config)
		if err == nil {
			t.Fatal("expected error for 500 status")
		}
		if !strings.Contains(err.Error(), "HTTP 500") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "HTTP 500")
		}
	})

	t.Run("redirect status 301 is non-success without follow", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusMovedPermanently)
		}))
		defer server.Close()

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       1024,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client: &http.Client{
				// Disable redirect following so we actually see the 301
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			},
		}

		_, err := DownloadContent(context.Background(), server.URL, config)
		if err == nil {
			t.Fatal("expected error for 301 status")
		}
		if !strings.Contains(err.Error(), "HTTP 301") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "HTTP 301")
		}
	})

	t.Run("read body error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "1000") // Claim 1000 bytes
			w.WriteHeader(http.StatusOK)
			// Write only partial data then close, causing a read error
			w.Write([]byte("partial"))
			// Hijack the connection to force-close it
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
			}
		}))
		defer server.Close()

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       2000,
			UserAgent:     "test-agent",
			RetryAttempts: 1,
			Client:        server.Client(),
		}

		_, err := DownloadContent(context.Background(), server.URL, config)
		if err == nil {
			t.Fatal("expected error for truncated body")
		}
	})

	t.Run("user agent header is set", func(t *testing.T) {
		customUA := "CustomAgent/2.0"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("User-Agent") != customUA {
				t.Errorf("User-Agent = %q, want %q", r.Header.Get("User-Agent"), customUA)
			}
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "ok")
		}))
		defer server.Close()

		config := &DownloadConfig{
			Timeout:       5 * time.Second,
			MaxSize:       1024,
			UserAgent:     customUA,
			RetryAttempts: 1,
			Client:        server.Client(),
		}

		_, err := DownloadContent(context.Background(), server.URL, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

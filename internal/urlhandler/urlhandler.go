package urlhandler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/httpclient"
)

const (
	// MaxContentSize defines the maximum allowed content size (50MB)
	MaxContentSize = 50 * 1024 * 1024
	// DefaultTimeout for HTTP requests
	DefaultTimeout = 30 * time.Second
	// DefaultUserAgent for HTTP requests
	DefaultUserAgent = "CPE/1.0 (Chat-based Programming Editor)"
)

// DownloadConfig holds configuration for downloading content from URLs
type DownloadConfig struct {
	Timeout       time.Duration
	MaxSize       int64
	UserAgent     string
	RetryAttempts int
	Client        *http.Client
}

// DefaultConfig returns a default download configuration
func DefaultConfig() *DownloadConfig {
	return &DownloadConfig{
		Timeout:       DefaultTimeout,
		MaxSize:       MaxContentSize,
		UserAgent:     DefaultUserAgent,
		RetryAttempts: 3,
		Client: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

func downloadHTTPClient(config *DownloadConfig) *http.Client {
	return httpclient.New(
		httpclient.WithBaseClient(config.Client),
		httpclient.WithBackoff(200*time.Millisecond, 3*time.Second),
		httpclient.WithJitterFactor(0.2),
		httpclient.WithMaxRetries(config.RetryAttempts-1),
		httpclient.WithDefaultTimeout(config.Timeout),
	)
}

// DownloadedContent represents content downloaded from a URL
type DownloadedContent struct {
	Data        []byte
	ContentType string
	URL         string
	Size        int64
}

// IsURL checks if the given string is a valid HTTP or HTTPS URL
func IsURL(input string) bool {
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		return false
	}

	parsed, err := url.Parse(input)
	if err != nil {
		return false
	}

	// Basic validation
	return parsed.Scheme != "" && parsed.Host != ""
}

// DownloadContent downloads content from a URL with size limits. It retries
// request failures and retryable HTTP statuses; errors while reading a
// successful response body are returned immediately and are not retried.
func DownloadContent(ctx context.Context, urlStr string, config *DownloadConfig) (*DownloadedContent, error) {
	if config == nil {
		config = DefaultConfig()
	}

	result, err := download(ctx, urlStr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to download content: %w", err)
	}
	return result, nil
}

// download performs a single download attempt
func download(ctx context.Context, urlStr string, config *DownloadConfig) (*DownloadedContent, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent
	req.Header.Set("User-Agent", config.UserAgent)

	// Make the request
	resp, err := downloadHTTPClient(config).Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Check content length if available
	if resp.ContentLength > 0 && resp.ContentLength > config.MaxSize {
		return nil, fmt.Errorf("content size (%d bytes) exceeds maximum limit (%d bytes)", resp.ContentLength, config.MaxSize)
	}

	// Create a limited reader to prevent reading too much data
	limitedReader := io.LimitReader(resp.Body, config.MaxSize+1)

	// Read the response body
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check if we exceeded the size limit
	if len(data) > int(config.MaxSize) {
		return nil, fmt.Errorf("content size exceeds maximum limit (%d bytes)", config.MaxSize)
	}

	// Get content type from response headers
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return &DownloadedContent{
		Data:        data,
		ContentType: contentType,
		URL:         urlStr,
		Size:        int64(len(data)),
	}, nil
}

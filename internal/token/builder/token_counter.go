package builder

import (
	"context"
	"fmt"
	"os"

	"github.com/spachava753/gai"
)

// CountStdin counts tokens in content from stdin
func CountStdin(ctx context.Context, content []byte, tc gai.TokenCounter) (uint, error) {
	// Create dialog from content
	dialog, err := BuildDialog(content)
	if err != nil {
		return 0, fmt.Errorf("failed to build dialog from stdin: %w", err)
	}

	// Count tokens
	count, err := tc.Count(ctx, dialog)
	if err != nil {
		return 0, fmt.Errorf("failed to count tokens from stdin: %w", err)
	}

	return count, nil
}

// CountFile counts tokens in a specific file
func CountFile(ctx context.Context, path string, tc gai.TokenCounter) (uint, error) {
	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	// Create dialog with appropriate modality from content
	dialog, err := BuildDialog(content)
	if err != nil {
		return 0, fmt.Errorf("failed to process file %s: %w", path, err)
	}

	// Count tokens
	count, err := tc.Count(ctx, dialog)
	if err != nil {
		return 0, fmt.Errorf("failed to count tokens in file %s: %w", path, err)
	}

	return count, nil
}

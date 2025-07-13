package tree

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/spachava753/cpe/internal/token/builder"
	"github.com/spachava753/gai"
	"golang.org/x/sync/errgroup"
)

// Default constants for token counting
const (
	// Default maximum file size for token counting (10MB)
	DefaultMaxFileSize = 10 * 1024 * 1024

	// Default maximum parallel file processing
	DefaultMaxConcurrentFiles = 10
)

// Runtime values that can be configured via flags
var (
	MaxFileSize        = DefaultMaxFileSize
	MaxConcurrentFiles = DefaultMaxConcurrentFiles
)

func countFilesParallel(
	ctx context.Context,
	files []string,
	tc gai.TokenCounter,
	showFileSkip bool,
	progressWriter io.Writer,
) (map[string]uint, error) {
	var (
		mu           sync.Mutex
		counts       = make(map[string]uint)
		eg, egCtx    = errgroup.WithContext(ctx)
		processedCnt uint64 // for progress reporting
		totalFiles   = uint64(len(files))
		lastPercent  uint64 // Track last reported 10% boundary
	)

	// Limit concurrency to the configurable value
	eg.SetLimit(MaxConcurrentFiles)

	// Process each file concurrently
	for _, file := range files {
		filePath := file // Create a local copy for the goroutine
		eg.Go(func() error {
			// Check for context cancellation first
			if egCtx.Err() != nil {
				return egCtx.Err()
			}

			// Get file info for size check
			info, err := os.Stat(filePath)
			if err != nil {
				// Skip files we can't stat
				return nil
			}

			// Skip large files using the configurable size limit
			if info.Size() > int64(MaxFileSize) {
				if showFileSkip {
					fmt.Fprintf(progressWriter, "Skipping large file: %s (%.1f MB)\n", filePath, float64(info.Size())/1024/1024)
				}
				return nil
			}

			// Read file content
			content, err := os.ReadFile(filePath)
			if err != nil {
				// Skip unreadable files
				return nil
			}

			// Build dialog with appropriate modality based on content type
			dialog, err := builder.BuildDialog(content) // Use builder package
			if err != nil {
				// Skip files with unsupported content types
				if showFileSkip {
					fmt.Fprintf(progressWriter, "Skipping unsupported file: %s (%v)\n", filePath, err)
				}
				return nil
			}

			// Count tokens
			count, err := tc.Count(egCtx, dialog)
			if err != nil {
				if showFileSkip {
					fmt.Fprintf(progressWriter, "Error counting tokens for file %s: %v\n", filePath, err)
				}
				return nil
			}

			// Update progress
			processed := atomic.AddUint64(&processedCnt, 1)

			// Report progress based on the specified criteria:
			// - Every 10% increment OR
			// - Every 100 files
			// Whichever comes first
			currentPercentVal := processed * 100 / totalFiles

			// Atomically load and compare lastPercent
			// This ensures that we only print when the 10% boundary is crossed
			// and avoids race conditions on lastPercent
			oldLastPercent := atomic.LoadUint64(&lastPercent)
			if processed%100 == 0 || (currentPercentVal/10 > oldLastPercent/10) {
				// Attempt to atomically update lastPercent only if it hasn't changed
				if atomic.CompareAndSwapUint64(&lastPercent, oldLastPercent, currentPercentVal) {
					fmt.Fprintf(progressWriter, "Counting tokens: %d/%d files processed (%.1f%%)\n",
						processed, totalFiles, float64(processed)/float64(totalFiles)*100)
				}
			}

			// Store the result
			mu.Lock()
			counts[filePath] = count
			mu.Unlock()

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return counts, nil
}

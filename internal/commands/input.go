package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spachava753/gai"

	inputpkg "github.com/spachava753/cpe/internal/input"
)

// ProcessUserInputOptions contains parameters for processing user input
type ProcessUserInputOptions struct {
	// Args are command line arguments (prompt)
	Args []string
	// InputPaths are file paths or URLs to include
	InputPaths []string
	// Stdin is the stdin reader (can be nil to skip stdin)
	Stdin io.Reader
	// SkipStdin disables reading from stdin even if data is available
	SkipStdin bool
}

// ProcessUserInput processes and combines user input from all available sources
func ProcessUserInput(ctx context.Context, opts ProcessUserInputOptions) ([]gai.Block, error) {
	var userBlocks []gai.Block

	// Get input from stdin if available and not skipped
	if opts.Stdin != nil && !opts.SkipStdin {
		shouldReadStdin := true
		if f, ok := opts.Stdin.(*os.File); ok {
			stdinStat, err := f.Stat()
			if err != nil {
				return nil, fmt.Errorf("failed to check stdin: %w", err)
			}
			// Interactive terminals should not be drained implicitly.
			shouldReadStdin = (stdinStat.Mode() & os.ModeCharDevice) == 0
		}

		if shouldReadStdin {
			stdinBytes, err := io.ReadAll(opts.Stdin)
			if err != nil {
				return nil, fmt.Errorf("failed to read from stdin: %w", err)
			}
			if len(stdinBytes) > 0 {
				userBlocks = append(userBlocks, gai.Block{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(stdinBytes),
				})
			}
		}
	}

	// Extract prompt from positional arguments
	var prompt string
	if len(opts.Args) > 0 {
		if len(opts.Args) != 1 {
			return nil, fmt.Errorf("too many arguments to process")
		}
		prompt = opts.Args[0]
	}

	// Build blocks from prompt and resource paths (files/URLs)
	blocks, err := inputpkg.BuildUserBlocks(ctx, prompt, opts.InputPaths)
	if err != nil {
		return nil, err
	}

	userBlocks = append(userBlocks, blocks...)
	return userBlocks, nil
}

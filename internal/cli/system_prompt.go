package cli

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spf13/afero"
)

//go:embed system_prompt.md
var defaultSystemPrompt string

//go:embed compaction_prompt.md
var defaultCompactionPrompt string

func loadSystemPrompt(filesystem afero.Fs, directory string) (string, error) {
	prompt := defaultSystemPrompt
	path := filepath.Join(
		directory,
		"SYSTEM.md",
	)
	content, err := afero.ReadFile(
		filesystem,
		path,
	)
	switch {
	case err == nil:
		prompt = string(content)
	case !errors.Is(
		err,
		fs.ErrNotExist,
	):
		return "", fmt.Errorf(
			"read system prompt %q: %w",
			path,
			err,
		)
	}
	path = filepath.Join(
		directory,
		"APPEND-SYSTEM.md",
	)
	content, err = afero.ReadFile(
		filesystem,
		path,
	)
	if err != nil && !errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return "", fmt.Errorf(
			"read system prompt %q: %w",
			path,
			err,
		)
	}
	if len(content) != 0 {
		if prompt != "" {
			prompt += "\n\n"
		}
		prompt += string(content)
	}
	compaction := defaultCompactionPrompt
	path = filepath.Join(
		directory,
		"COMPACTION.md",
	)
	content, err = afero.ReadFile(
		filesystem,
		path,
	)
	switch {
	case err == nil:
		compaction = string(content)
	case !errors.Is(
		err,
		fs.ErrNotExist,
	):
		return "", fmt.Errorf(
			"read compaction instructions %q: %w",
			path,
			err,
		)
	}
	if compaction != "" {
		if prompt != "" {
			prompt += "\n\n"
		}
		prompt += compaction
	}
	return prompt, nil
}

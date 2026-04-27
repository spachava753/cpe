// Package textedit implements CPE's bundled file creation and exact-replace behavior.
package textedit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ToolName is the MCP tool name exposed by the bundled text editor server.
const ToolName = "text_edit"

// Input is the JSON payload accepted by the text_edit tool.
type Input struct {
	Path    string `json:"path" jsonschema:"Path to the file to edit or create"`
	OldText string `json:"old_text,omitempty" jsonschema:"Exact text to find and replace. If empty, creates a new file instead"`
	Text    string `json:"text" jsonschema:"Replacement text or content for new file"`
}

// Output is the structured result returned by text_edit.
type Output struct {
	Path         string `json:"path" jsonschema:"Resolved path that was edited or created"`
	Operation    string `json:"operation" jsonschema:"Operation performed: created or modified"`
	Replacements int    `json:"replacements,omitempty" jsonschema:"Number of replacements performed"`
}

// Message returns a short human-readable summary for tool result content.
func (o Output) Message() string {
	switch o.Operation {
	case "created":
		return fmt.Sprintf("created %s", o.Path)
	case "modified":
		return fmt.Sprintf("modified %s (%d replacement)", o.Path, o.Replacements)
	default:
		return fmt.Sprintf("updated %s", o.Path)
	}
}

// Apply performs the text_edit operation relative to the current working directory.
func Apply(input Input) (Output, error) {
	if strings.TrimSpace(input.Path) == "" {
		return Output{}, fmt.Errorf("path is required")
	}

	resolvedPath, err := filepath.Abs(input.Path)
	if err != nil {
		return Output{}, fmt.Errorf("resolving path: %w", err)
	}

	if input.OldText == "" {
		return createFile(resolvedPath, input.Text)
	}
	return replaceText(resolvedPath, input.OldText, input.Text)
}

func createFile(path, text string) (Output, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Output{}, fmt.Errorf("creating parent directories: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return Output{}, fmt.Errorf("file already exists: %s", path)
		}
		return Output{}, fmt.Errorf("creating file: %w", err)
	}

	if _, err := file.WriteString(text); err != nil {
		_ = file.Close()
		return Output{}, fmt.Errorf("writing file: %w", err)
	}
	if err := file.Close(); err != nil {
		return Output{}, fmt.Errorf("closing file: %w", err)
	}
	return Output{Path: path, Operation: "created"}, nil
}

func replaceText(path, oldText, newText string) (Output, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Output{}, fmt.Errorf("file does not exist: %s", path)
		}
		return Output{}, fmt.Errorf("stat file: %w", err)
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		return Output{}, fmt.Errorf("path is a symlink: %s", path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return Output{}, fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return Output{}, fmt.Errorf("path is a directory: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Output{}, fmt.Errorf("reading file: %w", err)
	}
	if !utf8.Valid(data) {
		return Output{}, fmt.Errorf("file is not valid UTF-8: %s", path)
	}

	content := string(data)
	count := countOverlappingOccurrences(content, oldText)
	switch count {
	case 0:
		return Output{}, fmt.Errorf("old_text not found in %s", path)
	case 1:
		// proceed
	default:
		return Output{}, fmt.Errorf("old_text appears %d times in %s; expected exactly one match", count, path)
	}

	updated := strings.Replace(content, oldText, newText, 1)
	if err := writeFileAtomically(path, []byte(updated), info.Mode().Perm()); err != nil {
		return Output{}, err
	}
	return Output{Path: path, Operation: "modified", Replacements: 1}, nil
}

func countOverlappingOccurrences(content, needle string) int {
	count := 0
	for start := 0; start < len(content); {
		idx := strings.Index(content[start:], needle)
		if idx == -1 {
			break
		}
		count++
		if count > 1 {
			return count
		}
		start += idx + 1
	}
	return count
}

func writeFileAtomically(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".text-edit-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting temp file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing file: %w", err)
	}
	return nil
}

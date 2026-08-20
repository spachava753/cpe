// Package textedit implements CPE's bundled file creation and exact-replace behavior.
package textedit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// toolName is the MCP tool name exposed by the bundled text editor server.
const toolName = "text_edit"

// input is the JSON payload accepted by the text_edit tool.
type input struct {
	Path    string `json:"path" jsonschema:"Path to the file to edit or create"`
	OldText string `json:"old_text,omitempty" jsonschema:"Exact text to find and replace. If empty, creates a new file instead"`
	NewText string `json:"new_text" jsonschema:"Replacement text or content for new file"`
}

// output is the structured result returned by text_edit.
type output struct {
	Path         string `json:"path" jsonschema:"Resolved path that was edited or created"`
	Operation    string `json:"operation" jsonschema:"Operation performed: created or modified"`
	Replacements int    `json:"replacements,omitempty" jsonschema:"Number of replacements performed"`
}

// Message returns a short human-readable summary for tool result content.
func (o output) Message() string {
	switch o.Operation {
	case "created":
		return fmt.Sprintf("created %s", o.Path)
	case "modified":
		return fmt.Sprintf("modified %s (%d replacement)", o.Path, o.Replacements)
	default:
		return fmt.Sprintf("updated %s", o.Path)
	}
}

// apply performs the text_edit operation relative to the current working directory.
func apply(input input) (output, error) {
	if strings.TrimSpace(input.Path) == "" {
		return output{}, fmt.Errorf("path is required")
	}

	resolvedPath, err := filepath.Abs(input.Path)
	if err != nil {
		return output{}, fmt.Errorf("resolving path: %w", err)
	}

	if input.OldText == "" {
		return createFile(resolvedPath, input.NewText)
	}
	return replaceText(resolvedPath, input.OldText, input.NewText)
}

func createFile(path, text string) (output, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return output{}, fmt.Errorf("creating parent directories: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return output{}, fmt.Errorf("file already exists: %s", path)
		}
		return output{}, fmt.Errorf("creating file: %w", err)
	}

	if _, err := file.WriteString(text); err != nil {
		_ = file.Close()
		return output{}, fmt.Errorf("writing file: %w", err)
	}
	if err := file.Close(); err != nil {
		return output{}, fmt.Errorf("closing file: %w", err)
	}
	return output{Path: path, Operation: "created"}, nil
}

func replaceText(path, oldText, newText string) (output, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return output{}, fmt.Errorf("file does not exist: %s", path)
		}
		return output{}, fmt.Errorf("stat file: %w", err)
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		return output{}, fmt.Errorf("path is a symlink: %s", path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return output{}, fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return output{}, fmt.Errorf("path is a directory: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return output{}, fmt.Errorf("reading file: %w", err)
	}
	if !utf8.Valid(data) {
		return output{}, fmt.Errorf("file is not valid UTF-8: %s", path)
	}

	content := string(data)
	count := countOverlappingOccurrences(content, oldText)
	switch count {
	case 0:
		return output{}, fmt.Errorf("old_text not found in %s", path)
	case 1:
		// proceed
	default:
		return output{}, fmt.Errorf("old_text appears %d times in %s; expected exactly one match", count, path)
	}

	updated := strings.Replace(content, oldText, newText, 1)
	if err := writeFileAtomically(path, []byte(updated), info.Mode().Perm()); err != nil {
		return output{}, err
	}
	return output{Path: path, Operation: "modified", Replacements: 1}, nil
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

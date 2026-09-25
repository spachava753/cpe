package skills

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// CommandPrefix identifies explicit skill invocations in either CLI mode.
const CommandPrefix = "/skill:"

const maxFrontmatter = 64 * 1024

type invocation uint8

const (
	byUser invocation = 1 << iota
	byModel
)

type skill struct {
	name, description, path string
	invocable               invocation
}

// Catalog holds parsed skills. Its zero value is an empty catalog.
type Catalog struct{ entries []skill }

// command is user-facing metadata for a complete /skill:NAME command.
type command struct{ Name, Description string }

// Discover scans roots in precedence order, returning usable skills and warnings.
// The caller chooses scopes; no ambient home or working directory is consulted.
func Discover(roots ...string) (Catalog, []error) {
	selected := make(map[string]skill)
	var warnings []error
	for _, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil {
			warnings = append(warnings, err)
			continue
		}
		children, err := os.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			warnings = append(warnings, err)
			continue
		}
		for _, child := range children {
			if !child.IsDir() && child.Type()&os.ModeSymlink == 0 {
				continue
			}
			path := filepath.Join(root, child.Name(), "SKILL.md")
			info, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if _, exists := selected[child.Name()]; exists {
				warnings = append(warnings, fmt.Errorf("%s overrides an earlier skill named %q", path, child.Name()))
			}
			// An invalid project override must not reveal a global skill whose
			// invocation policy may be less restrictive.
			delete(selected, child.Name())
			if err != nil {
				warnings = append(warnings, err)
				continue
			}
			if !info.Mode().IsRegular() {
				warnings = append(warnings, fmt.Errorf("%s: expected a regular file", path))
				continue
			}
			entry, err := load(path)
			if err != nil {
				warnings = append(warnings, fmt.Errorf("%s: %w", path, err))
				continue
			}
			selected[entry.name] = entry
		}
	}
	catalog := Catalog{entries: make([]skill, 0, len(selected))}
	for _, entry := range selected {
		catalog.entries = append(catalog.entries, entry)
	}
	slices.SortFunc(catalog.entries, func(a, b skill) int { return strings.Compare(a.name, b.name) })
	return catalog, warnings
}

func load(path string) (skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return skill{}, err
	}
	defer f.Close()
	return parse(f, path)
}

func parse(r io.Reader, path string) (skill, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, maxFrontmatter+1))
	scanner.Buffer(make([]byte, 4096), maxFrontmatter+1)
	consumed := 0
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		advance, line, err := bufio.ScanLines(data, atEOF)
		consumed += advance
		if consumed > maxFrontmatter {
			return 0, nil, errors.New("frontmatter must end with --- within 64 KiB")
		}
		return advance, line, err
	})
	if !scanner.Scan() || strings.TrimPrefix(scanner.Text(), "\uFEFF") != "---" {
		return skill{}, errors.New("SKILL.md must start with YAML frontmatter (---)")
	}
	var header bytes.Buffer
	closed := false
	for scanner.Scan() {
		if scanner.Text() == "---" {
			closed = true
			break
		}
		header.WriteString(scanner.Text())
		header.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return skill{}, fmt.Errorf("read frontmatter: %w", err)
	}
	if !closed {
		return skill{}, errors.New("frontmatter must end with --- within 64 KiB")
	}
	var fields map[string]yaml.Node
	if err := yaml.Unmarshal(header.Bytes(), &fields); err != nil {
		return skill{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	name, description := fields["name"], fields["description"]
	invalidCharacters := strings.TrimFunc(name.Value, func(r rune) bool { return r == '-' || unicode.IsLower(r) || unicode.IsDigit(r) })
	if name.Tag != "!!str" || utf8.RuneCountInString(name.Value) < 1 || utf8.RuneCountInString(name.Value) > 64 || invalidCharacters != "" || strings.HasPrefix(name.Value, "-") || strings.HasSuffix(name.Value, "-") || strings.Contains(name.Value, "--") {
		return skill{}, errors.New("name must be 1–64 lowercase letters, digits or single hyphens, without leading or trailing hyphens")
	}
	if name.Value != filepath.Base(filepath.Dir(path)) {
		return skill{}, errors.New("name must match the skill directory name")
	}
	if description.Tag != "!!str" || strings.TrimSpace(description.Value) == "" || utf8.RuneCountInString(description.Value) > 1024 {
		return skill{}, errors.New("description must be a nonempty string of at most 1024 characters")
	}
	policy := byUser | byModel
	for _, flag := range []struct {
		key     string
		value   bool
		disable invocation
	}{
		{"disable-model-invocation", true, byModel},
		{"user-invocable", false, byUser},
	} {
		if node, exists := fields[flag.key]; exists {
			var value bool
			if node.Tag != "!!bool" || node.Decode(&value) != nil {
				return skill{}, fmt.Errorf("%s must be a YAML boolean", flag.key)
			}
			if value == flag.value {
				policy &^= flag.disable
			}
		}
	}
	return skill{name: name.Value, description: strings.TrimSpace(description.Value), path: path, invocable: policy}, nil
}

// Commands returns a sorted copy of the user-invocable command metadata.
func (c Catalog) Commands() []command {
	var commands []command
	for _, entry := range c.entries {
		if entry.invocable&byUser != 0 {
			commands = append(commands, command{Name: CommandPrefix + entry.name, Description: entry.description})
		}
	}
	return commands
}

// Instructions discloses only skills the model may choose on its own.
func (c Catalog) Instructions() string {
	type metadata struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Location    string `json:"location"`
	}
	var entries []metadata
	for _, entry := range c.entries {
		if entry.invocable&byModel != 0 {
			entries = append(entries, metadata{entry.name, entry.description, entry.path})
		}
	}
	if len(entries) == 0 {
		return ""
	}
	data, _ := json.Marshal(entries) // Only strings; encoding cannot fail.
	return "\n\nAvailable skills (metadata only):\n" + string(data) + "\nWhen a task matches a skill's description, use starlark_repl to read its SKILL.md before following it. Resolve relative references and scripts against the directory containing SKILL.md, using absolute paths. Read supporting files only as needed. Skills with disable-model-invocation: true require an explicit user request."
}

// Expand resolves a leading skill command before it enters conversation history.
// Ordinary prompts pass through unchanged. Unknown or model-only skills fail.
func (c Catalog) Expand(prompt string) (string, error) {
	trimmed := strings.TrimSpace(prompt)
	if !strings.HasPrefix(trimmed, CommandPrefix) {
		return prompt, nil
	}
	command := strings.Fields(trimmed)[0]
	name := strings.TrimPrefix(command, CommandPrefix)
	for _, entry := range c.entries {
		if entry.name != name {
			continue
		}
		if entry.invocable&byUser == 0 {
			return "", fmt.Errorf("skill %q is not user-invocable", name)
		}
		return prompt + fmt.Sprintf("\n\nUse the %q skill for this request. Read %q with starlark_repl before following its instructions. Resolve relative references and scripts against %q. Any text after %s is the user's input for the skill.", name, entry.path, filepath.Dir(entry.path), command), nil
	}
	return "", fmt.Errorf("unknown skill %q; choose an available /skill:<name> command", name)
}

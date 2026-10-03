package skills

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Skill represents a single skill, as defined by https://agentskills.io/specification
type Skill struct {
	// Name is the name of the skill. Max 64 characters
	Name string
	// Description is the skill description for what the skill does and when to use it. Max 1024 characters
	Description string
	// License name or reference to a bundled license file
	License string
	// Compatibility indicates environment requirements (intended product, system packages, network access, etc.). Max 500 characters
	Compatibility string
	// Metadata is arbitrary key-value mapping for additional metadata
	Metadata map[string]string
	// AllowedTools is an experimental field that is a space-separated string of tools that are pre-approved to run
	AllowedTools []string
	// InvocationDisabled reflects the disable-model-invocation boolean.
	InvocationDisabled bool
	// Path is the file path that the skill is stored in
	Path string
}

func GlobalPath() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".agents", "skills"), nil
}

func ProjectPath(cwd string) string {
	return filepath.Join(cwd, ".agents", "skills")
}

// Load reads SKILL.md from each immediate child directory of root, following
// symlinked directories. Paths are relative to root. Every child directory must
// contain a valid skill; other files and bundled resources are ignored.
// Load stops at the first error and returns no partial results.
func Load(root fs.FS) ([]Skill, error) {
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return nil, fmt.Errorf("read skills directory: %w", err)
	}
	var skills []Skill
	for _, entry := range entries {
		isDir := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			info, err := fs.Stat(root, entry.Name())
			if err != nil {
				return nil, fmt.Errorf("stat skill directory %q: %w", entry.Name(), err)
			}
			isDir = info.IsDir()
		}
		if !isDir {
			continue
		}
		filename := path.Join(entry.Name(), "SKILL.md")
		data, err := fs.ReadFile(root, filename)
		if err != nil {
			return nil, fmt.Errorf("read skill %q: %w", filename, err)
		}
		skill, err := parseSkill(data, filename)
		if err != nil {
			return nil, fmt.Errorf("parse skill %q: %w", filename, err)
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

// YAML nodes retain field presence and scalar types. Decoding straight into
// strings would silently coerce numbers and booleans, and lose explicit nulls.
type skillFrontmatter struct {
	Name               yaml.Node `yaml:"name"`
	Description        yaml.Node `yaml:"description"`
	License            yaml.Node `yaml:"license"`
	Compatibility      yaml.Node `yaml:"compatibility"`
	Metadata           yaml.Node `yaml:"metadata"`
	AllowedTools       yaml.Node `yaml:"allowed-tools"`
	InvocationDisabled yaml.Node `yaml:"disable-model-invocation"`
}

func parseSkill(data []byte, filename string) (Skill, error) {
	frontmatter, err := skillHeader(data)
	if err != nil {
		return Skill{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(frontmatter))
	decoder.KnownFields(true)
	var fields skillFrontmatter
	if err := decoder.Decode(&fields); err != nil {
		return Skill{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return Skill{}, fmt.Errorf("decode trailing frontmatter: %w", err)
		}
		return Skill{}, fmt.Errorf("frontmatter must contain one YAML mapping")
	}

	skill := Skill{Path: filename}
	var allowedTools string
	for _, field := range []struct {
		node     *yaml.Node
		name     string
		required bool
		limit    int
		value    *string
	}{
		{&fields.Name, "name", true, 64, &skill.Name},
		{&fields.Description, "description", true, 1024, &skill.Description},
		{&fields.License, "license", false, 0, &skill.License},
		{&fields.Compatibility, "compatibility", fields.Compatibility.Kind != 0, 500, &skill.Compatibility},
		{&fields.AllowedTools, "allowed-tools", false, 0, &allowedTools},
	} {
		value, err := skillString(field.node, field.name, field.required, field.limit)
		if err != nil {
			return Skill{}, err
		}
		*field.value = value
	}
	if strings.HasPrefix(skill.Name, "-") || strings.HasSuffix(skill.Name, "-") || strings.Contains(skill.Name, "--") {
		return Skill{}, fmt.Errorf("name must not start or end with a hyphen or contain consecutive hyphens")
	}
	for _, r := range skill.Name {
		if (!unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-') || unicode.ToLower(r) != r {
			return Skill{}, fmt.Errorf("name must contain only lowercase alphanumeric characters and hyphens")
		}
	}
	if skill.Name != path.Dir(filename) {
		return Skill{}, fmt.Errorf("name %q must match directory %q", skill.Name, path.Dir(filename))
	}
	if fields.Metadata.Kind != 0 {
		metadata, err := skillMetadata(&fields.Metadata)
		if err != nil {
			return Skill{}, err
		}
		skill.Metadata = metadata
	}
	skill.AllowedTools = strings.Fields(allowedTools)
	if fields.InvocationDisabled.Kind != 0 {
		node := resolveSkillAlias(&fields.InvocationDisabled)
		if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
			return Skill{}, fmt.Errorf("disable-model-invocation must be a boolean")
		}
		if err := node.Decode(&skill.InvocationDisabled); err != nil {
			return Skill{}, fmt.Errorf("decode disable-model-invocation: %w", err)
		}
	}
	return skill, nil
}

// Delimiters occupy their own lines. Embedded "---" in scalar values and
// arbitrary Markdown after the closing delimiter are not frontmatter.
func skillHeader(data []byte) ([]byte, error) {
	opening, remaining, newline := bytes.Cut(data, []byte("\n"))
	if !newline || !skillDelimiter(opening) {
		return nil, fmt.Errorf("SKILL.md must start with YAML frontmatter (---)")
	}
	header := remaining
	for {
		line, next, newline := bytes.Cut(remaining, []byte("\n"))
		if skillDelimiter(line) {
			return header[:len(header)-len(remaining)], nil
		}
		if !newline {
			return nil, fmt.Errorf("frontmatter must end with a closing --- line")
		}
		remaining = next
	}
}

func skillDelimiter(line []byte) bool {
	return bytes.Equal(bytes.TrimRight(line, " \t\r"), []byte("---"))
}

func skillString(node *yaml.Node, field string, required bool, limit int) (string, error) {
	if node.Kind == 0 {
		if required {
			return "", fmt.Errorf("missing required field %q", field)
		}
		return "", nil
	}
	node = resolveSkillAlias(node)
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" || !utf8.ValidString(node.Value) {
		return "", fmt.Errorf("%s must be a UTF-8 string", field)
	}
	if required && strings.TrimSpace(node.Value) == "" {
		return "", fmt.Errorf("%s must be non-empty", field)
	}
	if limit > 0 && utf8.RuneCountInString(node.Value) > limit {
		return "", fmt.Errorf("%s exceeds %d characters", field, limit)
	}
	return node.Value, nil
}

func skillMetadata(node *yaml.Node) (map[string]string, error) {
	node = resolveSkillAlias(node)
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("metadata must be a string-to-string mapping")
	}
	metadata := make(map[string]string, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, err := skillString(node.Content[i], "metadata key", false, 0)
		if err != nil {
			return nil, err
		}
		value, err := skillString(node.Content[i+1], "metadata value", false, 0)
		if err != nil {
			return nil, err
		}
		if _, exists := metadata[key]; exists {
			return nil, fmt.Errorf("duplicate metadata key %q", key)
		}
		metadata[key] = value
	}
	return metadata, nil
}

func resolveSkillAlias(node *yaml.Node) *yaml.Node {
	// YAML aliases refer to an already anchored node, not another alias.
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		return node.Alias
	}
	return node
}

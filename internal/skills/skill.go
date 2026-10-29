package skills

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/spachava753/cpe/internal/commandio"
)

const skillDefinition = "SKILL.md"

// Skill contains parsed Agent Skills frontmatter and its definition's location.
type Skill struct {
	// Metadata preserves client-specific attributes, including nested values.
	Metadata map[string]any
	// Name is the skill identifier declared in frontmatter.
	Name string
	// Description explains what the skill does and when to use it.
	Description string
	// License is the license name or a reference to a bundled license file.
	License string
	// Compatibility describes environment requirements (up to 500 characters).
	Compatibility string
	// Path names SKILL.md. Loaded paths are source-relative slash paths;
	// installed paths include the supplied installation root and use native syntax.
	Path string
	// AllowedTools lists tool names parsed from the allowed-tools field.
	AllowedTools []string
	// Violations records non-fatal length and directory-name warnings.
	Violations []string `json:",omitempty"`
	// InvocationDisabled indicates that the model should not invoke this skill.
	InvocationDisabled bool
}

// ProjectPath returns the default skill directory, cwd/.agents/skills.
func ProjectPath(cwd string) string {
	return commandio.ProjectPath(
		cwd,
		"skills",
	)
}

// Load reads SKILL.md from each immediate child directory of root, following
// directory symlinks. Returned paths are root-relative slash paths.
// Invalid definitions return nil skills and an error; specification warnings
// are recorded in Skill.Violations.
func Load(root fs.FS) ([]Skill, error) {
	entries, err := fs.ReadDir(
		root,
		".",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"read skills directory: %w",
			err,
		)
	}
	var skills []Skill
	for _, entry := range entries {
		isDir := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			info, err := fs.Stat(
				root,
				entry.Name(),
			)
			if err != nil {
				return nil, fmt.Errorf(
					"stat skill directory %q: %w",
					entry.Name(),
					err,
				)
			}
			isDir = info.IsDir()
		}
		if !isDir {
			continue
		}
		filename := path.Join(
			entry.Name(),
			"SKILL.md",
		)
		data, err := fs.ReadFile(
			root,
			filename,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"read skill %q: %w",
				filename,
				err,
			)
		}
		skill, err := parseSkill(
			data,
			filename,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"parse skill %q: %w",
				filename,
				err,
			)
		}
		skills = append(
			skills,
			skill,
		)
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

func decodeSkillFrontmatter(data []byte) (skillFrontmatter, error) {
	data = bytes.TrimPrefix(
		data,
		[]byte{
			0xef,
			0xbb,
			0xbf,
		},
	)
	frontmatter, err := skillHeader(data)
	if err != nil {
		return skillFrontmatter{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(frontmatter))
	var fields skillFrontmatter
	if err := decoder.Decode(&fields); err != nil {
		return skillFrontmatter{}, fmt.Errorf(
			"decode frontmatter: %w",
			err,
		)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return skillFrontmatter{}, fmt.Errorf(
				"decode trailing frontmatter: %w",
				err,
			)
		}
		return skillFrontmatter{}, fmt.Errorf("frontmatter must contain one YAML mapping")
	}
	return fields, nil
}

func parseSkill(data []byte, filename string) (Skill, error) {
	fields, err := decodeSkillFrontmatter(data)
	if err != nil {
		return Skill{}, err
	}
	skill := Skill{
		Path: filename,
	}
	var allowedTools string
	for _, field := range []struct {
		node     *yaml.Node
		value    *string
		name     string
		limit    int
		required bool
	}{
		{
			node:     &fields.Name,
			value:    &skill.Name,
			name:     "name",
			limit:    0,
			required: true,
		},
		{
			node:     &fields.Description,
			value:    &skill.Description,
			name:     "description",
			limit:    0,
			required: true,
		},
		{
			node:     &fields.License,
			value:    &skill.License,
			name:     "license",
			limit:    0,
			required: false,
		},
		{
			node:     &fields.Compatibility,
			value:    &skill.Compatibility,
			name:     "compatibility",
			limit:    500,
			required: fields.Compatibility.Kind != 0,
		},
		{
			node:     &fields.AllowedTools,
			value:    &allowedTools,
			name:     "allowed-tools",
			limit:    0,
			required: false,
		},
	} {
		value, err := skillString(
			field.node,
			field.name,
			field.required,
			field.limit,
		)
		if err != nil {
			return Skill{}, err
		}
		*field.value = value
	}
	if strings.HasPrefix(
		skill.Name,
		"-",
	) || strings.HasSuffix(
		skill.Name,
		"-",
	) || strings.Contains(
		skill.Name,
		"--",
	) {
		return Skill{}, fmt.Errorf("name must not start or end with a hyphen or contain consecutive hyphens")
	}
	for _, r := range skill.Name {
		if (!unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-') || unicode.ToLower(r) != r {
			return Skill{}, fmt.Errorf("name must contain only lowercase alphanumeric characters and hyphens")
		}
	}
	if directory := path.Dir(filename); directory != "." && skill.Name != path.Base(directory) {
		skill.Violations = append(
			skill.Violations,
			fmt.Sprintf(
				"name %q does not match directory %q",
				skill.Name,
				directory,
			),
		)
	}
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{
			name:  "name",
			value: skill.Name,
			limit: 64,
		},
		{
			name:  "description",
			value: skill.Description,
			limit: 1024,
		},
	} {
		if length := utf8.RuneCountInString(field.value); length > field.limit {
			skill.Violations = append(
				skill.Violations,
				fmt.Sprintf(
					"%s exceeds %d characters (found %d)",
					field.name,
					field.limit,
					length,
				),
			)
		}
	}
	if fields.Metadata.Kind != 0 {
		if err := uniqueMetadataKeys(
			&fields.Metadata,
			map[*yaml.Node]bool{},
			true,
		); err != nil {
			return Skill{}, err
		}
		if err := fields.Metadata.Decode(&skill.Metadata); err != nil {
			return Skill{}, fmt.Errorf(
				"decode metadata: %w",
				err,
			)
		}
	}
	skill.AllowedTools = strings.Fields(allowedTools)
	if fields.InvocationDisabled.Kind != 0 {
		node := resolveSkillAlias(&fields.InvocationDisabled)
		if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
			return Skill{}, fmt.Errorf("disable-model-invocation must be a boolean")
		}
		if err := node.Decode(&skill.InvocationDisabled); err != nil {
			return Skill{}, fmt.Errorf(
				"decode disable-model-invocation: %w",
				err,
			)
		}
	}
	return skill, nil
}

// Delimiters occupy their own lines. Embedded "---" in scalar values and
// arbitrary Markdown after the closing delimiter are not frontmatter.
func skillHeader(data []byte) ([]byte, error) {
	opening, remaining, newline := bytes.Cut(
		data,
		[]byte("\n"),
	)
	if !newline || !skillDelimiter(opening) {
		return nil, fmt.Errorf("SKILL.md must start with YAML frontmatter (---)")
	}
	header := remaining
	for {
		line, next, newline := bytes.Cut(
			remaining,
			[]byte("\n"),
		)
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
	return bytes.Equal(
		bytes.TrimRight(
			line,
			" \t\r",
		),
		[]byte("---"),
	)
}

func skillString(node *yaml.Node, field string, required bool, limit int) (string, error) {
	if node.Kind == 0 {
		if required {
			return "", fmt.Errorf(
				"missing required field %q",
				field,
			)
		}
		return "", nil
	}
	node = resolveSkillAlias(node)
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" || !utf8.ValidString(node.Value) {
		return "", fmt.Errorf(
			"%s must be a UTF-8 string",
			field,
		)
	}
	if required && strings.TrimSpace(node.Value) == "" {
		return "", fmt.Errorf(
			"%s must be non-empty",
			field,
		)
	}
	if limit > 0 && utf8.RuneCountInString(node.Value) > limit {
		return "", fmt.Errorf(
			"%s exceeds %d characters",
			field,
			limit,
		)
	}
	return node.Value, nil
}

// yaml.Node.Decode checks literal duplicates but misses aliased mapping keys.
// Visit each anchored node once, including nested metadata, before decoding.
func uniqueMetadataKeys(node *yaml.Node, visited map[*yaml.Node]bool, root bool) error {
	node = resolveSkillAlias(node)
	if visited[node] {
		return nil
	}
	visited[node] = true
	if node.Kind == yaml.MappingNode {
		keys := map[any]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := resolveSkillAlias(node.Content[i])
			if keyNode.Kind != yaml.ScalarNode {
				return fmt.Errorf("metadata keys must be scalar values")
			}
			var key any
			if root {
				// The exported metadata map decodes root keys as strings.
				var text string
				if err := keyNode.Decode(&text); err != nil {
					return err
				}
				key = text
			} else if err := keyNode.Decode(&key); err != nil {
				return err
			}
			if keys[key] {
				return fmt.Errorf(
					"duplicate metadata key %v",
					key,
				)
			}
			keys[key] = true
		}
	}
	for _, child := range node.Content {
		if err := uniqueMetadataKeys(
			child,
			visited,
			false,
		); err != nil {
			return err
		}
	}
	return nil
}

func resolveSkillAlias(node *yaml.Node) *yaml.Node {
	// YAML aliases refer to an already anchored node, not another alias.
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		return node.Alias
	}
	return node
}

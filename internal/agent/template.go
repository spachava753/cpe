package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"gopkg.in/yaml.v3"

	"github.com/spachava753/cpe/internal/config"
)

type TemplateData struct {
	*config.Config
}

// SystemPromptTemplate renders a template string with system info data
func SystemPromptTemplate(ctx context.Context, templateStr string, td TemplateData, w io.Writer) (string, error) {
	tmpl, err := template.New("sysinfo").Funcs(createTemplateFuncMap(ctx, w)).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template string: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// createTemplateFuncMap returns the FuncMap for system prompt templates
func createTemplateFuncMap(ctx context.Context, w io.Writer) template.FuncMap {
	// Start with sprig's rich set of template functions
	fm := sprig.TxtFuncMap()
	// Add/override with our custom helpers
	fm["fileExists"] = fileExists
	fm["includeFile"] = includeFile
	fm["exec"] = func(command string) string {
		return execCommand(ctx, command)
	}
	fm["skills"] = func(paths ...string) []Skill {
		return skills(w, paths...)
	}
	return fm
}

// fileExists checks if a file exists and is readable
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// includeFile reads and returns the contents of a file
func includeFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

// execCommand executes a bash command and returns stdout
func execCommand(ctx context.Context, command string) string {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

var (
	frontmatterRegexp = regexp.MustCompile(`(?s)^---\r?\n(.+?)\r?\n---`)
	skillNameRegexp   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// SkillMetadata represents the YAML frontmatter of a SKILL.md file
type SkillMetadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Skill represents a parsed skill with the metadata exposed to templates.
type Skill struct {
	Name        string
	Description string
	Path        string
}

// skills scans the provided directories for valid skill folders and returns
// a list of parsed skills for template-level formatting.
// Each skill folder must contain a SKILL.md file with valid YAML frontmatter.
func skills(w io.Writer, paths ...string) []Skill {
	var allSkills []Skill

	for _, basePath := range paths {
		// Expand home directory
		if strings.HasPrefix(basePath, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				basePath = filepath.Join(home, basePath[2:])
			}
		}

		// Check if path exists
		info, err := os.Stat(basePath)
		if err != nil || !info.IsDir() {
			continue
		}

		// Read all entries in the skills directory
		entries, err := os.ReadDir(basePath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillMdPath := filepath.Join(basePath, entry.Name(), "SKILL.md")

			// Check if SKILL.md exists
			if _, err := os.Stat(skillMdPath); err != nil {
				continue
			}

			// Parse the skill
			skill, err := parseSkill(skillMdPath)
			if err != nil {
				fmt.Fprintf(w, "warning: failed to load skill %q: %v\n", entry.Name(), err)
				continue
			}

			allSkills = append(allSkills, skill)
		}
	}

	return allSkills
}

// parseSkill reads a SKILL.md file and extracts the metadata
func parseSkill(skillMdPath string) (Skill, error) {
	content, err := os.ReadFile(skillMdPath)
	if err != nil {
		return Skill{}, err
	}

	// Extract YAML frontmatter
	frontmatter, err := extractFrontmatter(string(content))
	if err != nil {
		return Skill{}, err
	}

	var meta SkillMetadata
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return Skill{}, err
	}

	// Validate required fields
	if meta.Name == "" || meta.Description == "" {
		return Skill{}, fmt.Errorf("skill missing required name or description")
	}

	// Validate skill name format (lowercase alphanumeric with hyphens)
	if !isValidSkillName(meta.Name) {
		return Skill{}, fmt.Errorf("invalid skill name: %s", meta.Name)
	}

	return Skill{
		Name:        meta.Name,
		Description: meta.Description,
		Path:        filepath.Dir(skillMdPath),
	}, nil
}

// extractFrontmatter extracts YAML frontmatter from markdown content
func extractFrontmatter(content string) (string, error) {
	// Match YAML frontmatter between --- delimiters
	matches := frontmatterRegexp.FindStringSubmatch(content)
	if len(matches) < 2 {
		return "", fmt.Errorf("no frontmatter found")
	}
	return matches[1], nil
}

// isValidSkillName checks if the skill name follows the spec
// (lowercase letters, numbers, and hyphens only, max 64 chars)
func isValidSkillName(name string) bool {
	if len(name) > 64 || len(name) == 0 {
		return false
	}
	return skillNameRegexp.MatchString(name)
}

package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill describes one discovered skill directory.
//
// A skill is loaded from a directory containing SKILL.md with YAML
// frontmatter. Name and Description are promoted from that frontmatter for
// common callers, while Metadata preserves the complete frontmatter map for
// prompt templates. Path is a stable model-facing display path such as
// "./.agents/skills/domain-modeling" or "~/.agents/skills/domain-modeling";
// use AbsPath for filesystem access.
type Skill struct {
	// Name is the frontmatter name. Valid names are lowercase words separated by
	// hyphens, for example "domain-modeling".
	Name string
	// Description is the frontmatter description shown in prompt templates and
	// command metadata.
	Description string
	// Path is the stable display path callers can place in prompts. It preserves
	// prefixes such as "./" and "~" and is not intended for filesystem access.
	Path string
	// AbsPath is the absolute filesystem path to the skill directory.
	AbsPath string
	// DisableModelInvocation is true when frontmatter sets
	// disable-model-invocation: true. Such skills are omitted from ModelVisible
	// results but remain discoverable for explicit caller-controlled use.
	DisableModelInvocation bool
	// Metadata is the complete YAML frontmatter map from SKILL.md.
	Metadata map[string]any
}

// Catalog is the discovered set of skills available from the configured roots.
type Catalog struct {
	// Skills is ordered by root precedence: project-local skills first, then
	// global skills. Duplicate skill names are omitted after the first match.
	Skills []Skill
}

// DiscoverOptions configures skill discovery.
type DiscoverOptions struct {
	// Cwd is the project directory whose .agents/skills root should be scanned.
	// If empty, Discover uses os.Getwd.
	Cwd string
	// HomeDir is the home directory whose .agents/skills root should be scanned.
	// If empty, Discover uses os.UserHomeDir; tests may set it to isolate global
	// skills.
	HomeDir string
}

// Regular expressions for parsing SKILL.md frontmatter and validating the
// slash-command-safe skill name grammar shared with ACP command expansion.
var (
	frontmatterRegexp = regexp.MustCompile(`(?s)^---\r?\n(.+?)\r?\n---`)
	skillNameRegexp   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Discover scans project and user skill roots for valid skill directories.
// Context-scoped slog attributes are preserved on discovery warnings.
//
// It looks for SKILL.md files under <Cwd>/.agents/skills and
// <HomeDir>/.agents/skills. Project-local skills are returned before global
// skills and take precedence when both roots define the same skill name. Invalid
// skills, broken symlinked directories, and duplicate global skills are skipped;
// those conditions are logged with slog.Warn rather than returned as hard
// errors so one bad skill does not hide the rest of the catalog.
//
// For example, with Cwd "/repo" and HomeDir "/home/me", a skill named
// "domain-modeling" under /repo/.agents/skills is returned with Path
// "./.agents/skills/domain-modeling", while one under /home/me/.agents/skills
// is returned with Path "~/.agents/skills/domain-modeling".
func Discover(ctx context.Context, opts DiscoverOptions) Catalog {
	cwd := opts.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return Catalog{}
		}
	}

	home := opts.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			home = ""
		}
	}

	roots := []skillRoot{{
		absPath:     filepath.Join(cwd, ".agents", "skills"),
		displayPath: "./.agents/skills",
	}}
	if home != "" {
		roots = append(roots, skillRoot{
			absPath:     filepath.Join(home, ".agents", "skills"),
			displayPath: "~/.agents/skills",
		})
	}

	return discoverRoots(ctx, roots)
}

// ModelVisible returns skills that may be shown to the model by default.
//
// Skills whose frontmatter contains disable-model-invocation: true are filtered
// out. The returned slice preserves catalog order and contains copies of Skill
// values, so appending to the returned slice does not mutate the catalog.
func (c Catalog) ModelVisible() []Skill {
	visible := make([]Skill, 0, len(c.Skills))
	for _, skill := range c.Skills {
		if skill.DisableModelInvocation {
			continue
		}
		visible = append(visible, skill)
	}
	return visible
}

// skillRoot pairs the filesystem directory to scan with the display prefix used
// when building Skill.Path values for prompt-facing output.
type skillRoot struct {
	absPath     string
	displayPath string
}

// discoverRoots walks the configured roots in precedence order and returns the
// first valid skill for each skill name. Warnings are logged and discovery keeps
// going so unrelated skills remain available.
func discoverRoots(ctx context.Context, roots []skillRoot) Catalog {
	var discovered []Skill
	seen := make(map[string]struct{})

	for _, root := range roots {
		info, err := os.Stat(root.absPath)
		if err != nil || !info.IsDir() {
			continue
		}

		entries, err := os.ReadDir(root.absPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			candidate, err := skillCandidateDir(root.absPath, entry)
			if err != nil {
				slog.WarnContext(ctx, "failed to inspect skill", "skill", entry.Name(), "err", err)
				continue
			}
			if !candidate {
				continue
			}

			skillMdPath := filepath.Join(root.absPath, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillMdPath); err != nil {
				continue
			}

			skill, err := parseSkill(skillMdPath, displayJoin(root.displayPath, entry.Name()))
			if err != nil {
				slog.WarnContext(ctx, "failed to load skill", "skill", entry.Name(), "err", err)
				continue
			}
			if _, exists := seen[skill.Name]; exists {
				slog.WarnContext(ctx, "ignoring duplicate skill", "skill", skill.Name, "path", skill.Path)
				continue
			}

			seen[skill.Name] = struct{}{}
			discovered = append(discovered, skill)
		}
	}

	return Catalog{Skills: discovered}
}

// displayJoin preserves model-facing path prefixes such as "./" and "~";
// filepath.Join would clean "./.agents/skills/name" to ".agents/skills/name".
func displayJoin(base, name string) string {
	return strings.TrimRight(base, "/") + "/" + name
}

// skillCandidateDir reports whether entry should be considered a skill
// directory. Real directories are accepted, symlinks are accepted only when they
// resolve to directories, and non-directory files are ignored.
func skillCandidateDir(basePath string, entry os.DirEntry) (bool, error) {
	if entry.IsDir() {
		return true, nil
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false, nil
	}

	path := filepath.Join(basePath, entry.Name())
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("resolve symlink: %w", err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("symlink target is not a directory")
	}

	return true, nil
}

// parseSkill reads SKILL.md, parses its YAML frontmatter, validates the
// required metadata, and returns the promoted Skill fields plus the original
// metadata map. displayPath is the caller-chosen prompt-facing path for the
// containing skill directory.
func parseSkill(skillMdPath, displayPath string) (Skill, error) {
	content, err := os.ReadFile(skillMdPath)
	if err != nil {
		return Skill{}, err
	}

	frontmatter, err := extractFrontmatter(string(content))
	if err != nil {
		return Skill{}, err
	}

	var metadata map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &metadata); err != nil {
		return Skill{}, err
	}

	name, _ := metadata["name"].(string)
	description, _ := metadata["description"].(string)
	if name == "" || description == "" {
		return Skill{}, fmt.Errorf("skill missing required name or description")
	}
	if !isValidSkillName(name) {
		return Skill{}, fmt.Errorf("invalid skill name: %s", name)
	}
	disableModelInvocation, _ := metadata["disable-model-invocation"].(bool)

	return Skill{
		Name:                   name,
		Description:            description,
		Path:                   displayPath,
		AbsPath:                filepath.Dir(skillMdPath),
		DisableModelInvocation: disableModelInvocation,
		Metadata:               metadata,
	}, nil
}

// extractFrontmatter returns the YAML block between the opening and closing
// --- markers at the start of SKILL.md.
func extractFrontmatter(content string) (string, error) {
	matches := frontmatterRegexp.FindStringSubmatch(content)
	if len(matches) < 2 {
		return "", fmt.Errorf("no frontmatter found")
	}
	return matches[1], nil
}

// isValidSkillName enforces the slash-command-safe skill name grammar:
// lowercase alphanumeric words separated by single hyphens, up to 64 bytes.
func isValidSkillName(name string) bool {
	if len(name) > 64 || len(name) == 0 {
		return false
	}
	return skillNameRegexp.MatchString(name)
}

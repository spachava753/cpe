package skills

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/source"
)

func (m *Manager) acquire(ctx context.Context, locator string) (source.Checkout, error) {
	var client source.Client
	if m != nil {
		client = m.sources
	}
	return source.Acquire(
		ctx,
		client,
		locator,
	)
}

// Select by declared skill names, not by source directory names.
func discover(ctx context.Context, filesystem fs.FS, opts DiscoveryOptions, names []string) ([]Skill, error) {
	var result []Skill
	err := fs.WalkDir(
		filesystem,
		".",
		func(directory string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() {
				return nil
			}
			if excludedDirectory(entry.Name()) {
				return fs.SkipDir
			}
			if !opts.FullDepth && directory != "." && len(strings.Split(
				directory,
				"/",
			)) > 5 {
				return fs.SkipDir
			}
			entries, err := fs.ReadDir(
				filesystem,
				directory,
			)
			if err != nil {
				return err
			}
			for _, child := range entries {
				if child.Name() != "SKILL.md" {
					continue
				}
				if err := bundlefs.CheckSourceDirectory(
					filesystem,
					directory,
				); err != nil {
					return err
				}
				data, err := readDefinitionData(
					filesystem,
					directory,
				)
				filename := path.Join(
					directory,
					"SKILL.md",
				)
				if len(names) > 0 {
					var declaredName string
					if err == nil || data != nil {
						fields, parseErr := decodeSkillFrontmatter(data)
						if parseErr == nil {
							declaredName, parseErr = skillString(
								&fields.Name,
								"name",
								true,
								0,
							)
						}
						if parseErr != nil && err == nil {
							err = fmt.Errorf(
								"parse %s: %w",
								filename,
								parseErr,
							)
						}
					}
					if err != nil && declaredName == "" {
						// An unreadable name cannot be selected by frontmatter. Keep
						// failures at requested directories fatal; skip unrelated bundles.
						if directory != "." && !slices.Contains(
							names,
							path.Base(directory),
						) {
							return fs.SkipDir
						}
						return err
					}
					if !slices.Contains(
						names,
						declaredName,
					) {
						return fs.SkipDir
					}
				}
				if err != nil {
					return err
				}
				skill, err := parseSkill(
					data,
					filename,
				)
				if err != nil {
					return fmt.Errorf(
						"parse %s: %w",
						filename,
						err,
					)
				}
				result = append(
					result,
					skill,
				)
				return fs.SkipDir // Nested resources belong to this bundle, not a collection.
			}
			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"discover source skills: %w",
			err,
		)
	}
	return result, nil
}

// Source callers reject directory symlinks before reading. Installed inventory
// may follow user-owned directory links without acquiring or changing their data.
func readDefinitionData(filesystem fs.FS, directory string) ([]byte, error) {
	filename := path.Join(
		directory,
		"SKILL.md",
	)
	entries, err := fs.ReadDir(
		filesystem,
		directory,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"read %s: %w",
			filename,
			err,
		)
	}
	regular := false
	for _, entry := range entries {
		if entry.Name() == "SKILL.md" {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			regular = info.Mode().IsRegular()
			break
		}
	}
	if !regular {
		return nil, fmt.Errorf(
			"%s must be a regular definition file",
			filename,
		)
	}
	data, err := fs.ReadFile(
		filesystem,
		filename,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"read %s: %w",
			filename,
			err,
		)
	}
	if !utf8.Valid(data) {
		// Retain bytes so discovery can identify a selected name in valid
		// frontmatter even when the body has an encoding error.
		return data, fmt.Errorf(
			"%s must contain UTF-8",
			filename,
		)
	}
	return data, nil
}

func readDefinition(filesystem fs.FS, directory string) (Skill, error) {
	data, err := readDefinitionData(
		filesystem,
		directory,
	)
	if err != nil {
		return Skill{}, err
	}
	filename := path.Join(
		directory,
		"SKILL.md",
	)
	skill, err := parseSkill(
		data,
		filename,
	)
	if err != nil {
		return Skill{}, fmt.Errorf(
			"parse %s: %w",
			filename,
			err,
		)
	}
	return skill, nil
}

func excludedDirectory(name string) bool { return name == ".git" || name == "node_modules" }

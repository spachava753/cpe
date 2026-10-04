package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"
)

func (m *Manager) acquire(ctx context.Context, source string) (Checkout, error) {
	if err := ctx.Err(); err != nil {
		return Checkout{}, err
	}
	if m == nil || m.sources == nil {
		return Checkout{}, errors.New("source client is unavailable")
	}
	if strings.TrimSpace(source) == "" {
		return Checkout{}, errors.New("source is required")
	}
	checkout, err := m.sources.Fetch(
		ctx,
		source,
	)
	if err != nil {
		return Checkout{}, fmt.Errorf(
			"fetch source: %w",
			err,
		)
	}
	if checkout.FS == nil {
		return Checkout{}, errors.Join(
			errors.New("source filesystem is unavailable"),
			releaseCheckout(checkout),
		)
	}
	return checkout, nil
}

func releaseCheckout(checkout Checkout) error {
	if checkout.Cleanup != nil {
		return checkout.Cleanup()
	}
	return nil
}

func discover(ctx context.Context, filesystem fs.FS, opts DiscoveryOptions) ([]Skill, error) {
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
				skill, err := readDefinition(
					filesystem,
					directory,
				)
				if err != nil {
					return err
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

func readDefinition(filesystem fs.FS, directory string) (Skill, error) {
	if err := ensureSourceDirectory(
		filesystem,
		directory,
	); err != nil {
		return Skill{}, err
	}
	filename := path.Join(
		directory,
		"SKILL.md",
	)
	entries, err := fs.ReadDir(
		filesystem,
		directory,
	)
	if err != nil {
		return Skill{}, fmt.Errorf(
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
				return Skill{}, err
			}
			regular = info.Mode().IsRegular()
			break
		}
	}
	if !regular {
		return Skill{}, fmt.Errorf(
			"%s must be a regular definition file",
			filename,
		)
	}
	data, err := fs.ReadFile(
		filesystem,
		filename,
	)
	if err != nil {
		return Skill{}, fmt.Errorf(
			"read %s: %w",
			filename,
			err,
		)
	}
	if !utf8.Valid(data) {
		return Skill{}, fmt.Errorf(
			"%s must contain UTF-8",
			filename,
		)
	}
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

// Check components through parent directory entries: fs.Stat and WalkDir's root
// follow links on an OS-backed FS, unlike DirEntry.Info.
func ensureSourceDirectory(filesystem fs.FS, directory string) error {
	if !fs.ValidPath(directory) || strings.ContainsAny(
		directory,
		"\\\x00",
	) {
		return fmt.Errorf(
			"invalid source directory %q",
			directory,
		)
	}
	if directory == "." {
		return nil
	}
	parent := "."
	for component := range strings.SplitSeq(
		directory,
		"/",
	) {
		entries, err := fs.ReadDir(
			filesystem,
			parent,
		)
		if err != nil {
			return err
		}
		found := false
		for _, entry := range entries {
			if entry.Name() != component {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				return fmt.Errorf(
					"source directory %s is a symlink",
					path.Join(
						parent,
						component,
					),
				)
			}
			if !info.IsDir() {
				return fmt.Errorf(
					"source path %s is not a directory",
					path.Join(
						parent,
						component,
					),
				)
			}
			found = true
			break
		}
		if !found {
			return &fs.PathError{
				Op:   "read source directory",
				Path: directory,
				Err:  fs.ErrNotExist,
			}
		}
		parent = path.Join(
			parent,
			component,
		)
	}
	return nil
}

func excludedDirectory(name string) bool { return name == ".git" || name == "node_modules" }

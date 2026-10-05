package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
)

// Manager owns skill discovery and destination files.
// It returns domain results without prompting or writing command output.
type Manager struct {
	sources      SourceClient
	destinations afero.Fs
}

// NewManager supplies acquisition and destination filesystem boundaries. A nil
// source client is sufficient for installed listing and removal.
func NewManager(sources SourceClient, destinations afero.Fs) *Manager {
	return &Manager{
		sources:      sources,
		destinations: destinations,
	}
}

type DiscoveryOptions struct{ FullDepth bool }

// InstalledSkill is a skill found in a destination directory.
type InstalledSkill struct {
	Skill     Skill
	Directory string
}

// Directory is an exact skill root. Path selects one source-relative bundle
// directory and is exclusive with Skills. With neither, install all discovered skills.
type InstallRequest struct {
	Source    string
	Directory string
	Skills    []string
	Path      *string
	Override  bool
	Discovery DiscoveryOptions
}

// Removal selects explicit installed skill directory names.
type RemoveRequest struct {
	Directory string
	Skills    []string
}

// ListSource returns source-relative Skill.Path values using io/fs conventions.
func (m *Manager) ListSource(ctx context.Context, source string, opts DiscoveryOptions) (result []Skill, err error) {
	checkout, err := m.acquire(
		ctx,
		source,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(
			err,
			releaseCheckout(checkout),
		)
		if err != nil {
			result = nil
		}
	}()
	return discover(
		ctx,
		checkout.FS,
		opts,
		nil,
	)
}

// Install copies selected bundles. Override replaces complete existing bundles.
func (m *Manager) Install(ctx context.Context, request InstallRequest) (result []InstalledSkill, err error) {
	if err := m.destination(
		ctx,
		request.Directory,
	); err != nil {
		return nil, err
	}
	if request.Path != nil {
		if len(request.Skills) > 0 {
			return nil, errors.New("--path and --skill are mutually exclusive")
		}
		if !fs.ValidPath(*request.Path) || strings.ContainsAny(
			*request.Path,
			"\\\x00",
		) {
			return nil, fmt.Errorf(
				"invalid source directory %q; --path must be a source-relative skill directory (use . for the source root)",
				*request.Path,
			)
		}
	}
	names, err := selectedNames(request.Skills)
	if err != nil {
		return nil, err
	}
	checkout, err := m.acquire(
		ctx,
		request.Source,
	)
	if err != nil {
		return nil, err
	}
	released := false
	defer func() {
		if !released {
			err = errors.Join(
				err,
				releaseCheckout(checkout),
			)
			if err != nil {
				result = nil
			}
		}
	}()
	var available []Skill
	if request.Path != nil {
		if err := ensureSourceDirectory(
			checkout.FS,
			*request.Path,
		); err != nil {
			return nil, err
		}
		skill, err := readDefinition(
			checkout.FS,
			*request.Path,
		)
		if err != nil {
			return nil, err
		}
		available = []Skill{
			skill,
		}
		names = []string{
			skill.Name,
		}
	} else {
		available, err = discover(
			ctx,
			checkout.FS,
			request.Discovery,
			names,
		)
		if err != nil {
			return nil, err
		}
	}
	candidates := map[string][]Skill{}
	for _, skill := range available {
		candidates[skill.Name] = append(
			candidates[skill.Name],
			skill,
		)
	}
	if len(names) == 0 {
		for name := range candidates {
			names = append(
				names,
				name,
			)
		}
	}
	sort.Strings(names)
	var changes []bundleChange
	for _, name := range names {
		options := candidates[name]
		if len(options) == 0 {
			return nil, fmt.Errorf(
				"skill %q not found: %w",
				name,
				fs.ErrNotExist,
			)
		}
		if len(options) > 1 {
			var directories []string
			for _, skill := range options {
				directories = append(
					directories,
					sourceDirectory(skill),
				)
			}
			return nil, fmt.Errorf(
				"ambiguous skill name %q at source directories %q; use --path to select one",
				name,
				directories,
			)
		}
		filename := filepath.Join(
			request.Directory,
			name,
		)
		if info, statErr := m.pathInfo(filename); statErr == nil {
			if !request.Override {
				return nil, fmt.Errorf(
					"destination %s already exists; use --override to replace it: %w",
					filename,
					fs.ErrExist,
				)
			}
			if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
				return nil, fmt.Errorf(
					"destination %s is not a regular skill directory",
					filename,
				)
			}
			definition, err := m.pathInfo(filepath.Join(
				filename,
				"SKILL.md",
			))
			if err != nil {
				return nil, err
			}
			if !definition.Mode().IsRegular() {
				return nil, fmt.Errorf(
					"destination %s must contain a regular SKILL.md",
					filename,
				)
			}
		} else if !errors.Is(
			statErr,
			fs.ErrNotExist,
		) {
			return nil, statErr
		}
		skill := options[0]
		directory := sourceDirectory(skill)
		payload, err := readPayload(
			ctx,
			checkout.FS,
			directory,
		)
		if err != nil {
			return nil, err
		}
		action := installBundle
		if request.Override {
			action = replaceBundle
		}
		changes = append(
			changes,
			bundleChange{
				name:    name,
				payload: payload,
				action:  action,
			},
		)
		skill.Path = filepath.Join(
			filename,
			"SKILL.md",
		)
		result = append(
			result,
			InstalledSkill{
				Skill:     skill,
				Directory: filename,
			},
		)
	}
	released = true
	if err := releaseCheckout(checkout); err != nil {
		return nil, fmt.Errorf(
			"release source: %w",
			err,
		)
	}
	if err := m.applyChanges(
		ctx,
		request.Directory,
		changes,
	); err != nil {
		if _, committed := errors.AsType[*CommittedCleanupError](err); !committed {
			result = nil
		}
		return result, fmt.Errorf(
			"install skills: %w",
			err,
		)
	}
	return result, nil
}

// ListInstalled inspects one exact root, including manually copied skills and
// symlinked skill directories. Listing never mutates their targets.
func (m *Manager) ListInstalled(ctx context.Context, directory string) ([]InstalledSkill, error) {
	if err := m.destination(
		ctx,
		directory,
	); err != nil {
		return nil, err
	}
	entries, err := afero.ReadDir(
		m.destinations,
		directory,
	)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	filesystem := afero.NewIOFS(afero.NewBasePathFs(
		m.destinations,
		directory,
	))
	var result []InstalledSkill
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() && entry.Mode()&fs.ModeSymlink == 0 {
			continue
		}
		name := entry.Name()
		info, err := m.pathInfo(filepath.Join(
			directory,
			name,
		))
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			info, err = m.destinations.Stat(filepath.Join(
				directory,
				name,
			))
			if err != nil {
				return nil, err
			}
		}
		if !info.IsDir() {
			continue
		}
		if _, err := m.pathInfo(filepath.Join(
			directory,
			name,
			"SKILL.md",
		)); errors.Is(
			err,
			fs.ErrNotExist,
		) {
			continue
		} else if err != nil {
			return nil, err
		}
		skill, err := readDefinition(
			filesystem,
			name,
		)
		if err != nil {
			return nil, err
		}
		skill.Path = filepath.Join(
			directory,
			name,
			"SKILL.md",
		)
		installed := InstalledSkill{
			Skill: skill,
			Directory: filepath.Join(
				directory,
				name,
			),
		}
		result = append(
			result,
			installed,
		)
	}
	return result, nil
}

// Remove deletes named skill directories without parsing their frontmatter.
func (m *Manager) Remove(ctx context.Context, request RemoveRequest) ([]string, error) {
	plan, err := m.prepareRemoval(
		ctx,
		request,
	)
	if err != nil {
		return nil, err
	}
	if err := m.applyChanges(
		ctx,
		request.Directory,
		plan.changes,
	); err != nil {
		if _, committed := errors.AsType[*CommittedCleanupError](err); committed {
			return plan.paths, fmt.Errorf(
				"remove skills: %w",
				err,
			)
		}
		return nil, fmt.Errorf(
			"remove skills: %w",
			err,
		)
	}
	return plan.paths, nil
}

func (m *Manager) pathInfo(filename string) (fs.FileInfo, error) {
	if filesystem, ok := m.destinations.(afero.Lstater); ok {
		info, _, err := filesystem.LstatIfPossible(filename)
		return info, err
	}
	return m.destinations.Stat(filename)
}

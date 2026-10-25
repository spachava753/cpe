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

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/source"
)

// Manager discovers and manages skill bundles in source and destination filesystems.
// A bundle contains SKILL.md and its resources. Construct a Manager with NewManager.
type Manager struct {
	sources      source.Client
	destinations afero.Fs
}

// NewManager uses sources to fetch trees and destinations to manage installed files.
// sources may be nil for installed listing and removal. Installed operations require
// a non-nil destinations filesystem, such as afero.NewOsFs().
func NewManager(sources source.Client, destinations afero.Fs) *Manager {
	return &Manager{
		sources:      sources,
		destinations: destinations,
	}
}

// DiscoveryOptions controls recursive source search, which skips .git and node_modules.
type DiscoveryOptions struct {
	// FullDepth removes the default search limit of five directory components.
	FullDepth bool
}

// InstalledSkill pairs a skill with its installed bundle directory.
type InstalledSkill struct {
	// Directory is the bundle's location beneath the supplied destination root.
	Directory string
	// Skill.Path names the installed SKILL.md file in native filesystem syntax.
	Skill Skill
}

// RemovedSkill identifies a deleted bundle. Unreadable frontmatter yields the
// directory name and an empty description.
type RemovedSkill struct {
	Name        string
	Description string
	// Path names the former SKILL.md file in native filesystem syntax.
	Path string
}

// InstallRequest selects skills and an exact root for Manager.Install.
// Empty Skills and nil Path select all discovered skills.
type InstallRequest struct {
	// Path selects a source-relative bundle directory; "." selects the root.
	// It uses io/fs slash paths and is mutually exclusive with Skills.
	Path *string
	// Source is a locator accepted by the configured source.Client.
	Source string
	// Directory receives bundles at Directory/<declared skill name>.
	Directory string
	// Skills selects declared frontmatter names. Use Path to resolve ambiguity.
	Skills []string
	// Override replaces existing bundles, including obsolete files.
	Override bool
	// Discovery controls search when Path is nil.
	Discovery DiscoveryOptions
}

// RemoveRequest selects installed bundle directories for Manager.Remove.
type RemoveRequest struct {
	// Directory is the exact installation root in native filesystem syntax.
	Directory string
	// Skills contains child directory names. At least one name is required.
	Skills []string
}

// ListSource fetches source and discovers skills according to opts.
// Skill.Path values are source-relative SKILL.md paths using slash separators.
// The checkout is closed before returning; any error returns nil skills.
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
			checkout.Close(),
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

// Install copies selected bundles to request.Directory/<declared skill name>.
// Payloads omit .git and node_modules and reject symlinks and special files.
// Existing destinations must be regular directories containing a regular SKILL.md.
//
// Publication failures attempt rollback. Non-nil results with an error identify
// committed installations whose cleanup failed; rollback failures retain recovery
// files described in the error.
func (m *Manager) Install(ctx context.Context, request InstallRequest) (result []InstalledSkill, installErr error) {
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
	names, installErr := selectedNames(request.Skills)
	if installErr != nil {
		return nil, installErr
	}
	checkout, installErr := m.acquire(
		ctx,
		request.Source,
	)
	if installErr != nil {
		return nil, installErr
	}
	released := false
	defer func() {
		if !released {
			installErr = errors.Join(
				installErr,
				checkout.Close(),
			)
			if installErr != nil {
				result = nil
			}
		}
	}()
	var available []Skill
	if request.Path != nil {
		if err := bundlefs.CheckSourceDirectory(
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
		available, installErr = discover(
			ctx,
			checkout.FS,
			request.Discovery,
			names,
		)
		if installErr != nil {
			return nil, installErr
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
	var changes []bundlefs.Change
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
		if info, statErr := bundlefs.Lstat(
			m.destinations,
			filename,
		); statErr == nil {
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
			if err := m.checkDefinition(filename); err != nil {
				return nil, err
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
		action := bundlefs.Install
		if request.Override {
			action = bundlefs.Replace
		}
		changes = append(
			changes,
			bundlefs.Change{
				Name:    name,
				Payload: payload,
				Action:  action,
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
	if err := checkout.Close(); err != nil {
		return nil, fmt.Errorf(
			"release source: %w",
			err,
		)
	}
	if outcome, err := bundlefs.Apply(
		ctx,
		m.destinations,
		request.Directory,
		changes,
		m.checkDefinition,
	); err != nil {
		if outcome != bundlefs.Committed {
			result = nil
		}
		return result, fmt.Errorf(
			"install skills: %w",
			err,
		)
	}
	return result, nil
}

// ListInstalled reads bundles immediately beneath directory, including manually
// copied and symlinked bundles. Entries without SKILL.md are skipped.
// A missing root returns no results; malformed definitions return nil results and an error.
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
		info, statErr := bundlefs.Lstat(
			m.destinations,
			filepath.Join(
				directory,
				name,
			),
		)
		if statErr != nil {
			return nil, statErr
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			info, statErr = m.destinations.Stat(filepath.Join(
				directory,
				name,
			))
			if statErr != nil {
				return nil, statErr
			}
		}
		if !info.IsDir() {
			continue
		}
		if _, err := bundlefs.Lstat(
			m.destinations,
			filepath.Join(
				directory,
				name,
				"SKILL.md",
			),
		); errors.Is(
			err,
			fs.ErrNotExist,
		) {
			continue
		} else if err != nil {
			return nil, err
		}
		skill, definitionErr := readDefinition(
			filesystem,
			name,
		)
		if definitionErr != nil {
			return nil, definitionErr
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

// Remove deletes selected bundle directories containing regular SKILL.md files.
// Directory symlinks are rejected; result metadata is read best-effort.
// Non-nil results with an error identify committed removals whose cleanup failed.
// Failed rollback retains recovery files described in the error.
func (m *Manager) Remove(ctx context.Context, request RemoveRequest) ([]RemovedSkill, error) {
	plan, err := m.prepareRemoval(
		ctx,
		request,
	)
	if err != nil {
		return nil, err
	}
	filesystem := afero.NewIOFS(afero.NewBasePathFs(
		m.destinations,
		request.Directory,
	))
	var result []RemovedSkill
	for _, directory := range plan.paths {
		removed := RemovedSkill{
			Name: filepath.Base(directory),
			Path: filepath.Join(
				directory,
				"SKILL.md",
			),
		}
		if skill, err := readDefinition(
			filesystem,
			filepath.Base(directory),
		); err == nil {
			removed.Name = skill.Name
			removed.Description = skill.Description
		}
		result = append(
			result,
			removed,
		)
	}
	if outcome, err := bundlefs.Apply(
		ctx,
		m.destinations,
		request.Directory,
		plan.changes,
		m.checkDefinition,
	); err != nil {
		if outcome == bundlefs.Committed {
			return result, fmt.Errorf(
				"remove skills: %w",
				err,
			)
		}
		return nil, fmt.Errorf(
			"remove skills: %w",
			err,
		)
	}
	return result, nil
}

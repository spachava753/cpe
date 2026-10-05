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

// Manager owns discovery, destination files, and skills.json bookkeeping.
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

// SkillSource identifies a bundle in its original source, never a temporary
// checkout path. Directory is relative to the acquired source filesystem.
type SkillSource struct {
	Location  string `json:"location"`
	Directory string `json:"directory"`
}

// A nil Source denotes an unmanaged installation.
type InstalledSkill struct {
	Skill     Skill
	Directory string
	Source    *SkillSource
}

// Directory is an exact skill root. Path selects one source-relative bundle
// directory and is exclusive with Skills. With neither, install all discovered skills.
type InstallRequest struct {
	Source    string
	Directory string
	Skills    []string
	Path      *string
	Discovery DiscoveryOptions
}

// Removal selects explicit installed directory names, including unmanaged skills.
type RemoveRequest struct {
	Directory string
	Skills    []string
}

// Empty Skills selects all managed installations in the exact destination root.
type UpdateRequest struct {
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

// Install copies selected bundles and records their original provenance.
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
	records, err := m.records(request.Directory)
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
		if _, ok := records.Skills[name]; ok {
			return nil, fmt.Errorf(
				"skill %q already has an installation record",
				name,
			)
		}
		filename := filepath.Join(
			request.Directory,
			name,
		)
		if _, statErr := m.pathInfo(filename); statErr == nil {
			return nil, fmt.Errorf(
				"destination %s already exists: %w",
				filename,
				fs.ErrExist,
			)
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
		source := SkillSource{
			Location:  request.Source,
			Directory: directory,
		}
		records.Skills[name] = source
		changes = append(
			changes,
			bundleChange{
				name:    name,
				payload: payload,
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
				Source:    &source,
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
		records,
		true,
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
	records, err := m.records(directory)
	if err != nil {
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
		if source, ok := records.Skills[name]; ok {
			installed.Source = &source
		}
		result = append(
			result,
			installed,
		)
	}
	return result, nil
}

// Remove deletes named bundle directories and their records when present.
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
		plan.records,
		plan.writeRecords,
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

// Update replaces only recorded bundles, leaving unmanaged files untouched.
func (m *Manager) Update(ctx context.Context, request UpdateRequest) (result []InstalledSkill, err error) {
	if err := m.destination(
		ctx,
		request.Directory,
	); err != nil {
		return nil, err
	}
	records, err := m.records(request.Directory)
	if err != nil {
		return nil, err
	}
	names, err := selectedNames(request.Skills)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		for name := range records.Skills {
			names = append(
				names,
				name,
			)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := records.Skills[name]; !ok {
			return nil, fmt.Errorf(
				"skill %q is not managed",
				name,
			)
		}
	}
	var changes []bundleChange
	for _, name := range names {
		filename := filepath.Join(
			request.Directory,
			name,
		)
		info, statErr := m.pathInfo(filename)
		if statErr != nil && !errors.Is(
			statErr,
			fs.ErrNotExist,
		) {
			return nil, statErr
		}
		if statErr == nil && (!info.IsDir() || info.Mode()&fs.ModeSymlink != 0) {
			return nil, fmt.Errorf(
				"%s is not a skill directory",
				filename,
			)
		}
		source := records.Skills[name]
		checkout, err := m.acquire(
			ctx,
			source.Location,
		)
		if err != nil {
			return nil, err
		}
		readErr := ensureSourceDirectory(
			checkout.FS,
			source.Directory,
		)
		var skill Skill
		if readErr == nil {
			skill, readErr = readDefinition(
				checkout.FS,
				source.Directory,
			)
		}
		var payload []payloadEntry
		if readErr == nil && skill.Name != name {
			readErr = fmt.Errorf(
				"source name %q no longer matches installed name %q",
				skill.Name,
				name,
			)
		}
		if readErr == nil {
			payload, readErr = readPayload(
				ctx,
				checkout.FS,
				source.Directory,
			)
		}
		if err := errors.Join(
			readErr,
			releaseCheckout(checkout),
		); err != nil {
			return nil, err
		}
		changes = append(
			changes,
			bundleChange{
				name:    name,
				payload: payload,
				action:  replaceBundle,
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
				Source:    &source,
			},
		)
	}
	if err := m.applyChanges(
		ctx,
		request.Directory,
		changes,
		records,
		true,
	); err != nil {
		if _, committed := errors.AsType[*CommittedCleanupError](err); !committed {
			result = nil
		}
		return result, fmt.Errorf(
			"update skills: %w",
			err,
		)
	}
	return result, nil
}

func (m *Manager) pathInfo(filename string) (fs.FileInfo, error) {
	if filesystem, ok := m.destinations.(afero.Lstater); ok {
		info, _, err := filesystem.LstatIfPossible(filename)
		return info, err
	}
	return m.destinations.Stat(filename)
}

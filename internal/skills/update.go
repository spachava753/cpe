package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/commandio"
)

// UpdateOptions selects installed skills and their result format.
type UpdateOptions struct {
	// Scope restricts inventory; its zero value searches project and global roots.
	Scope commandio.ScopeOptions
	// Skills contains installed directory names. Empty selects every installed skill.
	Skills []string
	// Override replaces bundles whose installed contents differ from tracking.
	Override bool
	// JSON forces JSON results even when output is a terminal.
	JSON bool
}

// Update replaces tracked skills from their recorded Git sources and writes results.
// Missing, ambiguous, untracked, and locally changed selections are warned about
// and skipped. Override permits content mismatches; each eligible source is fetched
// once. Results identify updates committed before a later scope or cleanup fails.
func Update(ctx context.Context, manager *Manager, opts UpdateOptions, streams commandio.IO) error {
	resolved, err := resolveScopes(
		opts.Scope,
		false,
	)
	if err != nil {
		return err
	}
	names, err := selectedNames(opts.Skills)
	if err != nil {
		return err
	}
	var scopes []scopeUpdate
	for _, scope := range resolved {
		installed, err := manager.installedNames(
			ctx,
			scope.directory,
			names,
		)
		if err != nil {
			return err
		}
		scopes = append(
			scopes,
			scopeUpdate{
				target: scope,
				names:  installed,
			},
		)
	}
	if len(names) != 0 {
		matches := map[string][]int{}
		for index, scope := range scopes {
			for _, name := range scope.names {
				matches[name] = append(
					matches[name],
					index,
				)
			}
			scopes[index].names = nil
		}
		for _, name := range names {
			switch found := matches[name]; len(found) {
			case 0:
				if err := writeWarning(
					streams,
					fmt.Sprintf(
						"skill %q does not exist in the selected scope",
						name,
					),
				); err != nil {
					return err
				}
			case 1:
				scopes[found[0]].names = append(
					scopes[found[0]].names,
					name,
				)
			default:
				if err := writeWarning(
					streams,
					fmt.Sprintf(
						"skill %q exists in project and global scope; provide an explicit --scope",
						name,
					),
				); err != nil {
					return err
				}
			}
		}
	}
	// Read selected locks before publishing any scope. Unselected inventories
	// remain independent, including when their lock files are damaged.
	for index, scope := range scopes {
		if len(scope.names) == 0 {
			continue
		}
		scopes[index].lock, err = manager.readLock(
			ctx,
			scope.target.lock,
		)
		if err != nil {
			return err
		}
	}
	if err := skipOverlappingUpdates(
		scopes,
		streams,
	); err != nil {
		return err
	}
	if err := manager.planUpdates(
		ctx,
		scopes,
		opts.Override,
		streams,
	); err != nil {
		return err
	}
	var result []InstalledSkill
	for _, scope := range scopes {
		if len(scope.updates) == 0 {
			continue
		}
		updated, err := manager.updateScope(
			ctx,
			scope,
		)
		result = append(
			result,
			updated...,
		)
		if err != nil {
			if len(result) == 0 {
				return err
			}
			return errors.Join(
				fmt.Errorf(
					"some skills updates committed: %w",
					err,
				),
				writeInstalled(
					streams,
					opts.JSON,
					result,
				),
			)
		}
	}
	return writeInstalled(
		streams,
		opts.JSON,
		result,
	)
}

func skipOverlappingUpdates(scopes []scopeUpdate, streams commandio.IO) error {
	for first := range scopes {
		scope := &scopes[first]
		if scope.lock == nil {
			continue
		}
		for second := first + 1; second < len(scopes); second++ {
			other := &scopes[second]
			if other.lock == nil {
				continue
			}
			if filepath.Clean(scope.target.directory) != filepath.Clean(other.target.directory) {
				continue
			}
			for _, name := range slices.Clone(scope.names) {
				if _, tracked := scope.lock.skills[name]; !tracked {
					continue
				}
				if _, tracked := other.lock.skills[name]; !tracked || !slices.Contains(
					other.names,
					name,
				) {
					continue
				}
				if err := writeWarning(
					streams,
					fmt.Sprintf(
						"cannot update skill %q: project and global tracking share one installed bundle; provide an explicit --scope",
						name,
					),
				); err != nil {
					return err
				}
				scope.names = slices.DeleteFunc(
					scope.names,
					func(candidate string) bool { return candidate == name },
				)
				other.names = slices.DeleteFunc(
					other.names,
					func(candidate string) bool { return candidate == name },
				)
			}
		}
	}
	return nil
}

// Inventory does not parse frontmatter: damaged or manually copied definitions
// must not stop warnings about untracked skills or updates to unrelated bundles.
func (m *Manager) installedNames(ctx context.Context, directory string, requested []string) ([]string, error) {
	if err := m.destination(
		ctx,
		directory,
	); err != nil {
		return nil, err
	}
	candidates := requested
	if len(candidates) == 0 {
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
		for _, entry := range entries {
			candidates = append(
				candidates,
				entry.Name(),
			)
		}
	}
	var names []string
	for _, name := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		filename := filepath.Join(
			directory,
			name,
		)
		info, err := m.destinations.Stat(filename)
		if errors.Is(
			err,
			fs.ErrNotExist,
		) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			continue
		}
		if _, err := bundlefs.Lstat(
			m.destinations,
			filepath.Join(
				filename,
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
		names = append(
			names,
			name,
		)
	}
	return names, nil
}

func (m *Manager) updateScope(ctx context.Context, scope scopeUpdate) ([]InstalledSkill, error) {
	var changes []bundlefs.Change
	var result []InstalledSkill
	for _, update := range scope.updates {
		bundle, err := update.source.bundle(
			ctx,
			m,
			update.record.SkillPath,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"update skill %q: %w",
				update.name,
				err,
			)
		}
		filename := filepath.Join(
			scope.target.directory,
			update.name,
		)
		record := scope.lock.hashRecord(
			bundle.skill,
			bundle.payload,
			update.record,
		)
		if err := scope.lock.put(
			update.name,
			record,
		); err != nil {
			return nil, err
		}
		changes = append(
			changes,
			bundlefs.Change{
				Name:    update.name,
				Payload: bundle.payload,
				Action:  bundlefs.Replace,
			},
		)
		skill := bundle.skill
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
	outcome, err := m.applyTracked(
		ctx,
		scope.target.directory,
		changes,
		scope.lock,
		m.checkDefinition,
	)
	if err != nil {
		if outcome != bundlefs.Committed {
			result = nil
		}
		return result, fmt.Errorf(
			"update skills: %w",
			err,
		)
	}
	return result, nil
}

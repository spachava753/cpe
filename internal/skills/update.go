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
	scopes, err := resolveScopes(
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
	locks := make(
		[]*skillLock,
		len(scopes),
	)
	inventory := make(
		[][]string,
		len(scopes),
	)
	for index, scope := range scopes {
		inventory[index], err = manager.installedNames(
			ctx,
			scope.directory,
			names,
		)
		if err != nil {
			return err
		}
	}
	selected := make(
		[][]string,
		len(scopes),
	)
	if len(names) == 0 {
		selected = inventory
	} else {
		matches := map[string][]int{}
		for index, installed := range inventory {
			for _, name := range installed {
				matches[name] = append(
					matches[name],
					index,
				)
			}
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
				selected[found[0]] = append(
					selected[found[0]],
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
		if len(selected[index]) == 0 {
			continue
		}
		locks[index], err = manager.readLock(
			ctx,
			scope.lock,
		)
		if err != nil {
			return err
		}
	}
	if err := skipOverlappingUpdates(
		scopes,
		locks,
		selected,
		streams,
	); err != nil {
		return err
	}
	plan, err := manager.planUpdates(
		ctx,
		scopes,
		locks,
		selected,
		opts.Override,
		streams,
	)
	if err != nil {
		return err
	}
	var result []InstalledSkill
	for index, scope := range scopes {
		if len(plan[index]) == 0 {
			continue
		}
		updated, err := manager.updateScope(
			ctx,
			scope,
			locks[index],
			plan[index],
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

func skipOverlappingUpdates(scopes []skillScope, locks []*skillLock, selected [][]string, streams commandio.IO) error {
	for first, scope := range scopes {
		if locks[first] == nil {
			continue
		}
		for second := first + 1; second < len(scopes); second++ {
			if locks[second] == nil {
				continue
			}
			if filepath.Clean(scope.directory) != filepath.Clean(scopes[second].directory) {
				continue
			}
			for _, name := range slices.Clone(selected[first]) {
				if _, tracked := locks[first].skills[name]; !tracked {
					continue
				}
				if _, tracked := locks[second].skills[name]; !tracked || !slices.Contains(
					selected[second],
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
				selected[first] = slices.DeleteFunc(
					selected[first],
					func(candidate string) bool { return candidate == name },
				)
				selected[second] = slices.DeleteFunc(
					selected[second],
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

func (m *Manager) updateScope(ctx context.Context, scope skillScope, lock *skillLock, updates []skillUpdate) ([]InstalledSkill, error) {
	var changes []bundlefs.Change
	var result []InstalledSkill
	for _, update := range updates {
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
			scope.directory,
			update.name,
		)
		if err := m.destination(
			ctx,
			filename,
		); err != nil {
			return nil, err
		}
		if err := m.checkDefinition(filename); err != nil {
			return nil, err
		}
		record := lock.hashRecord(
			bundle.skill,
			bundle.payload,
			update.record,
		)
		if err := lock.put(
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
		scope.directory,
		changes,
		lock,
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

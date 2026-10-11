package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"sort"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/source"
)

type skillUpdate struct {
	name   string
	record lockedSkill
	source *updateSource
}

type updateSource struct {
	locator  source.GitLocator
	paths    map[string]struct{}
	snapshot *updateSnapshot
}

type updateSnapshot struct {
	bundles map[string]updateBundle
	err     error
}

type updateBundle struct {
	skill   Skill
	payload []bundlefs.Entry
	err     error
}

// planUpdates checks every selected installation before any source is acquired.
func (m *Manager) planUpdates(ctx context.Context, scopes []skillScope, locks []*skillLock, selected [][]string, override bool, streams commandio.IO) ([][]skillUpdate, error) {
	plan := make(
		[][]skillUpdate,
		len(scopes),
	)
	sources := map[source.GitLocator]*updateSource{}
	for index, scope := range scopes {
		sort.Strings(selected[index])
		lock := locks[index]
		for _, name := range selected[index] {
			if _, exists := lock.skills[name]; !exists {
				if err := writeWarning(
					streams,
					fmt.Sprintf(
						"cannot update skill %q in %s: not in the lock file",
						name,
						scope.directory,
					),
				); err != nil {
					return nil, err
				}
				continue
			}
			record, err := lock.record(name)
			if err != nil {
				if err := writeWarning(
					streams,
					fmt.Sprintf(
						"cannot update skill %q in %s: %v",
						name,
						scope.directory,
						err,
					),
				); err != nil {
					return nil, err
				}
				continue
			}
			filename := filepath.Join(
				scope.directory,
				name,
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
			previous := lock.recordedHash(record)
			if !override {
				matches, err := m.matchesInstalled(
					ctx,
					filename,
					previous,
					streams,
				)
				if err != nil {
					return nil, err
				}
				if !matches {
					continue
				}
			}
			locator, err := source.ParseGitLocator(record.locator())
			if err != nil {
				return nil, err
			}
			acquisition := sources[locator]
			if acquisition == nil {
				acquisition = &updateSource{
					locator: locator,
					paths:   map[string]struct{}{},
				}
				sources[locator] = acquisition
			}
			acquisition.paths[record.SkillPath] = struct{}{}
			plan[index] = append(
				plan[index],
				skillUpdate{
					name:   name,
					record: record,
					source: acquisition,
				},
			)
		}
	}
	return plan, nil
}

func (m *Manager) installedHash(ctx context.Context, directory string) (string, error) {
	filesystem := afero.NewIOFS(afero.NewBasePathFs(
		m.destinations,
		directory,
	))
	payload, err := collectPayload(
		ctx,
		filesystem,
		".",
		installedPayload,
	)
	if err != nil {
		return "", err
	}
	return folderHash(payload), nil
}

func (m *Manager) matchesInstalled(ctx context.Context, directory string, previous string, streams commandio.IO) (bool, error) {
	hash, err := m.installedHash(
		ctx,
		directory,
	)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		return false, writeWarning(
			streams,
			fmt.Sprintf(
				"cannot update skill in %s: cannot verify installed contents: %v; use --override to replace it",
				directory,
				err,
			),
		)
	}
	if hash != previous {
		return false, writeWarning(
			streams,
			fmt.Sprintf(
				"cannot update skill in %s: installed contents do not match the lock hash (local edits or incompatible tracking); use --override to replace it",
				directory,
			),
		)
	}
	return true, nil
}

// bundle retains owned payloads, not checkouts. Cleanup completes before publication.
func (acquisition *updateSource) bundle(ctx context.Context, manager *Manager, skillPath string) (updateBundle, error) {
	if acquisition.snapshot == nil {
		acquisition.snapshot = &updateSnapshot{
			bundles: map[string]updateBundle{},
		}
		checkout, err := manager.acquire(
			ctx,
			acquisition.locator.String(),
		)
		if err != nil {
			acquisition.snapshot.err = err
		} else {
			for _, filename := range slices.Sorted(maps.Keys(acquisition.paths)) {
				if err := ctx.Err(); err != nil {
					acquisition.snapshot.err = err
					break
				}
				acquisition.snapshot.bundles[filename] = readUpdateBundle(
					ctx,
					checkout.FS,
					filename,
				)
			}
			if err := checkout.Close(); err != nil {
				acquisition.snapshot.err = errors.Join(
					acquisition.snapshot.err,
					fmt.Errorf(
						"release source: %w",
						err,
					),
				)
			}
		}
	}
	if acquisition.snapshot.err != nil {
		return updateBundle{}, acquisition.snapshot.err
	}
	bundle, exists := acquisition.snapshot.bundles[skillPath]
	if !exists {
		return updateBundle{}, errors.New("skill is absent from the planned source snapshot")
	}
	return bundle, bundle.err
}

func readUpdateBundle(ctx context.Context, filesystem fs.FS, skillPath string) updateBundle {
	directory := path.Dir(skillPath)
	if err := bundlefs.CheckSourceDirectory(
		filesystem,
		directory,
	); err != nil {
		return updateBundle{
			err: err,
		}
	}
	skill, err := readDefinition(
		filesystem,
		directory,
	)
	if err != nil {
		return updateBundle{
			err: err,
		}
	}
	payload, err := readPayload(
		ctx,
		filesystem,
		directory,
	)
	return updateBundle{
		skill:   skill,
		payload: payload,
		err:     err,
	}
}

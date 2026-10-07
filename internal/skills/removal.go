package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spachava753/cpe/internal/bundlefs"
)

type removalPlan struct {
	changes []bundlefs.Change
	paths   []string
}

// All eligibility checks happen before a removal can mutate.
func (m *Manager) prepareRemoval(ctx context.Context, request RemoveRequest) (removalPlan, error) {
	if err := m.destination(
		ctx,
		request.Directory,
	); err != nil {
		return removalPlan{}, err
	}
	names, err := selectedNames(request.Skills)
	if err != nil {
		return removalPlan{}, err
	}
	if len(names) == 0 {
		return removalPlan{}, errors.New("select at least one skill to remove")
	}
	plan := removalPlan{}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return removalPlan{}, err
		}
		filename := filepath.Join(
			request.Directory,
			name,
		)
		info, err := bundlefs.Lstat(
			m.destinations,
			filename,
		)
		if err != nil {
			return removalPlan{}, fmt.Errorf(
				"remove %s: %w",
				filename,
				err,
			)
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return removalPlan{}, fmt.Errorf(
				"%s is not a skill directory",
				filename,
			)
		}
		if err := m.checkDefinition(filename); err != nil {
			return removalPlan{}, fmt.Errorf(
				"%s is not a skill directory: %w",
				filename,
				err,
			)
		}
		plan.changes = append(
			plan.changes,
			bundlefs.Change{
				Name:   name,
				Action: bundlefs.Remove,
			},
		)
		plan.paths = append(
			plan.paths,
			filename,
		)
	}
	return plan, nil
}

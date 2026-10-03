package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

type removalPlan struct {
	changes      []bundleChange
	paths        []string
	records      installationRecords
	writeRecords bool
}

// All eligibility and bookkeeping checks happen before a removal can mutate.
func (m *Manager) prepareRemoval(ctx context.Context, request RemoveRequest) (removalPlan, error) {
	if err := m.destination(ctx, request.Directory); err != nil {
		return removalPlan{}, err
	}
	names, err := selectedNames(request.Skills)
	if err != nil {
		return removalPlan{}, err
	}
	if len(names) == 0 {
		return removalPlan{}, errors.New("select at least one skill to remove")
	}
	records, err := m.records(request.Directory)
	if err != nil {
		return removalPlan{}, err
	}
	plan := removalPlan{records: records}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return removalPlan{}, err
		}
		filename := filepath.Join(request.Directory, name)
		info, err := m.pathInfo(filename)
		if err != nil {
			return removalPlan{}, fmt.Errorf("remove %s: %w", filename, err)
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return removalPlan{}, fmt.Errorf("%s is not a skill directory", filename)
		}
		if _, ok := records.Skills[name]; ok {
			delete(records.Skills, name)
			plan.writeRecords = true
		} else {
			definition, err := m.pathInfo(filepath.Join(filename, "SKILL.md"))
			if err != nil {
				return removalPlan{}, fmt.Errorf("%s is not a skill directory: %w", filename, err)
			}
			if !definition.Mode().IsRegular() {
				return removalPlan{}, fmt.Errorf("%s is not a skill directory with a regular SKILL.md", filename)
			}
		}
		plan.changes = append(plan.changes, bundleChange{name: name, action: removeBundle})
		plan.paths = append(plan.paths, filename)
	}
	return plan, nil
}

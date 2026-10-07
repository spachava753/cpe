package skills

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func (m *Manager) destination(ctx context.Context, directory string) error {
	var filesystem afero.Fs
	if m != nil {
		filesystem = m.destinations
	}
	return bundlefs.CheckDestination(
		ctx,
		filesystem,
		directory,
	)
}

// Eligibility is domain policy, rechecked by the transaction before publication.
// Damaged frontmatter does not prevent replacing or removing a regular definition.
func (m *Manager) checkDefinition(directory string) error {
	definition, err := bundlefs.Lstat(
		m.destinations,
		filepath.Join(
			directory,
			"SKILL.md",
		),
	)
	if err != nil {
		return err
	}
	if !definition.Mode().IsRegular() {
		return fmt.Errorf(
			"destination %s must contain a regular SKILL.md",
			directory,
		)
	}
	return nil
}

func selectedNames(names []string) ([]string, error) {
	seen := map[string]bool{}
	result := make(
		[]string,
		0,
		len(names),
	)
	for _, name := range names {
		if !bundlefs.ValidDirectoryName(name) {
			return nil, fmt.Errorf(
				"invalid skill directory name %q",
				name,
			)
		}
		if !seen[name] {
			seen[name] = true
			result = append(
				result,
				name,
			)
		}
	}
	return result, nil
}

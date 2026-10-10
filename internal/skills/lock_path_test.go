package skills_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/skills"
)

func TestSkillLockPathSafety(t *testing.T) {
	root := t.TempDir()
	safe := filepath.Join(
		root,
		"safe",
	)
	foreign := filepath.Join(
		root,
		"foreign",
	)
	for _, directory := range []string{
		filepath.Join(
			safe,
			"outside",
		),
		filepath.Join(
			foreign,
			"child",
		),
		filepath.Join(
			foreign,
			"outside",
		),
	} {
		if err := os.MkdirAll(
			directory,
			0o700,
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(
		filepath.Join(
			foreign,
			"child",
		),
		filepath.Join(
			safe,
			"link",
		),
	); err != nil {
		t.Skipf(
			"symlink unavailable: %v",
			err,
		)
	}
	const initial = `{"version":1,"skills":{"foreign":{"opaque":true}}}`
	filename := filepath.Join(
		foreign,
		"outside",
		"skills-lock.json",
	)
	if err := os.WriteFile(
		filename,
		[]byte(initial),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	manager := skills.NewManager(
		managerSourceClient{
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
		},
		afero.NewOsFs(),
	)
	unsafePath := safe + string(filepath.Separator) + "link" + string(filepath.Separator) + ".." + string(filepath.Separator) + "outside" + string(filepath.Separator) + "skills-lock.json"
	installed, err := manager.Install(
		t.Context(),
		skills.InstallRequest{
			Source: managerSource,
			Directory: filepath.Join(
				safe,
				"skills",
			),
			Lock: &skills.LockFile{
				Path:   unsafePath,
				Format: skills.ProjectLock,
			},
		},
	)
	if err == nil || len(installed) != 0 || !strings.Contains(
		err.Error(),
		"symlink",
	) {
		t.Fatalf(
			"Install = %v, %v; uncleaned lock path must be rejected",
			installed,
			err,
		)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != initial {
		t.Fatalf(
			"foreign lock changed: %s, %v",
			data,
			err,
		)
	}
}

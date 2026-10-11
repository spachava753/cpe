package skills_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/skills"
)

func TestConcurrentUntrackedRemoval(t *testing.T) {
	for _, format := range []skills.LockFormat{
		skills.ProjectLock,
		skills.GlobalLock,
	} {
		for _, native := range []bool{
			false,
			true,
		} {
			for _, existing := range []bool{
				false,
				true,
			} {
				t.Run(
					fmt.Sprintf(
						"format=%v/native=%v/existing=%v",
						format,
						native,
						existing,
					),
					func(t *testing.T) {
						base := t.TempDir()
						root := filepath.Join(
							base,
							"skills",
						)
						filename := filepath.Join(
							base,
							"lock.json",
						)
						definition := filepath.Join(
							root,
							"alpha",
							"SKILL.md",
						)
						filesystem := afero.NewMemMapFs()
						if native {
							filesystem = afero.NewOsFs()
						}
						writeTrackingFixture(
							t,
							filesystem,
							definition,
							alphaSkill,
						)
						if existing {
							version := 1
							if format == skills.GlobalLock {
								version = 3
							}
							writeTrackingFixture(
								t,
								filesystem,
								filename,
								fmt.Sprintf(
									`{"version":%d,"skills":{"foreign":{"opaque":true}}}`,
									version,
								),
							)
						}
						probe := &removalInterleaveFS{
							Fs:         filesystem,
							definition: definition,
						}
						manager := skills.NewManager(
							managerSourceClient{
								files: fstest.MapFS{
									"alpha/SKILL.md": {
										Data: []byte(alphaSkill),
									},
								},
							},
							probe,
						)
						lock := &skills.LockFile{
							Path:   filename,
							Format: format,
						}
						probe.interleave = func() error {
							installed, err := manager.Install(
								t.Context(),
								skills.InstallRequest{
									Source:    managerSource,
									Directory: root,
									Override:  true,
									Lock:      lock,
								},
							)
							if err == nil && len(installed) != 1 {
								return fmt.Errorf(
									"competing install returned %d results",
									len(installed),
								)
							}
							return err
						}
						removed, err := manager.Remove(
							t.Context(),
							skills.RemoveRequest{
								Directory: root,
								Skills: []string{
									"alpha",
								},
								Lock: lock,
							},
						)
						if err == nil || len(removed) != 0 || !strings.Contains(
							err.Error(),
							"changed since it was read",
						) {
							t.Fatalf(
								"Remove = %v, %v; stale removal must abort",
								removed,
								err,
							)
						}
						if exists, statErr := afero.Exists(
							filesystem,
							definition,
						); statErr != nil || !exists {
							t.Fatalf(
								"committed installation was removed: %v, %v",
								exists,
								statErr,
							)
						}
						data, readErr := afero.ReadFile(
							filesystem,
							filename,
						)
						if readErr != nil {
							t.Fatal(readErr)
						}
						var document struct {
							Skills map[string]json.RawMessage `json:"skills"`
						}
						if err := json.Unmarshal(
							data,
							&document,
						); err != nil {
							t.Fatal(err)
						}
						if document.Skills["alpha"] == nil {
							t.Fatalf(
								"committed record was lost: %s",
								data,
							)
						}
					},
				)
			}
		}
	}
}

type removalInterleaveFS struct {
	afero.Fs
	definition string
	interleave func() error
}

func (filesystem *removalInterleaveFS) LstatIfPossible(filename string) (fs.FileInfo, bool, error) {
	if filesystem, ok := filesystem.Fs.(afero.Lstater); ok {
		return filesystem.LstatIfPossible(filename)
	}
	info, err := filesystem.Stat(filename)
	return info, false, err
}

func (filesystem *removalInterleaveFS) Open(filename string) (afero.File, error) {
	if filename == filesystem.definition && filesystem.interleave != nil {
		interleave := filesystem.interleave
		filesystem.interleave = nil
		if err := interleave(); err != nil {
			return nil, err
		}
	}
	return filesystem.Fs.Open(filename)
}

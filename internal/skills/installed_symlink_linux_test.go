//go:build linux

package skills_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/skills"
)

func TestInstalledDirectoryAncestry(t *testing.T) {
	for _, test := range []struct {
		name     string
		captured bool
		internal bool
	}{
		{
			name: "outside ancestor symlink is rejected before acquisition",
		},
		{
			name:     "outside ancestor symlink in captured bundle is restored",
			captured: true,
		},
		{
			name:     "internal ancestor symlink is also a local edit",
			internal: true,
		},
		{
			name:     "internal ancestor symlink in captured bundle is restored",
			internal: true,
			captured: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				fixture := t.TempDir()
				root := filepath.Join(
					fixture,
					"skills",
				)
				bundle := filepath.Join(
					root,
					"alpha",
				)
				assets := filepath.Join(
					bundle,
					"assets",
				)
				target := filepath.Join(
					fixture,
					"foreign",
				)
				base := afero.NewOsFs()
				if err := base.MkdirAll(
					target,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := afero.WriteFile(
					base,
					filepath.Join(
						target,
						"note.txt",
					),
					[]byte("original asset"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				if test.internal {
					target = filepath.Join(
						bundle,
						"mirror",
					)
				}
				filesystem := &installedAncestorFilesystem{
					Fs:     base,
					assets: assets,
					target: target,
					retained: filepath.Join(
						fixture,
						"retained-assets",
					),
					captured: test.captured,
					internal: test.internal,
				}
				var fetches []string
				manager := skills.NewManager(
					updateSourceClient{
						files: fstest.MapFS{
							"alpha/SKILL.md": {
								Data: []byte(alphaSkill),
							},
							"alpha/assets/note.txt": {
								Data: []byte("original asset"),
							},
							"alpha/mirror/note.txt": {
								Data: []byte("original asset"),
							},
						},
						fetches:     &fetches,
						beforeFetch: func() {},
						cleanup:     func() error { return nil },
					},
					filesystem,
				)
				lockPath := filepath.Join(
					root,
					"skills-lock.json",
				)
				if _, err := manager.Install(
					t.Context(),
					skills.InstallRequest{
						Source:    managerSource,
						Directory: root,
						Lock: &skills.LockFile{
							Path:   lockPath,
							Format: skills.ProjectLock,
						},
					},
				); err != nil {
					t.Fatal(err)
				}
				beforeLock, err := afero.ReadFile(
					base,
					lockPath,
				)
				if err != nil {
					t.Fatal(err)
				}
				fetches = nil
				filesystem.armed = true
				var output, warnings bytes.Buffer
				err = skills.Update(
					t.Context(),
					manager,
					skills.UpdateOptions{
						Scope: commandio.ScopeOptions{
							Directory: &root,
						},
					},
					commandio.IO{
						Out: &output,
						Err: &warnings,
					},
				)
				if !filesystem.swapped {
					t.Fatal("late ancestor replacement hook did not run")
				}
				if test.captured {
					if err == nil || len(fetches) != 1 {
						t.Fatalf(
							"captured symlink was not rejected: %v, acquisitions=%v",
							err,
							fetches,
						)
					}
				} else if err != nil || len(fetches) != 0 || !strings.Contains(
					warnings.String(),
					"cannot verify installed contents",
				) {
					t.Fatalf(
						"unverifiable installation was not skipped before acquisition: %v, acquisitions=%v, warnings=%s",
						err,
						fetches,
						warnings.String(),
					)
				}
				info, err := os.Lstat(assets)
				if err != nil || info.Mode()&fs.ModeSymlink == 0 {
					t.Fatal("local ancestor symlink was lost")
				}
				afterLock, err := afero.ReadFile(
					base,
					lockPath,
				)
				if err != nil || !bytes.Equal(
					beforeLock,
					afterLock,
				) {
					t.Fatal("rejected update changed tracking")
				}
				data, err := afero.ReadFile(
					base,
					filepath.Join(
						fixture,
						"foreign",
						"note.txt",
					),
				)
				if err != nil || string(data) != "original asset" {
					t.Fatal("outside resource was changed")
				}
			},
		)
	}
}

type installedAncestorFilesystem struct {
	afero.Fs
	assets   string
	target   string
	retained string
	captured bool
	internal bool
	armed    bool
	swapped  bool
}

func (filesystem *installedAncestorFilesystem) OpenFileAt(parent afero.File, name string, flags int, _ fs.FileMode) (afero.File, error) {
	file, err := openInstalledTestFileAt(
		parent,
		name,
		flags,
	)
	if err != nil {
		return nil, err
	}
	filename := filepath.Join(
		parent.Name(),
		name,
	)
	captured := strings.Contains(
		filename,
		".cpe-stage-",
	) && filepath.Base(filepath.Dir(filepath.Dir(filename))) == "old"
	if filesystem.armed && name == "assets" && filesystem.captured == captured {
		filesystem.assets = filename
		return installedAncestorFile{
			File:       file,
			filesystem: filesystem,
		}, nil
	}
	return file, nil
}

type installedAncestorFile struct {
	afero.File
	filesystem *installedAncestorFilesystem
}

func (file installedAncestorFile) Fd() uintptr {
	return file.File.(interface{ Fd() uintptr }).Fd()
}

func (file installedAncestorFile) Readdir(count int) ([]fs.FileInfo, error) {
	infos, err := file.File.Readdir(count)
	if err != nil {
		return infos, err
	}
	filesystem := file.filesystem
	if err := filesystem.Rename(
		filesystem.assets,
		filesystem.retained,
	); err != nil {
		return nil, err
	}
	target := filesystem.target
	if filesystem.internal {
		target = filepath.Join(
			filepath.Dir(filesystem.assets),
			"mirror",
		)
	}
	if err := os.Symlink(
		target,
		filesystem.assets,
	); err != nil {
		return nil, err
	}
	filesystem.armed = false
	filesystem.swapped = true
	return infos, nil
}

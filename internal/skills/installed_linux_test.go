//go:build linux

package skills_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/skills"
)

func TestInstalledHashLateFIFO(t *testing.T) {
	for _, test := range []struct {
		name      string
		directory bool
	}{
		{
			name: "resource replaced after traversal metadata",
		},
		{
			name:      "bundle replaced after eligibility checks",
			directory: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				root := filepath.Join(
					t.TempDir(),
					"skills",
				)
				bundle := filepath.Join(
					root,
					"alpha",
				)
				resource := filepath.Join(
					bundle,
					"note.txt",
				)
				if test.directory {
					resource = bundle
				}
				base := afero.NewOsFs()
				filesystem := &installedFIFOFilesystem{
					Fs:        base,
					resource:  resource,
					triggered: make(chan struct{}),
				}
				var fetches []string
				manager := skills.NewManager(
					updateSourceClient{
						files: fstest.MapFS{
							"alpha/SKILL.md": {
								Data: []byte(alphaSkill),
							},
							"alpha/note.txt": {
								Data: []byte("original note"),
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
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var output, warnings bytes.Buffer
				done := make(
					chan error,
					1,
				)
				go func() {
					done <- skills.Update(
						ctx,
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
				}()
				select {
				case <-filesystem.triggered:
				case <-time.After(time.Second):
					t.Fatal("late FIFO replacement hook did not run")
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					cancel()
					// Release a defective blocking implementation before failing the test.
					writer, err := unix.Open(
						resource,
						unix.O_WRONLY|unix.O_NONBLOCK,
						0,
					)
					if err != nil {
						t.Fatal(err)
					}
					if err := unix.Close(writer); err != nil {
						t.Fatal(err)
					}
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Fatal("verification did not stop after its FIFO reader was released")
					}
					t.Fatal("verification waited for a FIFO writer")
				}
				if len(fetches) != 0 || !strings.Contains(
					warnings.String(),
					"cannot verify installed contents",
				) {
					t.Fatalf(
						"unverifiable payload was not skipped: acquisitions=%v; warnings=%s",
						fetches,
						warnings.String(),
					)
				}
				if filesystem.flags&unix.O_NONBLOCK == 0 {
					t.Fatal("installed file was not opened nonblocking")
				}
				afterLock, err := afero.ReadFile(
					base,
					lockPath,
				)
				if err != nil || !bytes.Equal(
					beforeLock,
					afterLock,
				) {
					t.Fatal("skipped update changed tracking")
				}
				info, err := base.Stat(resource)
				if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
					t.Fatal("local FIFO replacement was lost")
				}
			},
		)
	}
}

type installedFIFOFilesystem struct {
	afero.Fs
	resource  string
	armed     bool
	flags     int
	triggered chan struct{}
}

func (filesystem *installedFIFOFilesystem) OpenFileAt(parent afero.File, name string, flags int, _ os.FileMode) (afero.File, error) {
	filename := filepath.Join(
		parent.Name(),
		name,
	)
	if filesystem.armed && filename == filesystem.resource {
		filesystem.armed = false
		filesystem.flags = flags
		if err := filesystem.RemoveAll(filename); err != nil {
			return nil, err
		}
		if err := unix.Mkfifo(
			filename,
			0o600,
		); err != nil {
			return nil, err
		}
		close(filesystem.triggered)
	}
	return openInstalledTestFileAt(
		parent,
		name,
		flags,
	)
}

func openInstalledTestFileAt(parent afero.File, name string, flags int) (afero.File, error) {
	descriptor, err := unix.Openat(
		int(parent.(interface{ Fd() uintptr }).Fd()),
		name,
		flags|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(
		uintptr(descriptor),
		filepath.Join(
			parent.Name(),
			name,
		),
	), nil
}

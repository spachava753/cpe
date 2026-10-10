package skills_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/skills"
)

func TestConcurrentSkillTracking(t *testing.T) {
	for _, test := range []struct {
		name     string
		format   skills.LockFormat
		native   bool
		existing bool
	}{
		{
			name:   "project first publication in memory",
			format: skills.ProjectLock,
		},
		{
			name:     "project replacement in memory",
			format:   skills.ProjectLock,
			existing: true,
		},
		{
			name:   "global first publication in memory",
			format: skills.GlobalLock,
		},
		{
			name:     "global replacement in memory",
			format:   skills.GlobalLock,
			existing: true,
		},
		{
			name:   "project first publication on native filesystem",
			format: skills.ProjectLock,
			native: true,
		},
		{
			name:     "project replacement on native filesystem",
			format:   skills.ProjectLock,
			native:   true,
			existing: true,
		},
		{
			name:   "global first publication on native filesystem",
			format: skills.GlobalLock,
			native: true,
		},
		{
			name:     "global replacement on native filesystem",
			format:   skills.GlobalLock,
			native:   true,
			existing: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				ctx, cancel := context.WithTimeout(
					t.Context(),
					5*time.Second,
				)
				defer cancel()
				root := t.TempDir()
				filename := filepath.Join(
					root,
					"skills-lock.json",
				)
				version := 1
				if test.format == skills.GlobalLock {
					filename = filepath.Join(
						root,
						".agents",
						".skill-lock.json",
					)
					version = 3
				}
				filesystem := afero.NewMemMapFs()
				if test.native {
					filesystem = afero.NewOsFs()
				}
				if err := filesystem.MkdirAll(
					root,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if test.existing {
					initial, err := json.Marshal(map[string]any{
						"version": version,
						"skills": map[string]any{
							"foreign": map[string]any{
								"opaque": true,
							},
						},
					})
					if err != nil {
						t.Fatal(err)
					}
					writeTrackingFixture(
						t,
						filesystem,
						filename,
						string(initial),
					)
				}
				probe := &trackingPublicationFS{
					Fs:       filesystem,
					filename: filename,
					ctx:      ctx,
					entered:  make(chan struct{}),
					resume:   make(chan struct{}),
					attempted: make(
						chan struct{},
						1,
					),
				}
				manager := skills.NewManager(
					managerSourceClient{
						files: fstest.MapFS{
							"alpha/SKILL.md": {
								Data: []byte(alphaSkill),
							},
							"beta/SKILL.md": {
								Data: []byte(betaSkill),
							},
						},
						cleanup: func() error {
							probe.ready.Store(true)
							return nil
						},
					},
					probe,
				)
				lock := &skills.LockFile{
					Path:   filename,
					Format: test.format,
				}
				directory := filepath.Join(
					root,
					".agents",
					"skills",
				)
				type result struct {
					installed []skills.InstalledSkill
					err       error
				}
				alpha := make(
					chan result,
					1,
				)
				beta := make(
					chan result,
					1,
				)
				go func() {
					installed, err := manager.Install(
						ctx,
						skills.InstallRequest{
							Source:    managerSource,
							Directory: directory,
							Skills: []string{
								"alpha",
							},
							Lock: lock,
						},
					)
					alpha <- result{
						installed: installed,
						err:       err,
					}
				}()
				select {
				case <-probe.entered:
				case <-ctx.Done():
					t.Fatal("first publication did not reach the controlled interleaving")
				}
				go func() {
					installed, err := manager.Install(
						ctx,
						skills.InstallRequest{
							Source:    managerSource,
							Directory: directory,
							Skills: []string{
								"beta",
							},
							Lock: lock,
						},
					)
					beta <- result{
						installed: installed,
						err:       err,
					}
				}()
				select {
				case <-probe.attempted:
				case <-ctx.Done():
					t.Fatal("second publication did not attempt the held guard")
				}
				close(probe.resume)
				first, second := <-alpha, <-beta
				if first.err != nil || len(first.installed) != 1 {
					t.Fatalf(
						"first install = %v, %v",
						first.installed,
						first.err,
					)
				}
				if test.native {
					if second.err != nil || len(second.installed) != 1 {
						t.Fatalf(
							"serialized install = %v, %v",
							second.installed,
							second.err,
						)
					}
				} else if second.err == nil || len(second.installed) != 0 {
					t.Fatalf(
						"conflicting memory publication = %v, %v",
						second.installed,
						second.err,
					)
				}
				data, err := afero.ReadFile(
					filesystem,
					filename,
				)
				if err != nil {
					t.Fatal(err)
				}
				var document struct {
					Version int                        `json:"version"`
					Skills  map[string]json.RawMessage `json:"skills"`
				}
				if err := json.Unmarshal(
					data,
					&document,
				); err != nil {
					t.Fatal(err)
				}
				if document.Version != version || document.Skills["alpha"] == nil || (document.Skills["beta"] != nil) != test.native {
					t.Fatalf(
						"committed records were lost: %s",
						data,
					)
				}
				if test.existing && document.Skills["foreign"] == nil {
					t.Fatal("unrelated record was discarded")
				}
				if exists, err := afero.Exists(
					filesystem,
					filepath.Join(
						directory,
						"beta",
						"SKILL.md",
					),
				); err != nil || exists != test.native {
					t.Fatalf(
						"second bundle exists = %v, %v",
						exists,
						err,
					)
				}
			},
		)
	}
}

type trackingPublicationFS struct {
	afero.Fs
	filename   string
	ctx        context.Context
	ready      atomic.Bool
	publishing atomic.Bool
	once       sync.Once
	entered    chan struct{}
	resume     chan struct{}
	attempted  chan struct{}
}

func (filesystem *trackingPublicationFS) LstatIfPossible(filename string) (fs.FileInfo, bool, error) {
	if filename == filesystem.filename && filesystem.ready.Load() && !filesystem.publishing.Load() {
		filesystem.once.Do(func() {
			filesystem.publishing.Store(true)
			close(filesystem.entered)
			select {
			case <-filesystem.resume:
			case <-filesystem.ctx.Done():
			}
		})
	}
	if filesystem, ok := filesystem.Fs.(afero.Lstater); ok {
		return filesystem.LstatIfPossible(filename)
	}
	info, err := filesystem.Stat(filename)
	return info, false, err
}

func (filesystem *trackingPublicationFS) Open(filename string) (afero.File, error) {
	if filename == filesystem.filename+".cpe-lock" && filesystem.publishing.Load() {
		select {
		case filesystem.attempted <- struct{}{}:
		default:
		}
	}
	return filesystem.Fs.Open(filename)
}

func (filesystem *trackingPublicationFS) OpenFile(filename string, flags int, mode fs.FileMode) (afero.File, error) {
	if filename == filesystem.filename+".cpe-lock" && flags == os.O_RDWR && filesystem.publishing.Load() {
		select {
		case filesystem.attempted <- struct{}{}:
		default:
		}
	}
	return filesystem.Fs.OpenFile(
		filename,
		flags,
		mode,
	)
}

package bundlefs_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func TestApplyWithFile(t *testing.T) {
	for _, test := range []struct {
		name        string
		fault       fileFault
		missing     bool
		inside      bool
		empty       bool
		wantOutcome bundlefs.Outcome
		wantError   bool
		retained    bool
	}{
		{
			name:        "replace bundle and lock",
			wantOutcome: bundlefs.Committed,
		},
		{
			name:        "create bundle and lock with missing parents",
			missing:     true,
			wantOutcome: bundlefs.Committed,
		},
		{
			name:      "file staging failure changes nothing",
			fault:     fileStageFailure,
			wantError: true,
		},
		{
			name:      "file backup failure restores published bundle",
			fault:     fileBackupFailure,
			wantError: true,
		},
		{
			name:      "file publication failure restores lock and bundle",
			fault:     filePublishFailure,
			wantError: true,
		},
		{
			name:      "failed first publication removes new ancestors",
			missing:   true,
			fault:     filePublishFailure,
			wantError: true,
		},
		{
			name:      "cancellation after file publication restores both",
			fault:     fileCancelPublication,
			wantError: true,
		},
		{
			name:        "backup cleanup failure reports committed state",
			fault:       fileCleanupFailure,
			wantOutcome: bundlefs.Committed,
			wantError:   true,
			retained:    true,
		},
		{
			name:      "failed lock rollback retains original lock",
			fault:     fileRestoreFailure,
			wantError: true,
			retained:  true,
		},
		{
			name:      "failed first lock rollback identifies the surviving published file",
			missing:   true,
			fault:     fileCancelRollbackFailure,
			wantError: true,
		},
		{
			name:      "file cannot overlap bundle payload",
			inside:    true,
			wantError: true,
		},
		{
			name:        "no changes leave lock untouched",
			empty:       true,
			wantOutcome: bundlefs.Committed,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				root := filepath.FromSlash("/project/.agents/skills")
				filename := filepath.FromSlash("/state/skills/.skill-lock.json")
				marker := filepath.Join(
					root,
					"alpha",
					"marker",
				)
				if !test.missing {
					for name, content := range map[string]string{
						marker:   "old bundle",
						filename: "old lock",
					} {
						if err := filesystem.MkdirAll(
							filepath.Dir(name),
							0o700,
						); err != nil {
							t.Fatal(err)
						}
						if err := afero.WriteFile(
							filesystem,
							name,
							[]byte(content),
							0o600,
						); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := fileSnapshot(
					t,
					filesystem,
				)
				if test.inside {
					filename = filepath.Join(
						root,
						"alpha",
						"lock.json",
					)
				}
				changes := []bundlefs.Change{
					{
						Name:   "alpha",
						Action: bundlefs.Replace,
						Payload: []bundlefs.Entry{
							{
								Path: "marker",
								Data: []byte("new bundle"),
							},
						},
					},
				}
				if test.empty {
					changes = nil
				}
				var previous []byte
				if !test.missing {
					previous = []byte("old lock")
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				outcome, err := bundlefs.ApplyWithFile(
					ctx,
					fileFaultFS{
						Fs:     filesystem,
						fault:  test.fault,
						cancel: cancel,
					},
					root,
					changes,
					func(string) error { return nil },
					bundlefs.FileChange{
						Path:     filename,
						Data:     []byte("new lock"),
						Previous: previous,
					},
				)
				if outcome != test.wantOutcome || (err != nil) != test.wantError {
					t.Fatalf(
						"ApplyWithFile = %v, %v",
						outcome,
						err,
					)
				}
				after := fileSnapshot(
					t,
					filesystem,
				)
				if test.retained {
					found := false
					for filename, data := range after {
						if strings.HasPrefix(
							filepath.Base(filename),
							".cpe-backup-",
						) {
							found = data == "old lock"
							delete(
								after,
								filename,
							)
						}
					}
					if !found {
						t.Fatal("original lock recovery file was discarded")
					}
				}
				if outcome == bundlefs.Committed && !test.empty {
					if after[marker] != "new bundle" || after[filename] != "new lock" {
						t.Fatalf(
							"committed state = %v",
							after,
						)
					}
				} else if test.fault == fileCancelRollbackFailure {
					if after[filename] != "new lock" || !strings.Contains(
						err.Error(),
						"published file remains at "+filename,
					) {
						t.Fatalf(
							"failed first rollback did not identify surviving lock: %v, %v",
							after,
							err,
						)
					}
				} else if test.fault == fileRestoreFailure {
					if after[marker] != "old bundle" || after[filename] != "" || !strings.Contains(
						err.Error(),
						"recovery file retained",
					) {
						t.Fatalf(
							"failed rollback state = %v, %v",
							after,
							err,
						)
					}
				} else if diff := cmp.Diff(
					before,
					after,
				); diff != "" {
					t.Fatal(diff)
				}
			},
		)
	}
}

type fileFault uint8

const (
	fileNoFault fileFault = iota
	fileStageFailure
	fileBackupFailure
	filePublishFailure
	fileCancelPublication
	fileCleanupFailure
	fileRestoreFailure
	fileCancelRollbackFailure
)

type fileFaultFS struct {
	afero.Fs
	fault  fileFault
	cancel context.CancelFunc
}

func (filesystem fileFaultFS) OpenFile(name string, flag int, mode fs.FileMode) (afero.File, error) {
	if filesystem.fault == fileStageFailure && strings.HasPrefix(
		filepath.Base(name),
		".cpe-file-",
	) {
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.OpenFile(
		name,
		flag,
		mode,
	)
}

func (filesystem fileFaultFS) Rename(oldname string, newname string) error {
	newBase := filepath.Base(newname)
	oldBase := filepath.Base(oldname)
	if filesystem.fault == fileBackupFailure && strings.HasPrefix(
		newBase,
		".cpe-backup-",
	) {
		return fs.ErrPermission
	}
	if (filesystem.fault == filePublishFailure || filesystem.fault == fileRestoreFailure) && strings.HasPrefix(
		oldBase,
		".cpe-file-",
	) {
		return fs.ErrPermission
	}
	if filesystem.fault == fileRestoreFailure && strings.HasPrefix(
		oldBase,
		".cpe-backup-",
	) {
		return fs.ErrPermission
	}
	err := filesystem.Fs.Rename(
		oldname,
		newname,
	)
	if (filesystem.fault == fileCancelPublication || filesystem.fault == fileCancelRollbackFailure) && strings.HasPrefix(
		oldBase,
		".cpe-file-",
	) {
		filesystem.cancel()
	}
	return err
}

func (filesystem fileFaultFS) Remove(name string) error {
	if filesystem.fault == fileCancelRollbackFailure && filepath.Base(name) == ".skill-lock.json" {
		return fs.ErrPermission
	}
	if filesystem.fault == fileCleanupFailure && strings.HasPrefix(
		filepath.Base(name),
		".cpe-backup-",
	) {
		data, err := afero.ReadFile(
			filesystem.Fs,
			name,
		)
		if err == nil && string(data) == "old lock" {
			return fs.ErrPermission
		}
	}
	return filesystem.Fs.Remove(name)
}

func fileSnapshot(t *testing.T, filesystem afero.Fs) map[string]string {
	t.Helper()
	result := map[string]string{}
	if err := afero.Walk(
		filesystem,
		string(filepath.Separator),
		func(filename string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				result[filename] = "<directory>"
				return nil
			}
			data, err := afero.ReadFile(
				filesystem,
				filename,
			)
			if err != nil {
				return err
			}
			result[filename] = string(data)
			return nil
		},
	); err != nil && !errors.Is(
		err,
		fs.ErrNotExist,
	) {
		t.Fatal(err)
	}
	return result
}

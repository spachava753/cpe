package bundlefs_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

type transactionFault uint8

const (
	noFault transactionFault = iota
	stageFault
	publishFault
	backupFault
	rollbackFault
	cleanupFault
	cancelPublication
)

type transactionFS struct {
	afero.Fs
	cancel context.CancelFunc
	fault  transactionFault
}

func (filesystem transactionFS) OpenFile(name string, flag int, perm fs.FileMode) (afero.File, error) {
	if filesystem.fault == stageFault && strings.Contains(
		filepath.ToSlash(name),
		"/new/alpha/",
	) {
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.OpenFile(
		name,
		flag,
		perm,
	)
}

func (filesystem transactionFS) Rename(oldName string, newName string) error {
	oldPath, newPath := filepath.ToSlash(oldName), filepath.ToSlash(newName)
	if strings.Contains(
		oldPath,
		"/new/beta",
	) && (filesystem.fault == publishFault || filesystem.fault == rollbackFault) {
		return fs.ErrPermission
	}
	if filesystem.fault == backupFault && strings.Contains(
		newPath,
		"/old/beta",
	) {
		return fs.ErrPermission
	}
	if filesystem.fault == rollbackFault && strings.Contains(
		oldPath,
		"/old/alpha",
	) {
		return fs.ErrPermission
	}
	if filesystem.fault == cancelPublication && strings.Contains(
		oldPath,
		"/new/alpha",
	) {
		filesystem.cancel()
	}
	return filesystem.Fs.Rename(
		oldName,
		newName,
	)
}

func (filesystem transactionFS) RemoveAll(name string) error {
	if filesystem.fault == cleanupFault && strings.HasPrefix(
		filepath.Base(name),
		".cpe-stage-",
	) {
		return fs.ErrPermission
	}
	return filesystem.Fs.RemoveAll(name)
}

func TestApply(t *testing.T) {
	initial := map[string]string{
		"alpha/marker":  "old alpha",
		"alpha/old.txt": "old resource",
		"beta/marker":   "old beta",
	}
	replacements := []bundlefs.Change{
		{
			Name:   "alpha",
			Action: bundlefs.Replace,
			Payload: []bundlefs.Entry{
				{
					Path: "marker",
					Data: []byte("new alpha"),
				},
				{
					Path: "bin/run",
					Data: []byte("executable"),
					Mode: 0o755,
				},
			},
		},
		{
			Name:   "beta",
			Action: bundlefs.Replace,
			Payload: []bundlefs.Entry{
				{
					Path: "marker",
					Data: []byte("new beta"),
				},
			},
		},
	}
	replaced := map[string]string{
		"alpha/marker":  "new alpha",
		"alpha/bin/run": "executable",
		"beta/marker":   "new beta",
	}
	for _, test := range []struct {
		wantCause      error
		initial        map[string]string
		wantFiles      map[string]string
		name           string
		errText        string
		changes        []bundlefs.Change
		fault          transactionFault
		rejectExisting bool
		nilCheck       bool
		wantOutcome    bundlefs.Outcome
		wantRecovery   bool
	}{
		{
			name:        "replace entire bundles",
			initial:     initial,
			changes:     replacements,
			wantOutcome: bundlefs.Committed,
			wantFiles:   replaced,
		},
		{
			name:        "empty selection",
			initial:     initial,
			wantOutcome: bundlefs.Committed,
			wantFiles:   initial,
		},
		{
			name: "new root",
			changes: []bundlefs.Change{
				{
					Name:   "alpha",
					Action: bundlefs.Install,
					Payload: []bundlefs.Entry{
						{
							Path: ".",
							Mode: fs.ModeDir,
						},
						{
							Path: "empty",
							Mode: fs.ModeDir,
						},
						{
							Path: "marker",
							Data: []byte("new alpha"),
						},
					},
				},
			},
			wantOutcome: bundlefs.Committed,
			wantFiles: map[string]string{
				"alpha/marker": "new alpha",
			},
		},
		{
			name:    "remove selected bundle",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name:   "alpha",
					Action: bundlefs.Remove,
				},
			},
			wantOutcome: bundlefs.Committed,
			wantFiles: map[string]string{
				"beta/marker": "old beta",
			},
		},
		{
			name:      "staging failure leaves no new root",
			changes:   replacements,
			fault:     stageFault,
			wantCause: fs.ErrPermission,
			wantFiles: map[string]string{},
		},
		{
			name:      "staging failure preserves old bundles",
			initial:   initial,
			changes:   replacements,
			fault:     stageFault,
			wantCause: fs.ErrPermission,
			wantFiles: initial,
		},
		{
			name:      "publication failure restores every old bundle",
			initial:   initial,
			changes:   replacements,
			fault:     publishFault,
			wantCause: fs.ErrPermission,
			wantFiles: initial,
		},
		{
			name:      "backup failure restores prior publication",
			initial:   initial,
			changes:   replacements,
			fault:     backupFault,
			wantCause: fs.ErrPermission,
			wantFiles: initial,
		},
		{
			name:           "eligibility is rechecked before publication",
			initial:        initial,
			changes:        replacements,
			rejectExisting: true,
			wantCause:      fs.ErrPermission,
			wantFiles:      initial,
		},
		{
			name:      "cancellation rolls back publication",
			initial:   initial,
			changes:   replacements,
			fault:     cancelPublication,
			wantCause: context.Canceled,
			wantFiles: initial,
		},
		{
			name:      "rollback failure retains recovery files",
			initial:   initial,
			changes:   replacements,
			fault:     rollbackFault,
			wantCause: fs.ErrPermission,
			errText:   "recovery files retained",
			wantFiles: map[string]string{
				"beta/marker": "old beta",
			},
			wantRecovery: true,
		},
		{
			name:         "cleanup failure keeps committed outcome",
			initial:      initial,
			changes:      replacements,
			fault:        cleanupFault,
			wantOutcome:  bundlefs.Committed,
			wantCause:    fs.ErrPermission,
			errText:      "changes committed",
			wantFiles:    replaced,
			wantRecovery: true,
		},
		{
			name:    "install cannot replace",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name:   "alpha",
					Action: bundlefs.Install,
				},
			},
			wantCause: fs.ErrExist,
			wantFiles: initial,
		},
		{
			name:      "missing eligibility boundary",
			initial:   initial,
			changes:   replacements,
			nilCheck:  true,
			errText:   "check is unavailable",
			wantFiles: initial,
		},
		{
			name:    "escaping name",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "../outside",
				},
			},
			errText:   "directory name",
			wantFiles: initial,
		},
		{
			name:    "Windows directory separator",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "a\\b",
				},
			},
			errText:   "directory name",
			wantFiles: initial,
		},
		{
			name:    "drive directory name",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "C:bundle",
				},
			},
			errText:   "directory name",
			wantFiles: initial,
		},
		{
			name:    "duplicate name",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "alpha",
				},
				{
					Name: "alpha",
				},
			},
			errText:   "duplicate",
			wantFiles: initial,
		},
		{
			name:    "unknown action",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name:   "alpha",
					Action: bundlefs.Action(99),
				},
			},
			errText:   "unsupported",
			wantFiles: initial,
		},
		{
			name:    "removal with payload",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name:   "alpha",
					Action: bundlefs.Remove,
					Payload: []bundlefs.Entry{
						{
							Path: "marker",
						},
					},
				},
			},
			errText:   "removal",
			wantFiles: initial,
		},
		{
			name:    "escaping payload",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "alpha",
					Payload: []bundlefs.Entry{
						{
							Path: "../../outside",
						},
					},
				},
			},
			errText:   "invalid payload",
			wantFiles: initial,
		},
		{
			name:    "Windows payload separator",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "alpha",
					Payload: []bundlefs.Entry{
						{
							Path: "a\\b",
						},
					},
				},
			},
			errText:   "invalid payload",
			wantFiles: initial,
		},
		{
			name:    "null payload path",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "alpha",
					Payload: []bundlefs.Entry{
						{
							Path: "a\x00b",
						},
					},
				},
			},
			errText:   "invalid payload",
			wantFiles: initial,
		},
		{
			name:    "directory with file data",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "alpha",
					Payload: []bundlefs.Entry{
						{
							Path: ".",
							Mode: fs.ModeDir,
							Data: []byte("file"),
						},
					},
				},
			},
			errText:   "directory payload",
			wantFiles: initial,
		},
		{
			name:    "payload symlink",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "alpha",
					Payload: []bundlefs.Entry{
						{
							Path: "link",
							Mode: fs.ModeSymlink,
						},
					},
				},
			},
			errText:   "regular file or directory",
			wantFiles: initial,
		},
		{
			name:    "root payload is a file",
			initial: initial,
			changes: []bundlefs.Change{
				{
					Name: "alpha",
					Payload: []bundlefs.Entry{
						{
							Path: ".",
						},
					},
				},
			},
			errText:   "regular file or directory",
			wantFiles: initial,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				root := filepath.FromSlash("/bundles")
				base := afero.NewMemMapFs()
				for name, data := range test.initial {
					filename := filepath.Join(
						root,
						filepath.FromSlash(name),
					)
					if err := base.MkdirAll(
						filepath.Dir(filename),
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := afero.WriteFile(
						base,
						filename,
						[]byte(data),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				filesystem := transactionFS{
					Fs:     base,
					fault:  test.fault,
					cancel: cancel,
				}
				check := func(directory string) error {
					if test.rejectExisting && filepath.Base(directory) == "beta" {
						return fs.ErrPermission
					}
					info, err := bundlefs.Lstat(
						base,
						filepath.Join(
							directory,
							"marker",
						),
					)
					if err != nil {
						return err
					}
					if !info.Mode().IsRegular() {
						return errors.New("marker must be regular")
					}
					return nil
				}
				if test.nilCheck {
					check = nil
				}
				outcome, err := bundlefs.Apply(
					ctx,
					filesystem,
					root,
					test.changes,
					check,
				)
				if outcome != test.wantOutcome {
					t.Fatalf(
						"outcome = %v, want %v",
						outcome,
						test.wantOutcome,
					)
				}
				if test.wantCause != nil && !errors.Is(
					err,
					test.wantCause,
				) {
					t.Fatalf(
						"error = %v, want cause %v",
						err,
						test.wantCause,
					)
				}
				if test.errText != "" && (err == nil || !strings.Contains(
					err.Error(),
					test.errText,
				)) {
					t.Fatalf(
						"error = %v, want %q",
						err,
						test.errText,
					)
				}
				if test.wantCause == nil && test.errText == "" && err != nil {
					t.Fatal(err)
				}
				actual := map[string]string{}
				var stages []string
				err = afero.Walk(
					base,
					filepath.Dir(root),
					func(filename string, info fs.FileInfo, err error) error {
						if err != nil {
							return err
						}
						if info.IsDir() {
							if strings.HasPrefix(
								info.Name(),
								".cpe-stage-",
							) {
								stages = append(
									stages,
									filename,
								)
								return filepath.SkipDir
							}
							return nil
						}
						data, err := afero.ReadFile(
							base,
							filename,
						)
						if err != nil {
							return err
						}
						name, err := filepath.Rel(
							root,
							filename,
						)
						if err != nil {
							return err
						}
						actual[filepath.ToSlash(name)] = string(data)
						return nil
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(
					actual,
					test.wantFiles,
				) {
					t.Fatalf(
						"files = %v, want %v",
						actual,
						test.wantFiles,
					)
				}
				if (len(stages) > 0) != test.wantRecovery {
					t.Fatalf(
						"recovery stages = %v, want retained = %v",
						stages,
						test.wantRecovery,
					)
				}
				if test.wantRecovery {
					data, err := afero.ReadFile(
						base,
						filepath.Join(
							stages[0],
							"old",
							"alpha",
							"marker",
						),
					)
					if err != nil || string(data) != "old alpha" {
						t.Fatalf(
							"recoverable original = %q, %v",
							data,
							err,
						)
					}
				}
				if test.fault == stageFault && test.initial == nil {
					if _, err := base.Stat(root); !errors.Is(
						err,
						fs.ErrNotExist,
					) {
						t.Fatalf(
							"failed install retained a new root: %v",
							err,
						)
					}
				}
				if _, exists := actual["alpha/bin/run"]; exists {
					info, err := base.Stat(filepath.Join(
						root,
						"alpha",
						"bin",
						"run",
					))
					if err != nil || info.Mode().Perm() != 0o755 {
						t.Fatalf(
							"executable permissions were not preserved: %v",
							err,
						)
					}
				}
				if test.name == "new root" {
					info, err := base.Stat(filepath.Join(
						root,
						"alpha",
						"empty",
					))
					if err != nil || !info.IsDir() {
						t.Fatalf(
							"empty payload directory was not installed: %v",
							err,
						)
					}
				}
			},
		)
	}
}

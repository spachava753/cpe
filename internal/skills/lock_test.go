package skills

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
)

func TestReadLock(t *testing.T) {
	for _, test := range []struct {
		name      string
		content   string
		format    LockFormat
		exists    bool
		readOnly  bool
		wantError string
	}{
		{
			name:   "missing project lock",
			format: ProjectLock,
		},
		{
			name:      "empty lock is corrupt, not absent",
			format:    ProjectLock,
			exists:    true,
			wantError: "lock",
		},
		{
			name:     "read-only tracking needs no sidecar",
			format:   ProjectLock,
			content:  `{"version":1,"skills":{}}`,
			readOnly: true,
		},
		{
			name:      "corrupt lock is not silently discarded",
			format:    ProjectLock,
			content:   "<<<<<<< conflict",
			wantError: "lock",
		},
		{
			name:      "future version rejected",
			format:    ProjectLock,
			content:   `{"version":2,"skills":{}}`,
			wantError: "version",
		},
		{
			name:      "null skills rejected",
			format:    ProjectLock,
			content:   `{"version":1,"skills":null}`,
			wantError: "skills",
		},
		{
			name:      "null envelope rejected",
			format:    ProjectLock,
			content:   `null`,
			wantError: "lock",
		},
		{
			name:      "unconfigured format rejected",
			wantError: "format",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				if test.content != "" || test.exists {
					if err := afero.WriteFile(
						filesystem,
						"/skills-lock.json",
						[]byte(test.content),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				if test.readOnly {
					filesystem = afero.NewReadOnlyFs(filesystem)
				}
				manager := NewManager(
					nil,
					filesystem,
				)
				lock, err := manager.readLock(
					t.Context(),
					LockFile{
						Path:   "/skills-lock.json",
						Format: test.format,
					},
				)
				if test.wantError != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.wantError,
					) {
						t.Fatalf(
							"readLock error = %v, want %q",
							err,
							test.wantError,
						)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if lock == nil || lock.skills == nil || len(lock.skills) != 0 {
					t.Fatalf(
						"expected empty initialized tracking, got %+v",
						lock,
					)
				}
			},
		)
	}
}

package skills

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func TestReadLock(t *testing.T) {
	for _, test := range []struct {
		name      string
		content   string
		format    LockFormat
		wantError string
	}{
		{
			name:   "missing project lock",
			format: ProjectLock,
		},
		{
			name:    "project lock preserves unsupported records",
			format:  ProjectLock,
			content: `{"version":1,"skills":{"foreign":{"sourceType":"node_modules","extra":true}},"extra":{"enabled":true}}`,
		},
		{
			name:    "global preferences preserved",
			format:  GlobalLock,
			content: `{"version":3,"skills":{},"dismissed":{"findSkillsPrompt":true},"lastSelectedAgents":["claude-code"]}`,
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
				if test.content != "" {
					if err := afero.WriteFile(
						filesystem,
						"/skills-lock.json",
						[]byte(test.content),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
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
				encoded, err := lock.encode()
				if err != nil {
					t.Fatal(err)
				}
				if test.content != "" {
					var want, got any
					if err := json.Unmarshal(
						[]byte(test.content),
						&want,
					); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(
						encoded,
						&got,
					); err != nil {
						t.Fatal(err)
					}
					wantJSON, _ := json.Marshal(want)
					gotJSON, _ := json.Marshal(got)
					if string(wantJSON) != string(gotJSON) {
						t.Fatalf(
							"lock round trip changed fields: %s",
							gotJSON,
						)
					}
				}
			},
		)
	}
}

func TestFolderHash(t *testing.T) {
	// Generated with upstream's Node SHA-256 algorithm and localeCompare sort.
	payload := []bundlefs.Entry{
		{
			Path: "SKILL.md",
			Data: []byte("---\nname: alpha\ndescription: Alpha skill\n---\n"),
		},
		{
			Path: "A.txt",
			Data: []byte("upper"),
		},
		{
			Path: "a.txt",
			Data: []byte("lower"),
		},
		{
			Path: "assets/z.bin",
			Data: []byte{
				0,
				1,
				255,
			},
		},
		{
			Path: "é.txt",
			Data: []byte("accent"),
		},
		{
			Path: "_note",
			Data: []byte("punctuation"),
		},
	}
	if got := folderHash(payload); got != "1b605fff4f64b77114d26a9ef43415ae38b87df5f51c48b729316c9b31eda674" {
		t.Fatalf(
			"folderHash = %s",
			got,
		)
	}
}

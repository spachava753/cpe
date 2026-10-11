package skills

import (
	"encoding/json"
	"strconv"
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
	// Generated with upstream's SHA-256 algorithm and Node 26.8.2 / ICU 78.3
	// en-US localeCompare. No external executable is used by these tests.
	var equivalents []bundlefs.Entry
	for i, name := range []string{
		"a\u0327\u0323\u0301\u0315.txt",
		"a\u0327\u0323\u0315\u0301.txt",
		"a\u0327\u0301\u0323\u0315.txt",
		"a\u0327\u0301\u0315\u0323.txt",
		"a\u0327\u0315\u0323\u0301.txt",
		"a\u0327\u0315\u0301\u0323.txt",
		"a\u0323\u0327\u0301\u0315.txt",
		"a\u0323\u0327\u0315\u0301.txt",
		"a\u0323\u0301\u0327\u0315.txt",
		"a\u0323\u0301\u0315\u0327.txt",
		"a\u0323\u0315\u0327\u0301.txt",
		"a\u0323\u0315\u0301\u0327.txt",
		"a\u0301\u0327\u0323\u0315.txt",
		"a\u0301\u0327\u0315\u0323.txt",
		"a\u0301\u0323\u0327\u0315.txt",
		"a\u0301\u0323\u0315\u0327.txt",
		"a\u0301\u0315\u0327\u0323.txt",
		"a\u0301\u0315\u0323\u0327.txt",
		"a\u0315\u0327\u0323\u0301.txt",
		"a\u0315\u0327\u0301\u0323.txt",
		"a\u0315\u0323\u0327\u0301.txt",
		"a\u0315\u0323\u0301\u0327.txt",
		"a\u0315\u0301\u0327\u0323.txt",
		"a\u0315\u0301\u0323\u0327.txt",
	} {
		equivalents = append(
			equivalents,
			bundlefs.Entry{
				Path: name,
				Data: []byte(strconv.Itoa(i)),
			},
		)
	}
	for _, test := range []struct {
		name    string
		payload []bundlefs.Entry
		want    string
	}{
		{
			name: "original upstream vector",
			payload: []bundlefs.Entry{
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
			},
			want: "1b605fff4f64b77114d26a9ef43415ae38b87df5f51c48b729316c9b31eda674",
		},
		{
			name: "robot regression",
			payload: []bundlefs.Entry{
				{
					Path: "SKILL.md",
					Data: []byte("skill"),
				},
				{
					Path: "a.txt",
					Data: []byte("letter"),
				},
				{
					Path: "🤖.txt",
					Data: []byte("robot"),
				},
			},
			want: "cd4bd4942f844457860f54f12fd4bec412d11c51f1647ddd618ad40d1a26a95a",
		},
		{
			name: "skipped Tibetan marks form another contraction",
			payload: []bundlefs.Entry{
				{
					Path: "\u0f71\u0f71\u0f72\u0f72.txt",
					Data: []byte("double contraction"),
				},
				{
					Path: "\u0f73\u0f72.txt",
					Data: []byte("contraction plus single"),
				},
				{
					Path: "SKILL.md",
					Data: []byte("skill"),
				},
			},
			want: "e11b5f7734ab8c1f1bd46e3417cb006a72687b1ef09f3446a37da7f2a7de909f",
		},
		{
			name: "Unicode 15 through 17 emoji",
			payload: []bundlefs.Entry{
				{
					Path: "SKILL.md",
					Data: []byte("skill"),
				},
				{
					Path: "🫪.txt",
					Data: []byte("distorted"),
				},
				{
					Path: "🫩.txt",
					Data: []byte("bags"),
				},
				{
					Path: "🩷.txt",
					Data: []byte("heart"),
				},
				{
					Path: "a.txt",
					Data: []byte("letter"),
				},
			},
			want: "2092299d8ef699f32b1e725e2c657213bf9af00df47db355ff99c4315a6468d4",
		},
		{
			name: "Han ligatures and case",
			payload: []bundlefs.Entry{
				{
					Path: "SKILL.md",
					Data: []byte("skill"),
				},
				{
					Path: "𠀀.txt",
					Data: []byte("han-extension"),
				},
				{
					Path: "三.txt",
					Data: []byte("han-base"),
				},
				{
					Path: "æ.txt",
					Data: []byte("ligature"),
				},
				{
					Path: "ae.txt",
					Data: []byte("letters"),
				},
				{
					Path: "A.txt",
					Data: []byte("upper"),
				},
				{
					Path: "a.txt",
					Data: []byte("lower"),
				},
			},
			want: "2c39124e255670f669e6f34fc917c3e4f503d4e89bbd9ef57f32143651c2ac59",
		},
		{
			name: "canonically equivalent paths retain input order",
			payload: []bundlefs.Entry{
				{
					Path: "SKILL.md",
					Data: []byte("skill"),
				},
				{
					Path: "é.txt",
					Data: []byte("composed"),
				},
				{
					Path: "é.txt",
					Data: []byte("decomposed"),
				},
				{
					Path: "a.txt",
					Data: []byte("letter"),
				},
			},
			want: "9e2ad943f33d8dd50afdf1c17ecc846c00c206ab238e0c0a68bdc8a853259c4c",
		},
		{
			name:    "large canonical-equivalence class is stable",
			payload: equivalents,
			want:    "f0b517683635388be856156cad74550335b8c16c93aaff7d52cfcd3b79206fec",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				if got := folderHash(test.payload); got != test.want {
					t.Fatalf(
						"folderHash = %s; want %s",
						got,
						test.want,
					)
				}
			},
		)
	}
}

package skills

import (
	"io/fs"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func TestHashRecord(t *testing.T) {
	for _, test := range []struct {
		name   string
		format LockFormat
	}{
		{
			name:   "project hash fields",
			format: ProjectLock,
		},
		{
			name:   "global hash fields",
			format: GlobalLock,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				lock := &skillLock{
					file: LockFile{
						Format: test.format,
					},
				}
				got := lock.hashRecord(
					Skill{
						Path: "nested/alpha/SKILL.md",
					},
					[]bundlefs.Entry{
						{
							Path: "SKILL.md",
							Data: []byte("skill"),
						},
					},
					lockedSkill{},
				)
				if got.SkillPath != "nested/alpha/SKILL.md" {
					t.Fatal("source-relative skill path was lost")
				}
				if test.format == ProjectLock && (len(got.ComputedHash) != 64 || got.SkillFolderHash != "") {
					t.Fatalf(
						"incorrect project hash fields: %+v",
						got,
					)
				}
				if test.format == GlobalLock && (len(got.SkillFolderHash) != 64 || got.ComputedHash != "") {
					t.Fatalf(
						"incorrect global hash fields: %+v",
						got,
					)
				}
			},
		)
	}
}

func TestFolderHash(t *testing.T) {
	// This vector was independently calculated with Python's hashlib and struct.
	vector := []bundlefs.Entry{
		{
			Path: "SKILL.md",
			Data: []byte("skill"),
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
			Path: "é.txt",
			Data: []byte("composed"),
		},
		{
			Path: "e\u0301.txt",
			Data: []byte("decomposed"),
		},
		{
			Path: "🤖.txt",
			Data: []byte{
				0,
				255,
			},
		},
	}
	reversed := slices.Clone(vector)
	slices.Reverse(reversed)
	withDirectory := append(
		slices.Clone(vector),
		bundlefs.Entry{
			Path: "empty",
			Mode: fs.ModeDir | 0o700,
		},
	)
	for _, test := range []struct {
		name    string
		want    string
		payload []bundlefs.Entry
		other   []bundlefs.Entry
	}{
		{
			name:    "byte ordering distinguishes case and canonical-equivalent paths",
			payload: vector,
			want:    "aa08ed019672689cd4d864384edd2df0febde62dddd4c1117e29bb6dde1a78ee",
		},
		{
			name:    "traversal order does not affect the fingerprint",
			payload: reversed,
			want:    "aa08ed019672689cd4d864384edd2df0febde62dddd4c1117e29bb6dde1a78ee",
		},
		{
			name:    "empty directories do not affect the fingerprint",
			payload: withDirectory,
			want:    "aa08ed019672689cd4d864384edd2df0febde62dddd4c1117e29bb6dde1a78ee",
		},
		{
			name: "path and content boundaries are unambiguous",
			payload: []bundlefs.Entry{
				{
					Path: "a",
					Data: []byte("bc"),
				},
			},
			other: []bundlefs.Entry{
				{
					Path: "ab",
					Data: []byte("c"),
				},
			},
		},
		{
			name: "file boundaries are unambiguous",
			payload: []bundlefs.Entry{
				{
					Path: "a",
					Data: []byte("bcd"),
				},
			},
			other: []bundlefs.Entry{
				{
					Path: "a",
					Data: []byte("b"),
				},
				{
					Path: "c",
					Data: []byte("d"),
				},
			},
		},
		{
			name: "renaming a resource changes the fingerprint",
			payload: []bundlefs.Entry{
				{
					Path: "a.txt",
					Data: []byte("same content"),
				},
			},
			other: []bundlefs.Entry{
				{
					Path: "b.txt",
					Data: []byte("same content"),
				},
			},
		},
		{
			name: "editing a resource changes the fingerprint",
			payload: []bundlefs.Entry{
				{
					Path: "a.txt",
					Data: []byte("before"),
				},
			},
			other: []bundlefs.Entry{
				{
					Path: "a.txt",
					Data: []byte("after"),
				},
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				before := slices.Clone(test.payload)
				got := folderHash(test.payload)
				if test.want != "" && got != test.want {
					t.Fatalf(
						"folderHash = %s; want %s",
						got,
						test.want,
					)
				}
				if test.other != nil && got == folderHash(test.other) {
					t.Fatal("different file trees produced the same fingerprint")
				}
				if diff := cmp.Diff(
					before,
					test.payload,
				); diff != "" {
					t.Fatalf(
						"hashing mutated the payload: %s",
						diff,
					)
				}
			},
		)
	}
}

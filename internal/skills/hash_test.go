package skills

import (
	"context"
	"testing"

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/source"
)

func TestHashRecord(t *testing.T) {
	for _, test := range []struct {
		name         string
		format       LockFormat
		sourceType   skillSourceType
		metadata     bool
		wantMetadata bool
		wantError    bool
	}{
		{
			name:         "global GitHub record uses acquired tree metadata",
			format:       GlobalLock,
			sourceType:   githubSource,
			metadata:     true,
			wantMetadata: true,
		},
		{
			name:       "global GitHub requires tree metadata rather than checkout-byte fallback",
			format:     GlobalLock,
			sourceType: githubSource,
			wantError:  true,
		},
		{
			name:       "project uses content hashing even for GitHub sources",
			format:     ProjectLock,
			sourceType: githubSource,
			metadata:   true,
		},
		{
			name:       "generic global Git uses content hashing",
			format:     GlobalLock,
			sourceType: gitSource,
			metadata:   true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				const tree = "70993c44c50665a842d44706f3daf42d0530412f"
				checkout := source.Checkout{}
				called := false
				if test.metadata {
					checkout.GitTreeHash = func(ctx context.Context, directory string) (string, error) {
						called = true
						if ctx != t.Context() || directory != "nested/alpha" {
							t.Fatal("tree metadata request did not preserve context and source directory")
						}
						return tree, nil
					}
				}
				lock := &skillLock{
					file: LockFile{
						Format: test.format,
					},
				}
				got, err := lock.hashRecord(
					t.Context(),
					checkout,
					Skill{
						Path: "nested/alpha/SKILL.md",
					},
					[]bundlefs.Entry{
						{
							Path: "SKILL.md",
							Data: []byte("skill"),
						},
					},
					lockedSkill{
						SourceType: test.sourceType,
					},
				)
				if (err != nil) != test.wantError || called != test.wantMetadata {
					t.Fatalf(
						"hashRecord = %v; metadata called = %v",
						err,
						called,
					)
				}
				if test.wantError {
					return
				}
				if got.SkillPath != "nested/alpha/SKILL.md" {
					t.Fatal("source-relative skill path was lost")
				}
				if test.wantMetadata && got.SkillFolderHash != tree {
					t.Fatal("record did not retain the Git tree object ID")
				}
				if !test.wantMetadata && len(got.ComputedHash)+len(got.SkillFolderHash) != 64 {
					t.Fatal("content hash was not recorded in the selected schema")
				}
			},
		)
	}
}

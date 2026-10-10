package bundlefs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func TestTrackedFilePathSafety(t *testing.T) {
	for _, test := range []struct {
		name            string
		parentTraversal bool
		finalLink       bool
	}{
		{
			name: "parent symlink",
		},
		{
			name:            "parent symlink cannot be hidden by parent traversal",
			parentTraversal: true,
		},
		{
			name:      "final file symlink",
			finalLink: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				root := t.TempDir()
				safe := filepath.Join(
					root,
					"safe",
				)
				foreign := filepath.Join(
					root,
					"foreign",
				)
				for _, directory := range []string{
					filepath.Join(
						safe,
						"outside",
					),
					filepath.Join(
						foreign,
						"child",
					),
					filepath.Join(
						foreign,
						"outside",
					),
				} {
					if err := os.MkdirAll(
						directory,
						0o700,
					); err != nil {
						t.Fatal(err)
					}
				}
				target := filepath.Join(
					foreign,
					"outside",
					"lock.json",
				)
				if err := os.WriteFile(
					target,
					[]byte("foreign lock"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				filename := filepath.Join(
					safe,
					"link",
					"lock.json",
				)
				link, destination := filepath.Join(
					safe,
					"link",
				), filepath.Join(
					foreign,
					"outside",
				)
				if test.parentTraversal {
					destination = filepath.Join(
						foreign,
						"child",
					)
					filename = safe + string(filepath.Separator) + "link" + string(filepath.Separator) + ".." + string(filepath.Separator) + "outside" + string(filepath.Separator) + "lock.json"
				}
				if test.finalLink {
					filename = filepath.Join(
						safe,
						"lock.json",
					)
					link, destination = filename, target
				}
				if err := os.Symlink(
					destination,
					link,
				); err != nil {
					t.Skipf(
						"symlink unavailable: %v",
						err,
					)
				}
				filesystem := afero.NewOsFs()
				if _, err := bundlefs.ReadFile(
					t.Context(),
					filesystem,
					filename,
				); err == nil {
					t.Fatal("linked lock path was read")
				}
				outcome, err := bundlefs.ApplyWithFile(
					t.Context(),
					filesystem,
					filepath.Join(
						safe,
						"skills",
					),
					[]bundlefs.Change{
						{
							Name:   "alpha",
							Action: bundlefs.Install,
							Payload: []bundlefs.Entry{
								{
									Path: "marker",
									Data: []byte("new bundle"),
								},
							},
						},
					},
					func(string) error { return nil },
					bundlefs.FileChange{
						Path:     filename,
						Data:     []byte("new lock"),
						Previous: []byte("foreign lock"),
					},
				)
				if err == nil || outcome != bundlefs.NotCommitted {
					t.Fatalf(
						"ApplyWithFile = %v, %v",
						outcome,
						err,
					)
				}
				if !test.finalLink && !strings.Contains(
					err.Error(),
					"symlink",
				) {
					t.Fatalf(
						"uncleaned parent was not checked: %v",
						err,
					)
				}
				data, err := os.ReadFile(target)
				if err != nil || string(data) != "foreign lock" {
					t.Fatalf(
						"foreign lock changed: %s, %v",
						data,
						err,
					)
				}
				if exists, err := afero.Exists(
					filesystem,
					filepath.Join(
						safe,
						"skills",
					),
				); err != nil || exists {
					t.Fatal("rejected file path caused bundle changes")
				}
			},
		)
	}
}

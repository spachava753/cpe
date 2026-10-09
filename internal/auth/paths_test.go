package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

func TestStorePaths(t *testing.T) {
	for _, test := range []struct {
		name          string
		suffix        string
		fileLink      bool
		missingTarget bool
	}{
		{
			name: "bare symlink",
		},
		{
			name:     "credential file symlink",
			fileLink: true,
		},
		{
			name:          "dangling credential file symlink",
			fileLink:      true,
			missingTarget: true,
		},
		{
			name:   "trailing separator",
			suffix: string(os.PathSeparator),
		},
		{
			name:   "trailing dot",
			suffix: string(os.PathSeparator) + ".",
		},
		{
			name:   "parent traversal",
			suffix: string(os.PathSeparator) + "..",
		},
		{
			name:   "link in parent directory",
			suffix: string(os.PathSeparator) + "nested",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				outside := t.TempDir()
				path := filepath.Join(
					outside,
					credentialFile,
				)
				if err := os.WriteFile(
					path,
					[]byte(testAuthDocument),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(
					t.TempDir(),
					"link",
				)
				target := outside
				if test.fileLink {
					target = path
				}
				if test.missingTarget {
					target += ".missing"
				}
				if err := os.Symlink(
					target,
					link,
				); err != nil {
					t.Skipf(
						"symlinks unavailable: %v",
						err,
					)
				}
				store := NewStore(afero.NewOsFs())
				filename := link + test.suffix + string(os.PathSeparator) + credentialFile
				if test.fileLink {
					filename = link
				}
				if _, err := store.Retrieve(
					t.Context(),
					filename,
				); err == nil {
					t.Error("retrieve accepted a symlink directory")
				}
				if err := store.Save(
					t.Context(),
					filename,
					OpenCode,
					Credentials{
						OpenCode: &APIKeyCredentials{
							APIKey: "new",
						},
					},
				); err == nil {
					t.Error("save accepted a symlink directory")
				}
				if err := store.Save(
					t.Context(),
					filename,
					OpenCode,
					Credentials{},
				); err == nil {
					t.Error("logout accepted a symlink directory")
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != testAuthDocument {
					t.Error("symlink directory allowed external credential changes")
				}
				if _, err := os.Stat(filepath.Join(
					outside,
					"nested",
				)); !os.IsNotExist(err) {
					t.Error("rejected path created an external directory")
				}
			},
		)
	}
}

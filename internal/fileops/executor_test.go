package fileops

import (
	"github.com/spachava753/cpe/internal/extract"
	"os"
	"strings"
	"testing"
)

func TestValidateModifyCode(t *testing.T) {
	// Create a temporary test file
	content := `line1
line1
line2
line3`
	tempDir := t.TempDir()
	tmpfile, err := os.CreateTemp(tempDir, "test.*.txt")
	if err != nil {
		t.Fatal(err)
	}

	if _, err = tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err = tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		mod     extract.ModifyFile
		wantErr bool
		errMsg  string
	}{
		{
			name: "Single match",
			mod: extract.ModifyFile{
				Path: tmpfile.Name(),
				Edits: []extract.Edit{
					{Search: "line2", Replace: "newline2"},
				},
			},
			wantErr: false,
		},
		{
			name: "Multiple matches",
			mod: extract.ModifyFile{
				Path: tmpfile.Name(),
				Edits: []extract.Edit{
					{Search: "line1", Replace: "newline1"},
				},
			},
			wantErr: true,
			errMsg:  "matches 2 times",
		},
		{
			name: "No matches",
			mod: extract.ModifyFile{
				Path: tmpfile.Name(),
				Edits: []extract.Edit{
					{Search: "nonexistent", Replace: "new"},
				},
			},
			wantErr: true,
			errMsg:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModifyCode(tt.mod)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateModifyCode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateModifyCode() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"Valid relative path", "./main.go", false},
		{"Valid nested relative path", "./pkg/file.go", false},
		{"Invalid absolute path", "/main.go", true},
		{"Invalid nested absolute path", "/pkg/file.go", true},
		{"Invalid path outside project", "../outside.go", true},
		{"Valid current directory path", "main.go", false},
		{"Valid nested current directory path", "pkg/file.go", false},
		{"Invalid double parent path", "../../outside.go", true},
		{"Invalid double parent path", "/../../outside.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

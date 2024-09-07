package fileops

import "testing"

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

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
		{"Valid absolute path", "/main.go", false},
		{"Valid nested absolute path", "/pkg/file.go", false},
		{"Invalid path outside project", "../outside.go", true},
		{"Another invalid path", "C:/Windows/System32/file.txt", true},
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

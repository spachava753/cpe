package parser

import (
	"reflect"
	"testing"
)

func TestParseModifyCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModifyCode
		wantErr  bool
	}{
		{
			name: "Valid ModifyCode",
			input: `<modify_code>
<path>main.go</path>
<modification>
<search>fmt.Println("Hello, World!")</search>
<replace>fmt.Println("Hello, Go!")</replace>
</modification>
<modification>
<search>var x int</search>
<replace>var x int32</replace>
</modification>
<explanation>Updated greeting and changed variable type</explanation>
</modify_code>`,
			expected: ModifyCode{
				Path: "main.go",
				Modifications: []struct {
					Search  string
					Replace string
				}{
					{Search: `fmt.Println("Hello, World!")`, Replace: `fmt.Println("Hello, Go!")`},
					{Search: "var x int", Replace: "var x int32"},
				},
				Explanation: "Updated greeting and changed variable type",
			},
			wantErr: false,
		},
		{
			name:     "Invalid ModifyCode - Missing Path",
			input:    "<modify_code></modify_code>",
			expected: ModifyCode{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseModifyCode(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseModifyCode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseModifyCode() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseRemoveFile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected RemoveFile
		wantErr  bool
	}{
		{
			name: "Valid RemoveFile",
			input: `<remove_file>
<path>obsolete.go</path>
<explanation>This file is no longer needed</explanation>
</remove_file>`,
			expected: RemoveFile{
				Path:        "obsolete.go",
				Explanation: "This file is no longer needed",
			},
			wantErr: false,
		},
		{
			name:     "Invalid RemoveFile - Missing Path",
			input:    "<remove_file></remove_file>",
			expected: RemoveFile{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseRemoveFile(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRemoveFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseRemoveFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseCreateFile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected CreateFile
		wantErr  bool
	}{
		{
			name: "Valid CreateFile",
			input: `<create_file>
<path>newfile.go</path>
<content>
package main

import "fmt"

func main() {
	fmt.Println("Hello, new file!")
}
</content>
<explanation>Adding a new Go file with a simple main function</explanation>
</create_file>`,
			expected: CreateFile{
				Path: "newfile.go",
				Content: `package main

import "fmt"

func main() {
	fmt.Println("Hello, new file!")
}`,
				Explanation: "Adding a new Go file with a simple main function",
			},
			wantErr: false,
		},
		{
			name:     "Invalid CreateFile - Missing Path",
			input:    "<create_file><content>Some content</content></create_file>",
			expected: CreateFile{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseCreateFile(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCreateFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseCreateFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseRenameFile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected RenameFile
		wantErr  bool
	}{
		{
			name: "Valid RenameFile",
			input: `<rename_file>
<old_path>oldname.go</old_path>
<new_path>newname.go</new_path>
<explanation>Renaming for better clarity</explanation>
</rename_file>`,
			expected: RenameFile{
				OldPath:     "oldname.go",
				NewPath:     "newname.go",
				Explanation: "Renaming for better clarity",
			},
			wantErr: false,
		},
		{
			name:     "Invalid RenameFile - Missing Old Path",
			input:    "<rename_file><new_path>newname.go</new_path></rename_file>",
			expected: RenameFile{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseRenameFile(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRenameFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseRenameFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseMoveFile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected MoveFile
		wantErr  bool
	}{
		{
			name: "Valid MoveFile",
			input: `<move_file>
<old_path>src/file.go</old_path>
<new_path>pkg/file.go</new_path>
<explanation>Moving file to appropriate package</explanation>
</move_file>`,
			expected: MoveFile{
				OldPath:     "src/file.go",
				NewPath:     "pkg/file.go",
				Explanation: "Moving file to appropriate package",
			},
			wantErr: false,
		},
		{
			name:     "Invalid MoveFile - Missing New Path",
			input:    "<move_file><old_path>src/file.go</old_path></move_file>",
			expected: MoveFile{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseMoveFile(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMoveFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseMoveFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseCreateDirectory(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected CreateDirectory
		wantErr  bool
	}{
		{
			name: "Valid CreateDirectory",
			input: `<create_directory>
<path>new/directory</path>
<explanation>Creating a new directory for organization</explanation>
</create_directory>`,
			expected: CreateDirectory{
				Path:        "new/directory",
				Explanation: "Creating a new directory for organization",
			},
			wantErr: false,
		},
		{
			name:     "Invalid CreateDirectory - Missing Path",
			input:    "<create_directory></create_directory>",
			expected: CreateDirectory{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseCreateDirectory(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCreateDirectory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseCreateDirectory() = %v, want %v", result, tt.expected)
			}
		})
	}
}

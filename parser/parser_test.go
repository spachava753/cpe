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
		{
			name: "Invalid ModifyCode - Missing Modification",
			input: `<modify_code>
<path>main.go</path>
<explanation>No modifications provided</explanation>
</modify_code>`,
			expected: ModifyCode{
				Path:        "main.go",
				Explanation: "No modifications provided",
			},
			wantErr: false,
		},
		{
			name: "Invalid ModifyCode - Incomplete Modification",
			input: `<modify_code>
<path>main.go</path>
<modification>
<search>fmt.Println("Hello, World!")</search>
</modification>
</modify_code>`,
			expected: ModifyCode{},
			wantErr:  true,
		},
		{
			name: "Valid ModifyCode - Multiple Paths (should use first)",
			input: `<modify_code>
<path>main.go</path>
<path>ignored.go</path>
<modification>
<search>fmt.Println("Hello, World!")</search>
<replace>fmt.Println("Hello, Go!")</replace>
</modification>
</modify_code>`,
			expected: ModifyCode{
				Path: "main.go",
				Modifications: []struct {
					Search  string
					Replace string
				}{
					{Search: `fmt.Println("Hello, World!")`, Replace: `fmt.Println("Hello, Go!")`},
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid ModifyCode - Malformed Structure",
			input: `<modify_code>
<path>main.go</path>
<modification>
<search>fmt.Println("Hello, World!")</search>
<replace>fmt.Println("Hello, Go!")
</modification>
</modify_code>`,
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
		{
			name: "Valid RemoveFile - No Explanation",
			input: `<remove_file>
<path>unnecessary.go</path>
</remove_file>`,
			expected: RemoveFile{
				Path: "unnecessary.go",
			},
			wantErr: false,
		},
		{
			name: "Invalid RemoveFile - Empty Path",
			input: `<remove_file>
<path></path>
</remove_file>`,
			expected: RemoveFile{},
			wantErr:  true,
		},
		{
			name: "Valid RemoveFile - Multiple Paths (should use first)",
			input: `<remove_file>
<path>first.go</path>
<path>second.go</path>
</remove_file>`,
			expected: RemoveFile{
				Path: "first.go",
			},
			wantErr: false,
		},
		{
			name: "Invalid RemoveFile - Malformed Structure",
			input: `<remove_file>
<path>malformed.go
<explanation>Incomplete XML-like structure</explanation>
</remove_file>`,
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
		{
			name: "Invalid CreateFile - Missing Content",
			input: `<create_file>
<path>empty.go</path>
</create_file>`,
			expected: CreateFile{},
			wantErr:  true,
		},
		{
			name: "Valid CreateFile - No Explanation",
			input: `<create_file>
<path>simple.go</path>
<content>package main</content>
</create_file>`,
			expected: CreateFile{
				Path:    "simple.go",
				Content: "package main",
			},
			wantErr: false,
		},
		{
			name: "Invalid CreateFile - Empty Path",
			input: `<create_file>
<path></path>
<content>package main</content>
</create_file>`,
			expected: CreateFile{},
			wantErr:  true,
		},
		{
			name: "Valid CreateFile - Multiple Paths (should use first)",
			input: `<create_file>
<path>first.go</path>
<path>second.go</path>
<content>package main</content>
</create_file>`,
			expected: CreateFile{
				Path:    "first.go",
				Content: "package main",
			},
			wantErr: false,
		},
		{
			name: "Invalid CreateFile - Malformed Structure",
			input: `<create_file>
<path>malformed.go</path>
<content>package main
<explanation>Incomplete XML-like structure</explanation>
</create_file>`,
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
		{
			name: "Invalid RenameFile - Missing New Path",
			input: `<rename_file>
<old_path>oldname.go</old_path>
</rename_file>`,
			expected: RenameFile{},
			wantErr:  true,
		},
		{
			name: "Valid RenameFile - No Explanation",
			input: `<rename_file>
<old_path>old.go</old_path>
<new_path>new.go</new_path>
</rename_file>`,
			expected: RenameFile{
				OldPath: "old.go",
				NewPath: "new.go",
			},
			wantErr: false,
		},
		{
			name: "Invalid RenameFile - Empty Old Path",
			input: `<rename_file>
<old_path></old_path>
<new_path>new.go</new_path>
</rename_file>`,
			expected: RenameFile{},
			wantErr:  true,
		},
		{
			name: "Invalid RenameFile - Empty New Path",
			input: `<rename_file>
<old_path>old.go</old_path>
<new_path></new_path>
</rename_file>`,
			expected: RenameFile{},
			wantErr:  true,
		},
		{
			name: "Valid RenameFile - Multiple Old Paths (should use first)",
			input: `<rename_file>
<old_path>first.go</old_path>
<old_path>second.go</old_path>
<new_path>new.go</new_path>
</rename_file>`,
			expected: RenameFile{
				OldPath: "first.go",
				NewPath: "new.go",
			},
			wantErr: false,
		},
		{
			name: "Invalid RenameFile - Malformed Structure",
			input: `<rename_file>
<old_path>old.go</old_path>
<new_path>new.go
<explanation>Incomplete XML-like structure</explanation>
</rename_file>`,
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
		{
			name:     "Invalid MoveFile - Missing Old Path",
			input:    "<move_file><new_path>pkg/file.go</new_path></move_file>",
			expected: MoveFile{},
			wantErr:  true,
		},
		{
			name: "Valid MoveFile - No Explanation",
			input: `<move_file>
<old_path>src/file.go</old_path>
<new_path>pkg/file.go</new_path>
</move_file>`,
			expected: MoveFile{
				OldPath: "src/file.go",
				NewPath: "pkg/file.go",
			},
			wantErr: false,
		},
		{
			name: "Invalid MoveFile - Empty Old Path",
			input: `<move_file>
<old_path></old_path>
<new_path>pkg/file.go</new_path>
</move_file>`,
			expected: MoveFile{},
			wantErr:  true,
		},
		{
			name: "Invalid MoveFile - Empty New Path",
			input: `<move_file>
<old_path>src/file.go</old_path>
<new_path></new_path>
</move_file>`,
			expected: MoveFile{},
			wantErr:  true,
		},
		{
			name: "Valid MoveFile - Multiple Old Paths (should use first)",
			input: `<move_file>
<old_path>src/first.go</old_path>
<old_path>src/second.go</old_path>
<new_path>pkg/file.go</new_path>
</move_file>`,
			expected: MoveFile{
				OldPath: "src/first.go",
				NewPath: "pkg/file.go",
			},
			wantErr: false,
		},
		{
			name: "Invalid MoveFile - Malformed Structure",
			input: `<move_file>
<old_path>src/file.go</old_path>
<new_path>pkg/file.go
<explanation>Incomplete XML-like structure</explanation>
</move_file>`,
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
		{
			name: "Valid CreateDirectory - No Explanation",
			input: `<create_directory>
<path>simple/dir</path>
</create_directory>`,
			expected: CreateDirectory{
				Path: "simple/dir",
			},
			wantErr: false,
		},
		{
			name: "Invalid CreateDirectory - Empty Path",
			input: `<create_directory>
<path></path>
</create_directory>`,
			expected: CreateDirectory{},
			wantErr:  true,
		},
		{
			name: "Valid CreateDirectory - Multiple Paths (should use first)",
			input: `<create_directory>
<path>first/dir</path>
<path>second/dir</path>
</create_directory>`,
			expected: CreateDirectory{
				Path: "first/dir",
			},
			wantErr: false,
		},
		{
			name: "Invalid CreateDirectory - Malformed Structure",
			input: `<create_directory>
<path>malformed/dir
<explanation>Incomplete XML-like structure</explanation>
</create_directory>`,
			expected: CreateDirectory{},
			wantErr:  true,
		},
		{
			name: "Valid CreateDirectory - Nested Path",
			input: `<create_directory>
<path>parent/child/grandchild</path>
</create_directory>`,
			expected: CreateDirectory{
				Path: "parent/child/grandchild",
			},
			wantErr: false,
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

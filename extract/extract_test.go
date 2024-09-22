package extract

import (
	"reflect"
	"testing"
)

func TestGetModifyCode(t *testing.T) {
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
<edit>
<search>
<![CDATA[fmt.Println("Hello, World!")]]>
</search>
<replace>
<![CDATA[fmt.Println("Hello, Go!")]]>
</replace>
</edit>
<edit>
<search>
<![CDATA[var x int]]>
</search>
<replace>
<![CDATA[var x int32]]>
</replace>
</edit>
<explanation>Updated greeting and changed variable type</explanation>
</modify_code>`,
			expected: ModifyCode{
				Path: "main.go",
				Edits: []Edit{
					{Search: `fmt.Println("Hello, World!")`, Replace: `fmt.Println("Hello, Go!")`},
					{Search: "var x int", Replace: "var x int32"},
				},
				Explanation: "Updated greeting and changed variable type",
			},
			wantErr: false,
		},
		{
			name: "Invalid ModifyCode - Missing Path",
			input: `<modify_code>
<edit>
<search>
<![CDATA[fmt.Println("Hello, World!")]]>
</search>
<replace>
<![CDATA[fmt.Println("Hello, Go!")]]>
</replace>
</edit>
</modify_code>`,
			expected: ModifyCode{},
			wantErr:  true,
		},
		{
			name: "Invalid ModifyCode - No Edits",
			input: `<modify_code>
<path>main.go</path>
<explanation>No edits provided</explanation>
</modify_code>`,
			expected: ModifyCode{},
			wantErr:  true,
		},
		{
			name: "Valid ModifyCode - Multiple Paths (should use first)",
			input: `<modify_code>
<path>main.go</path>
<path>ignored.go</path>
<edit>
<search>
<![CDATA[fmt.Println("Hello, World!")]]>
</search>
<replace>
<![CDATA[fmt.Println("Hello, Go!")]]>
</replace>
</edit>
</modify_code>`,
			expected: ModifyCode{
				Path: "main.go",
				Edits: []Edit{
					{Search: `fmt.Println("Hello, World!")`, Replace: `fmt.Println("Hello, Go!")`},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getModifyCode(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("getModifyCode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("getModifyCode() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetRemoveFile(t *testing.T) {
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
			result, err := getRemoveFile(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("getRemoveFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("getRemoveFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetCreateFile(t *testing.T) {
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
<![CDATA[
package main

import "fmt"

func main() {
        fmt.Println("Hello, new file!")
}
]]>
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
			name: "Invalid CreateFile - Missing Path",
			input: `<create_file>
<content>
<![CDATA[package main]]>
</content>
</create_file>`,
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
<content>
<![CDATA[package main]]>
</content>
</create_file>`,
			expected: CreateFile{
				Path:    "simple.go",
				Content: "package main",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getCreateFile(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCreateFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("getCreateFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

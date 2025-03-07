package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestStruct struct {
	Name  string   `json:"name"`
	Age   int      `json:"age"`
	Items []string `json:"items"`
	Bool  bool     `json:"bool"`
}

type ArrayStruct struct {
	InputFiles []string `json:"input_files"`
}

func TestAutoCorrectJSON(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		targetObj  interface{}
		wantOutput string
		wantError  bool
	}{
		{
			name:       "Already valid JSON",
			input:      `{"name":"John","age":30}`,
			targetObj:  &TestStruct{},
			wantOutput: `{"name":"John","age":30}`,
			wantError:  false,
		},
		{
			name:       "Single quotes correction",
			input:      `{'name':'John','age':30}`,
			targetObj:  &TestStruct{},
			wantOutput: `{"name":"John","age":30}`,
			wantError:  false,
		},
		{
			name:       "Unquoted field names",
			input:      `{name:"John",age:30}`,
			targetObj:  &TestStruct{},
			wantOutput: `{"name":"John","age":30}`,
			wantError:  false,
		},
		{
			name:       "Mixed issues",
			input:      `{name:'John',age:30}`,
			targetObj:  &TestStruct{},
			wantOutput: `{"name":"John","age":30}`,
			wantError:  false,
		},
		{
			name:       "Array to string correction",
			input:      `{[input_files]:["file1.go","file2.go"]}`,
			targetObj:  &ArrayStruct{},
			wantOutput: `{"input_files":["file1.go","file2.go"]}`,
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := AutoCorrectJSON(tt.input, tt.targetObj)
			
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				
				// Verify that the corrected JSON can be unmarshaled
				err = json.Unmarshal([]byte(output), tt.targetObj)
				assert.NoError(t, err)
				
				// For simple cases, we can normalize and compare the JSON strings
				// For more complex cases, just the unmarshaling test above is sufficient
				if tt.wantOutput != "" {
					var expected, actual interface{}
					err1 := json.Unmarshal([]byte(tt.wantOutput), &expected)
					err2 := json.Unmarshal([]byte(output), &actual)
					
					if err1 == nil && err2 == nil {
						assert.Equal(t, expected, actual)
					}
				}
			}
		})
	}
}

func TestFixSingleQuotes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    `{'name':'John'}`,
			expected: `{"name":"John"}`,
		},
		{
			input:    `{"name":"John's"}`,
			expected: `{"name":"John's"}`,
		},
		{
			input:    `{'name':"John's"}`,
			expected: `{"name":"John's"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := fixSingleQuotes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFixUnquotedFieldNames(t *testing.T) {
	tests := []struct {
		input       string
		targetStruct interface{}
		expected    string
	}{
		{
			input:       `{name:"John",age:30}`,
			targetStruct: &TestStruct{},
			expected:    `{"name":"John","age":30}`,
		},
		{
			input:       `{name :"John",age :30}`,
			targetStruct: &TestStruct{},
			expected:    `{"name":"John","age":30}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := fixUnquotedFieldNames(tt.input, tt.targetStruct)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFixArraysToStrings(t *testing.T) {
	tests := []struct {
		input       string
		targetStruct interface{}
		expected    string
	}{
		{
			input:       `{[input_files]:["file1.go","file2.go"]}`,
			targetStruct: &ArrayStruct{},
			expected:    `{"input_files":["file1.go","file2.go"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := fixArraysToStrings(tt.input, tt.targetStruct)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetStructFieldNames(t *testing.T) {
	fields := getStructFieldNames(&TestStruct{})
	assert.ElementsMatch(t, []string{"name", "age", "items", "bool"}, fields)
}
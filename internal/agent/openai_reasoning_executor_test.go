package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testLogger struct{}

func (l *testLogger) Print(v ...any)                 {}
func (l *testLogger) Printf(format string, v ...any) {}
func (l *testLogger) Println(v ...any)               {}

func TestParseAndExecuteActions(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := t.TempDir()

	// Change to temp directory for relative path operations
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	defer os.Chdir(originalDir)

	// Create a test file for modification and deletion tests
	existingFile := filepath.Join(tempDir, "existing.txt")
	err = os.WriteFile(existingFile, []byte("original content"), 0644)
	require.NoError(t, err)

	executor := &openAiReasoningExecutor{
		logger: &testLogger{},
	}

	tests := []struct {
		name     string
		response string
		check    func(t *testing.T)
		wantErr  bool
	}{
		{
			name: "create file",
			response: `Some text before
<actions>
    <create path="new_file.txt"><![CDATA[
        Hello, World!
    ]]></create>
</actions>
Some text after`,
			check: func(t *testing.T) {
				content, err := os.ReadFile("new_file.txt")
				assert.NoError(t, err)
				assert.Contains(t, string(content), "Hello, World!")
			},
		},
		{
			name: "delete file",
			response: `<actions>
    <delete path="existing.txt"/>
</actions>`,
			check: func(t *testing.T) {
				_, err := os.Stat("existing.txt")
				assert.True(t, os.IsNotExist(err))
			},
		},
		{
			name: "modify file",
			response: `<actions>
    <modify path="existing.txt">
        <search>original content</search>
        <replace>modified content</replace>
    </modify>
</actions>`,
			check: func(t *testing.T) {
				content, err := os.ReadFile("existing.txt")
				assert.NoError(t, err)
				assert.Equal(t, "modified content", string(content))
			},
		},
		{
			name: "multiple actions",
			response: `<actions>
    <create path="multi1.txt"><![CDATA[file 1]]></create>
    <create path="multi2.txt"><![CDATA[file 2]]></create>
    <modify path="existing.txt">
        <search>original content</search>
        <replace>multi modified</replace>
    </modify>
</actions>`,
			check: func(t *testing.T) {
				content1, err := os.ReadFile("multi1.txt")
				assert.NoError(t, err)
				assert.Equal(t, "file 1", string(content1))

				content2, err := os.ReadFile("multi2.txt")
				assert.NoError(t, err)
				assert.Equal(t, "file 2", string(content2))

				content3, err := os.ReadFile("existing.txt")
				assert.NoError(t, err)
				assert.Equal(t, "multi modified", string(content3))
			},
		},
		{
			name:     "no actions",
			response: "Just some text without any actions",
			check: func(t *testing.T) {
				// Nothing should happen
			},
		},
		{
			name: "invalid XML",
			response: `<actions>
    <create path="test.txt">
    <unclosed-tag>
</actions>`,
			wantErr: true,
			check: func(t *testing.T) {
				// File should not be created
				_, err := os.Stat("test.txt")
				assert.True(t, os.IsNotExist(err))
			},
		},
		{
			name: "nested directory creation",
			response: `<actions>
    <create path="nested/dir/file.txt"><![CDATA[nested file content]]></create>
</actions>`,
			check: func(t *testing.T) {
				content, err := os.ReadFile("nested/dir/file.txt")
				assert.NoError(t, err)
				assert.Equal(t, "nested file content", string(content))
			},
		},
		{
			name: "modify nonexistent file",
			response: `<actions>
    <modify path="nonexistent.txt">
        <search>something</search>
        <replace>new text</replace>
    </modify>
</actions>`,
			check: func(t *testing.T) {
				// The error is logged but not returned
				_, err := os.Stat("nonexistent.txt")
				assert.True(t, os.IsNotExist(err))
			},
		},
		{
			name: "delete nonexistent file",
			response: `<actions>
    <delete path="nonexistent.txt"/>
</actions>`,
			check: func(t *testing.T) {
				// The error is logged but not returned
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset test file for each test
			err := os.WriteFile(existingFile, []byte("original content"), 0644)
			require.NoError(t, err)

			err = executor.parseAndExecuteActions(tt.response)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			tt.check(t)
		})
	}
}

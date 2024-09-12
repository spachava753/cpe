package repomap

import (
	"github.com/stretchr/testify/assert"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRepoMap(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		expected string
	}{
		{
			name: "Single file with struct and function",
			files: map[string]string{
				"main.go": `
package main

import (
	"fmt"
	"os"
)

type User struct {
	Name string
	Age  int
}

func main() {
	fmt.Println("Hello, World!")
}
`,
			},
			expected: `<repo_map>
<file>
<path>main.go</path>
<file_map>
package main
import (
 "fmt"
 "os"
)
type User struct {
    Name string
    Age int
}
func main() ()
</file_map>
</file>
</repo_map>
`,
		},
		{
			name: "Multiple files with different structures",
			files: map[string]string{
				"main.go": `
package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`,
				"user/user.go": `
package user

type User struct {
	ID   int
	Name string
}

func NewUser(name string) *User {
	return &User{Name: name}
}
`,
			},
			expected: `<repo_map>
<file>
<path>main.go</path>
<file_map>
package main
import (
 "fmt"
)
func main() ()
</file_map>
</file>
<file>
<path>user/user.go</path>
<file_map>
package user
type User struct {
    ID int
    Name string
}
func NewUser(name string) (*User)
</file_map>
</file>
</repo_map>
`,
		},
		{
			name: "File with interface and multiple functions",
			files: map[string]string{
				"service.go": `
package service

import (
	"context"
	"time"
)

type Service interface {
	Get(ctx context.Context, id string) (string, error)
	Create(ctx context.Context, data string) error
}

func NewService(timeout time.Duration) Service {
	return &serviceImpl{timeout: timeout}
}

func (s *serviceImpl) Get(ctx context.Context, id string) (string, error) {
	// Implementation
}

func (s *serviceImpl) Create(ctx context.Context, data string) error {
	// Implementation
}
`,
			},
			expected: `<repo_map>
<file>
<path>service.go</path>
<file_map>
package service
import (
 "context"
 "time"
)
type Service interface {
    Get(ctx context.Context, id string) (string, error)
    Create(ctx context.Context, data string) (error)
}
func NewService(timeout time.Duration) (Service)
func (s *serviceImpl) Get(ctx context.Context, id string) (string, error)
func (s *serviceImpl) Create(ctx context.Context, data string) (error)
</file_map>
</file>
</repo_map>
`,
		},
		{
			name: "File with nested structs and complex types",
			files: map[string]string{
				"complex.go": `
package complex

import "sync"

type Outer struct {
	Inner struct {
		Field1 string
		Field2 int
	}
	Map   map[string]interface{}
	Slice []int
}

type GenericType[T any] struct {
	Data T
}

func ProcessData(data *sync.Map) ([]byte, error) {
	// Implementation
}
`,
			},
			expected: `<repo_map>
<file>
<path>complex.go</path>
<file_map>
package complex
import (
 "sync"
)
type Outer struct {
    Inner struct
    Map map[string]interface{}
    Slice []int
}
type GenericType[T any] struct {
    Data T
}
func ProcessData(data *sync.Map) ([]byte, error)
</file_map>
</file>
</repo_map>
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the in-memory file system
			memFS := setupInMemoryFS(tt.files)

			// Parse the repository
			repoMap, err := ParseRepo(memFS)
			if err != nil {
				t.Fatalf("Failed to parse repo: %v", err)
			}

			// Generate the output
			output := repoMap.GenerateOutput()

			// Compare the output with the expected result
			if !assert.Equal(t, normalizeWhitespace(tt.expected), normalizeWhitespace(output)) {
				t.Errorf("Unexpected output.\nExpected:\n%s\nGot:\n%s", tt.expected, output)
			}
		})
	}
}

// normalizeWhitespace removes leading/trailing whitespace and normalizes newlines
func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n")
}

func setupInMemoryFS(files map[string]string) fs.FS {
	memFS := fstest.MapFS{}
	for filename, content := range files {
		memFS[filename] = &fstest.MapFile{
			Data: []byte(content),
		}
	}
	return memFS
}

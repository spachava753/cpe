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
			name: "Comprehensive test case",
			files: map[string]string{
				"main.go": `
// Package main is the entry point of the application.
package main

import (
	"fmt"
	"strings"
)

const (
	MaxUsers = 100
	Version  = "1.0.0"
)

var (
	Debug = false
	LogLevel = "info"
)

// User represents a user in the system.
type User struct {
	// ID is the unique identifier for the user.
	ID   int
	// Name is the user's full name.
	Name string
}

// UserRole is an alias for string representing user roles.
type UserRole = string

// Greeter defines the interface for greeting users.
type Greeter interface {
	// Greet returns a greeting message for the given name.
	Greet(name string) string
}

// SimpleGreeter implements the Greeter interface.
type SimpleGreeter struct{}

// Greet returns a simple greeting message.
func (sg *SimpleGreeter) Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

// main is the entry point of the application.
func main() {
	fmt.Println("Starting application...")
}

// CreateUser creates a new user with the given name.
func CreateUser(name string) *User {
	return &User{Name: name}
}
`,
			},
			expected: `<repo_map>
<file>
<path>main.go</path>
<file_map>
// Package main is the entry point of the application.
package main
import (
 "fmt"
 "strings"
)
const (
 MaxUsers = 100
 Version = "1.0.0"
)
var (
 Debug = false
 LogLevel = "info"
)
// User represents a user in the system.
type User struct {
    // ID is the unique identifier for the user.
    ID int
    // Name is the user's full name.
    Name string
}
type UserRole = string
// Greeter defines the interface for greeting users.
type Greeter interface {
    // Greet returns a greeting message for the given name.
    Greet(name string) (string)
}
// SimpleGreeter implements the Greeter interface.
type SimpleGreeter struct {
}
// Greet returns a simple greeting message.
func (sg *SimpleGreeter) Greet(name string) (string)
// main is the entry point of the application.
func main() ()
// CreateUser creates a new user with the given name.
func CreateUser(name string) (*User)
</file_map>
</file>
</repo_map>
`,
		},
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
		{
			name: "File with comments at various levels",
			files: map[string]string{
				"comments.go": `
// Package comments demonstrates various levels of comments in Go code.
package comments

// User represents a user in the system.
// It contains basic information about the user.
type User struct {
	// ID is the unique identifier for the user.
	ID   int
	// Name is the user's full name.
	Name string
}

// Admin represents an administrator in the system.
// It extends the User type with additional permissions.
type Admin struct {
	User
	// Permissions is a list of granted permissions.
	Permissions []string
}

// NewUser creates a new User with the given name.
func NewUser(name string) *User {
	return &User{Name: name}
}
`,
			},
			expected: `<repo_map>
<file>
<path>comments.go</path>
<file_map>
// Package comments demonstrates various levels of comments in Go code.
package comments
// User represents a user in the system.
// It contains basic information about the user.
type User struct {
    // ID is the unique identifier for the user.
    ID int
    // Name is the user's full name.
    Name string
}
// Admin represents an administrator in the system.
// It extends the User type with additional permissions.
type Admin struct {
    User
    // Permissions is a list of granted permissions.
    Permissions []string
}
// NewUser creates a new User with the given name.
func NewUser(name string) (*User)
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

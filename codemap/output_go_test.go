package codemap

import (
	"github.com/stretchr/testify/assert"
	"io/fs"
	"testing"
	"testing/fstest"
)

func setupInMemoryFS(files map[string]string) fs.FS {
	memFS := fstest.MapFS{}
	for filename, content := range files {
		memFS[filename] = &fstest.MapFile{
			Data: []byte(content),
		}
	}
	return memFS
}

func TestGenerateOutput(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		maxLen   int
		expected string
	}{
		{
			name:   "Comprehensive string literal truncation test",
			maxLen: 10,
			files: map[string]string{
				"globals.go": `
package globals

import "time"

// Single constant declaration
const singleConst = "This is a long single constant"

// Grouped constant declaration
const (
	groupedConst1 = "First grouped constant"
	groupedConst2 = "Second grouped constant"
	groupedConst3 = ` + "`" + `Third grouped constant
	with multiple lines` + "`" + `
)

// Single variable declaration
var singleVar = "This is a long single variable"

// Grouped variable declaration
var (
	groupedVar1 = "First grouped variable"
	groupedVar2 = "Second grouped variable"
	groupedVar3 = ` + "`" + `Third grouped variable
	with multiple lines` + "`" + `
)

// Constants with different types
const (
	intConst    = 42
	floatConst  = 3.14
	boolConst   = true
	runeConst   = 'A'
	stringConst = "Regular string constant"
)

// Variables with different types
var (
	intVar    = 42
	floatVar  = 3.14
	boolVar   = true
	runeVar   = 'A'
	stringVar = "Regular string variable"
)

// Byte slice constants
const (
	byteSliceConst1 = []byte("Byte slice constant")
	byteSliceConst2 = []byte(` + "`" + `Raw byte slice constant` + "`" + `)
)

func generateId() string {
	return "not-a-real-id"
}

// Byte slice variables
var (
	byteSliceVar1 = []byte("Byte slice variable")
	byteSliceVar2 = []byte(` + "`" + `Raw byte slice variable` + "`" + `)
)

// Complex type constants
const (
	complexConst1 = complex(1, 2)
	complexConst2 = 3 + 4i
)

// Complex type variables
var (
	complexVar1 = complex(1, 2)
	complexVar2 = 3 + 4i
)

// Constant expressions
const (
	constExpr1 = len("Hello, World!")
	constExpr2 = 60 * 60 * 24 // Seconds in a day
)

// Variable with type
var typedVar string = "This is a typed variable"

// Variables with initializer functions
var (
	timeVar = time.Now()
	uuidVar = generateUUID()
)

func generateUUID() string {
	return "not-a-real-uuid"
}
`,
			},
			expected: `<code_map>
<file>
<path>globals.go</path>
<file_map>
package globals

import "time"

// Single constant declaration
const singleConst = "This is a ..."

// Grouped constant declaration
const (
	groupedConst1 = "First grou..."
	groupedConst2 = "Second gro..."
	groupedConst3 = ` + "`" + `Third grou...` + "`" + `
)

// Single variable declaration
var singleVar = "This is a ..."

// Grouped variable declaration
var (
	groupedVar1 = "First grou..."
	groupedVar2 = "Second gro..."
	groupedVar3 = ` + "`" + `Third grou...` + "`" + `
)

// Constants with different types
const (
	intConst    = 42
	floatConst  = 3.14
	boolConst   = true
	runeConst   = 'A'
	stringConst = "Regular st..."
)

// Variables with different types
var (
	intVar    = 42
	floatVar  = 3.14
	boolVar   = true
	runeVar   = 'A'
	stringVar = "Regular st..."
)

// Byte slice constants
const (
	byteSliceConst1 = []byte("Byte slice...")
	byteSliceConst2 = []byte(` + "`" + `Raw byte s...` + "`" + `)
)

func generateId() string

// Byte slice variables
var (
	byteSliceVar1 = []byte("Byte slice...")
	byteSliceVar2 = []byte(` + "`" + `Raw byte s...` + "`" + `)
)

// Complex type constants
const (
	complexConst1 = complex(1, 2)
	complexConst2 = 3 + 4i
)

// Complex type variables
var (
	complexVar1 = complex(1, 2)
	complexVar2 = 3 + 4i
)

// Constant expressions
const (
	constExpr1 = len("Hello, Wor...")
	constExpr2 = 60 * 60 * 24 // Seconds in a day
)

// Variable with type
var typedVar string = "This is a ..."

// Variables with initializer functions
var (
	timeVar = time.Now()
	uuidVar = generateUUID()
)

func generateUUID() string
</file_map>
</file>
</code_map>
`,
		},
		{
			name:   "Global variables truncation",
			maxLen: 1,
			files: map[string]string{
				"globals.go": `
package globals

var (
	LongString = "Text"

	LongStringBackTick = ` + "`" + `Text` + "`" + `

	LongByteSlice = []byte("Text")

	LongByteSliceBackTick = []byte(` + "`" + `Text` + "`" + `)

	ShortString = "T"

	RegularVar = 42
)
`,
			},
			expected: `<code_map>
<file>
<path>globals.go</path>
<file_map>
package globals

var (
	LongString = "T..."

	LongStringBackTick = ` + "`" + `T...` + "`" + `

	LongByteSlice = []byte("T...")

	LongByteSliceBackTick = []byte(` + "`" + `T...` + "`" + `)

	ShortString = "T"

	RegularVar = 42
)
</file_map>
</file>
</code_map>
`,
		},
		{
			name:   "Comprehensive comments test",
			maxLen: 500,
			files: map[string]string{
				"comments.go": `
// Package comments demonstrates comprehensive comment usage in Go.
package comments

import "fmt"

// UserRole represents the role of a user in the system.
type UserRole string

// Predefined user roles.
const (
	// AdminRole represents an administrator user.
	AdminRole UserRole = "admin"
	// RegularRole represents a regular user.
	RegularRole UserRole = "regular"
)

// Config holds the application configuration.
type Config struct {
	// Debug enables debug mode when true.
	Debug bool
	// LogLevel sets the logging level.
	LogLevel string
}

// User represents a user in the system.
type User struct {
	// ID is the unique identifier for the user.
	ID int
	// Name is the user's full name.
	Name string
	// Role defines the user's role in the system.
	Role UserRole
}

// UserManager defines the interface for managing users.
type UserManager interface {
	// CreateUser creates a new user with the given name and role.
	// Returns the created user and any error encountered.
	CreateUser(name string, role UserRole) (*User, error)

	// GetUser retrieves a user by their ID.
	// Returns the user if found, or an error if not found or any other issue occurs.
	GetUser(id int) (*User, error)
}

// DefaultUserManager is the default implementation of UserManager.
type DefaultUserManager struct {
	// users is a map of user IDs to User objects.
	users map[int]*User
}

// CreateUser implements UserManager.CreateUser.
func (um *DefaultUserManager) CreateUser(name string, role UserRole) (*User, error) {
	// Implementation details...
	return nil, nil
}

// GetUser implements UserManager.GetUser.
func (um *DefaultUserManager) GetUser(id int) (*User, error) {
	// Implementation details...
	return nil, nil
}

// NewDefaultUserManager creates a new instance of DefaultUserManager.
func NewDefaultUserManager() *DefaultUserManager {
	return &DefaultUserManager{
		users: make(map[int]*User),
	}
}
`,
			},
			expected: `<code_map>
<file>
<path>comments.go</path>
<file_map>
// Package comments demonstrates comprehensive comment usage in Go.
package comments

import "fmt"

// UserRole represents the role of a user in the system.
type UserRole string

// Predefined user roles.
const (
	// AdminRole represents an administrator user.
	AdminRole UserRole = "admin"
	// RegularRole represents a regular user.
	RegularRole UserRole = "regular"
)

// Config holds the application configuration.
type Config struct {
	// Debug enables debug mode when true.
	Debug bool
	// LogLevel sets the logging level.
	LogLevel string
}

// User represents a user in the system.
type User struct {
	// ID is the unique identifier for the user.
	ID int
	// Name is the user's full name.
	Name string
	// Role defines the user's role in the system.
	Role UserRole
}

// UserManager defines the interface for managing users.
type UserManager interface {
	// CreateUser creates a new user with the given name and role.
	// Returns the created user and any error encountered.
	CreateUser(name string, role UserRole) (*User, error)

	// GetUser retrieves a user by their ID.
	// Returns the user if found, or an error if not found or any other issue occurs.
	GetUser(id int) (*User, error)
}

// DefaultUserManager is the default implementation of UserManager.
type DefaultUserManager struct {
	// users is a map of user IDs to User objects.
	users map[int]*User
}

// CreateUser implements UserManager.CreateUser.
func (um *DefaultUserManager) CreateUser(name string, role UserRole) (*User, error)

// GetUser implements UserManager.GetUser.
func (um *DefaultUserManager) GetUser(id int) (*User, error)

// NewDefaultUserManager creates a new instance of DefaultUserManager.
func NewDefaultUserManager() *DefaultUserManager
</file_map>
</file>
</code_map>
`,
		},
		{
			name:   "Including test files",
			maxLen: 500,
			files: map[string]string{
				"main.go": `
package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`,
				"utils/helper.go": `
package utils

func Add(a, b int) int {
	return a + b
}
`,
				"utils/helper_test.go": `
package utils

import "testing"

func TestAdd(t *testing.T) {
	result := Add(2, 3)
	if result != 5 {
		t.Errorf("Expected 5, got %d", result)
	}
}
`,
			},
			expected: `<code_map>
<file>
<path>main.go</path>
<file_map>
package main

import "fmt"

func main()
</file_map>
</file>
<file>
<path>utils/helper.go</path>
<file_map>
package utils

func Add(a, b int) int
</file_map>
</file>
<file>
<path>utils/helper_test.go</path>
<file_map>
package utils

import "testing"

func TestAdd(t *testing.T)
</file_map>
</file>
</code_map>
`,
		},
		{
			name:   "Comprehensive test case",
			maxLen: 500,
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
			expected: `<code_map>
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
	Version  = "1.0.0"
)

var (
	Debug    = false
	LogLevel = "info"
)

// User represents a user in the system.
type User struct {
	// ID is the unique identifier for the user.
	ID int
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
func (sg *SimpleGreeter) Greet(name string) string

// main is the entry point of the application.
func main()

// CreateUser creates a new user with the given name.
func CreateUser(name string) *User
</file_map>
</file>
</code_map>
`,
		},
		{
			name:   "Single file with struct and function",
			maxLen: 500,
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
			expected: `<code_map>
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
	Age  int
}

func main()
</file_map>
</file>
</code_map>
`,
		},
		{
			name:   "Multiple files with different structures",
			maxLen: 500,
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
			expected: `<code_map>
<file>
<path>main.go</path>
<file_map>
package main

import "fmt"

func main()
</file_map>
</file>
<file>
<path>user/user.go</path>
<file_map>
package user

type User struct {
	ID   int
	Name string
}

func NewUser(name string) *User
</file_map>
</file>
</code_map>
`,
		},
		{
			name:   "File with interface and multiple functions",
			maxLen: 500,
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

type serviceImpl struct {
	timeout time.Duration
}

func (s *serviceImpl) Get(ctx context.Context, id string) (string, error) {
	// Implementation
	return "", nil
}

func (s *serviceImpl) Create(ctx context.Context, data string) error {
	// Implementation
	return nil
}
`,
			},
			expected: `<code_map>
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
	Create(ctx context.Context, data string) error
}

func NewService(timeout time.Duration) Service

type serviceImpl struct {
	timeout time.Duration
}

func (s *serviceImpl) Get(ctx context.Context, id string) (string, error)

func (s *serviceImpl) Create(ctx context.Context, data string) error
</file_map>
</file>
</code_map>
`,
		},
		{
			name:   "File with nested structs and complex types",
			maxLen: 500,
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
	return nil, nil
}
`,
			},
			expected: `<code_map>
<file>
<path>complex.go</path>
<file_map>
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

func ProcessData(data *sync.Map) ([]byte, error)
</file_map>
</file>
</code_map>
`,
		},
		{
			name:   "File with comments at various levels",
			maxLen: 500,
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
			expected: `<code_map>
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
func NewUser(name string) *User
</file_map>
</file>
</code_map>
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the in-memory file system
			memFS := setupInMemoryFS(tt.files)

			// Generate the output using GenerateOutput
			output, err := GenerateOutput(memFS, tt.maxLen)
			if err != nil {
				t.Fatalf("Failed to generate output: %v", err)
			}

			// Compare the output with the expected result
			if !assert.Equal(t, tt.expected, output) {
				t.Errorf("Unexpected output.\nExpected:\n%s\nGot:\n%s", tt.expected, output)
			}
		})
	}
}

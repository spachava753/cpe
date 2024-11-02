package codemap

import (
	"github.com/spachava753/cpe/internal/ignore"
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
		expected []FileCodeMap
	}{
		{
			name:   "Function returning another function",
			maxLen: 500,
			files: map[string]string{
				"higher_order.go": `
package higherorder

// GreeterFactory returns a function that greets a person
func GreeterFactory(greeting string) func(name string) string {
	return func(name string) string {
		return greeting + ", " + name + "!"
	}
}

// UseGreeter demonstrates the usage of GreeterFactory
func UseGreeter() {
	greet := GreeterFactory("Hello")
	message := greet("Alice")
	println(message)
}

var testFunc = func(a int, b int) int {
	return 0
}
`,
			},
			expected: []FileCodeMap{
				{
					Path: "higher_order.go",
					Content: `package higherorder

// GreeterFactory returns a function that greets a person
func GreeterFactory(greeting string) func(name string) string

// UseGreeter demonstrates the usage of GreeterFactory
func UseGreeter()

var testFunc = func(a int, b int) int {
	return 0
}`,
				},
			},
		},
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
			expected: []FileCodeMap{
				{
					Path: "globals.go",
					Content: `package globals

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

func generateUUID() string`,
				},
			},
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
			expected: []FileCodeMap{
				{
					Path: "globals.go",
					Content: `package globals

var (
	LongString = "T..."

	LongStringBackTick = ` + "`" + `T...` + "`" + `

	LongByteSlice = []byte("T...")

	LongByteSliceBackTick = []byte(` + "`" + `T...` + "`" + `)

	ShortString = "T"

	RegularVar = 42
)`,
				},
			},
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
			expected: []FileCodeMap{
				{
					Path: "comments.go",
					Content: `// Package comments demonstrates comprehensive comment usage in Go.
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
func NewDefaultUserManager() *DefaultUserManager`,
				},
			},
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
			expected: []FileCodeMap{
				{
					Path: "main.go",
					Content: `package main

import "fmt"

func main()`,
				},
				{
					Path: "utils/helper.go",
					Content: `package utils

func Add(a, b int) int`,
				},
				{
					Path: "utils/helper_test.go",
					Content: `package utils

import "testing"

func TestAdd(t *testing.T)`,
				},
			},
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
			expected: []FileCodeMap{
				{
					Path: "main.go",
					Content: `// Package main is the entry point of the application.
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
func CreateUser(name string) *User`,
				},
			},
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
			expected: []FileCodeMap{
				{
					Path: "main.go",
					Content: `package main

import (
	"fmt"
	"os"
)

type User struct {
	Name string
	Age  int
}

func main()`,
				},
			},
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
			expected: []FileCodeMap{
				{
					Path: "main.go",
					Content: `package main

import "fmt"

func main()`,
				},
				{
					Path: "user/user.go",
					Content: `package user

type User struct {
	ID   int
	Name string
}

func NewUser(name string) *User`,
				},
			},
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
			expected: []FileCodeMap{
				{
					Path: "service.go",
					Content: `package service

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

func (s *serviceImpl) Create(ctx context.Context, data string) error`,
				},
			},
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
			expected: []FileCodeMap{
				{
					Path: "complex.go",
					Content: `package complex

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

func ProcessData(data *sync.Map) ([]byte, error)`,
				},
			},
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
			expected: []FileCodeMap{
				{
					Path: "comments.go",
					Content: `// Package comments demonstrates various levels of comments in Go code.
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
func NewUser(name string) *User`,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the in-memory file system
			memFS := setupInMemoryFS(tt.files)

			// Generate the output using GenerateOutput
			ignoreRules := ignore.NewIgnoreRules()
			output, err := GenerateOutput(memFS, tt.maxLen, ignoreRules)
			if err != nil {
				t.Fatalf("Failed to generate output: %v", err)
			}

			// Compare the output with the expected result
			if !assert.Equal(t, tt.expected, output) {
				t.Errorf("Unexpected output.\nExpected:\n%v\nGot:\n%v", tt.expected, output)
			}
		})
	}
}

# CPE Testing Strategy Specification

This document outlines the testing philosophy, patterns, and best practices used throughout the CPE codebase.

## Overview

CPE employs a comprehensive testing strategy focused on:
- **Correctness over performance** - Tests verify behavior, not micro-optimizations
- **Determinism** - Tests avoid flaky behavior through proper timeout handling and isolation
- **Maintainability** - Table-driven tests reduce duplication and improve readability
- **Snapshot-based verification** - Complex outputs are validated using snapshot testing

The testing approach prioritizes unit tests with strategic integration tests for critical paths like HTTP communication and database operations.

## Test Paradigms

### Table-Driven Tests

Table-driven tests are the preferred pattern throughout CPE. They provide:
- Clear enumeration of test cases
- Shared setup/validation logic
- Descriptive test case names
- Easy extension with new cases

#### Basic Structure

```go
func TestFeature(t *testing.T) {
    tests := []struct {
        name      string
        input     SomeInput
        want      string
        wantErr   bool
    }{
        {
            name:    "valid input produces expected output",
            input:   SomeInput{Field: "value"},
            want:    "expected",
            wantErr: false,
        },
        {
            name:    "invalid input returns error",
            input:   SomeInput{Field: ""},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Feature(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Feature() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("Feature() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

#### Real Example: Collision Detection Tests

From `internal/codemode/collision_test.go`:

```go
func TestCheckPascalCaseCollision(t *testing.T) {
    tests := []struct {
        name      string
        toolNames []string
        wantErr   bool
    }{
        {
            name:      "no collision with empty list",
            toolNames: []string{},
            wantErr:   false,
        },
        {
            name:      "no collision with different tools",
            toolNames: []string{"get_weather", "get_city", "read_file"},
            wantErr:   false,
        },
        {
            name:      "collision with underscore vs camelCase",
            toolNames: []string{"get_weather", "getWeather"},
            wantErr:   true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := CheckPascalCaseCollision(tt.toolNames)
            if (err != nil) != tt.wantErr {
                t.Errorf("CheckPascalCaseCollision() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if err != nil {
                cupaloy.SnapshotT(t, err.Error())
            }
        })
    }
}
```

### Snapshot Testing

CPE uses the `cupaloy` library for snapshot testing. Snapshots capture complex outputs (error messages, generated code, structured data) and automatically detect regressions.

#### How Snapshots Work

1. First run: `cupaloy.SnapshotT(t, result)` creates `.snapshots/TestName-case_name` file
2. Subsequent runs: Output is compared against the stored snapshot
3. Updates: Run `UPDATE_SNAPSHOTS=true go test ./...` to regenerate snapshots

#### Snapshot Directory Structure

Each package with snapshot tests has a `.snapshots` directory:
```
internal/codemode/.snapshots/
  TestSchemaToGoType-nil_schema_returns_string_alias
  TestSchemaToGoType-simple_object_with_string_field_(optional)
  TestCheckReservedNameCollision-collision_with_execute_go_code
internal/storage/.snapshots/
  TestStoreAndRetrieveMessages
  TestBranchingDialogs
```

#### When to Use Snapshots

- Generated code output (Go types from JSON schema)
- Error message formatting
- Complex structured data
- Multi-line string outputs

```go
// Good: Snapshot for generated Go type definitions
func TestSchemaToGoType(t *testing.T) {
    got, err := SchemaToGoType(schema, typeName)
    if err != nil {
        t.Fatal(err)
    }
    cupaloy.SnapshotT(t, got)
}

// Good: Snapshot for structured results with deterministic fields
cupaloy.SnapshotT(t, struct {
    Data        string
    ContentType string
}{
    Data:        string(result.Data),
    ContentType: result.ContentType,
})
```

### Test Organization Conventions

Tests follow these conventions:

1. **File naming**: `*_test.go` in the same package as the code being tested
2. **Package proximity**: Tests live alongside implementation files
3. **Helper functions**: Defined at the top of test files, prefixed with lowercase names indicating test-only usage
4. **Test name format**: `Test<FunctionName>_<Scenario>`

## Test Categories

### Unit Tests

Unit tests verify individual functions and methods in isolation.

**Characteristics:**
- No external dependencies (network, filesystem state)
- Fast execution (milliseconds)
- Deterministic results
- Mock external interfaces

**Example: Schema Conversion Unit Test**

```go
func TestSchemaToGoType(t *testing.T) {
    tests := []struct {
        name       string
        schemaJSON string
        typeName   string
        wantErr    bool
    }{
        {
            name: "object with enum field",
            schemaJSON: `{
                "type": "object",
                "properties": {
                    "unit": {
                        "type": "string",
                        "enum": ["celsius", "fahrenheit"]
                    }
                }
            }`,
            typeName: "WeatherInput",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var schema *jsonschema.Schema
            if tt.schemaJSON != "" {
                schema = &jsonschema.Schema{}
                json.Unmarshal([]byte(tt.schemaJSON), schema)
            }

            got, err := SchemaToGoType(schema, tt.typeName)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            cupaloy.SnapshotT(t, got)
        })
    }
}
```

### Integration Tests

Integration tests verify interactions between components or with external systems.

**Characteristics:**
- May involve actual HTTP servers (via `httptest`)
- May use in-memory databases (SQLite `:memory:`)
- Test real process execution (code mode execution)
- Longer execution time (seconds)

**Example: HTTP Server Integration**

From `internal/urlhandler/urlhandler_test.go`:

```go
func TestDownloadContent(t *testing.T) {
    tests := []struct {
        name          string
        serverHandler http.HandlerFunc
        config        *DownloadConfig
        wantErr       bool
        errMsg        string
    }{
        {
            name: "successful download",
            serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/plain")
                w.WriteHeader(http.StatusOK)
                w.Write([]byte("Hello, World!"))
            }),
            config:  testConfig(),
            wantErr: false,
        },
        // ... more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            server := httptest.NewServer(tt.serverHandler)
            defer server.Close()

            ctx := context.Background()
            result, err := DownloadContent(ctx, server.URL, tt.config)

            if tt.wantErr {
                // Verify error message
            } else {
                snapshot := downloadResultSnapshot{
                    Data:        string(result.Data),
                    ContentType: result.ContentType,
                    Size:        result.Size,
                }
                cupaloy.SnapshotT(t, snapshot)
            }
        })
    }
}
```

**Example: Code Execution Integration**

From `internal/codemode/executor_test.go`, the `TestExecuteCode` table-driven test covers execution scenarios:

```go
func TestExecuteCode(t *testing.T) {
    tests := []struct {
        name          string
        llmCode       string
        wantExitCode  int
        wantErrType   string // "none", "recoverable", "fatal", "other"
        validate      func(t *testing.T, result ExecutionResult, err error)
        snapshot      bool
    }{
        {
            name: "successful execution",
            llmCode: `package main

import (
    "context"
    "fmt"

    "github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
    fmt.Println("Hello from generated code")
    return nil, nil
}
`,
            wantExitCode: 0,
            wantErrType:  "none",
            snapshot:     true,
        },
        // ... more test cases for compilation errors, panics, etc.
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := ExecuteCode(ctx, nil, tt.llmCode, 30)
            // Validate exit code and error type...
            if tt.snapshot {
                cupaloy.SnapshotT(t, result.Output)
            }
        })
    }
}
```

### Manual/E2E Tests

Some scenarios require manual verification or full end-to-end testing.

**When Manual Testing is Appropriate:**
- LLM response quality assessment
- Visual output formatting
- Interactive conversation flows
- Cross-process MCP communication

**Documentation in Tests:**

For tests that have manual verification aspects, document the manual steps in comments. For example, `internal/subagentlog/server_test.go` has `TestServerGracefulShutdown` which tests context cancellation:

```go
func TestServerGracefulShutdown(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())

    server := NewServer(nil)
    address, err := server.Start(ctx)
    // ... verify server is running ...

    // Cancel context to trigger shutdown
    cancel()
    time.Sleep(100 * time.Millisecond)

    // Server should no longer be reachable
    _, err = client.Get(address + "/subagent-events")
    if err == nil {
        t.Error("server should not be reachable after shutdown")
    }
}
```

## Testing Utilities

### Test Helper Functions

Common patterns are extracted into helper functions within test files. These are typically package-private and defined in the test file where they are used.

**Example: Storage test helpers (`internal/storage/dialog_storage_test.go`)**

```go
// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) (*sql.DB, *DialogStorage) {
    db, err := sql.Open("sqlite3", ":memory:")
    require.NoError(t, err, "Failed to open in-memory database")

    _, err = db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON;")
    require.NoError(t, err, "Failed to enable foreign keys")

    _, err = db.ExecContext(context.Background(), schemaSQL)
    require.NoError(t, err, "Failed to create schema")

    storage := &DialogStorage{
        db:          db,
        q:           New(db),
        idGenerator: generateId,
    }
    return db, storage
}

// normalizeMessage clears IDs for snapshot comparison
func normalizeMessage(msg gai.Message) gai.Message {
    normalized := gai.Message{
        Role:            msg.Role,
        ToolResultError: msg.ToolResultError,
    }
    normalized.Blocks = make([]gai.Block, len(msg.Blocks))
    for i, block := range msg.Blocks {
        normalized.Blocks[i] = gai.Block{
            ID:           "", // Clear the ID
            BlockType:    block.BlockType,
            ModalityType: block.ModalityType,
            MimeType:     block.MimeType,
            Content:      block.Content,
        }
    }
    return normalized
}
```

**Example: Subagent test helpers (`internal/commands/subagent_test.go`)**

```go
// mustToolCallBlock creates a tool call block for testing, fails test on error
func mustToolCallBlock(t *testing.T, id, toolName string, params map[string]any) gai.Block {
    t.Helper()
    block, err := gai.ToolCallBlock(id, toolName, params)
    if err != nil {
        t.Fatalf("failed to create tool call block: %v", err)
    }
    return block
}
```

### Assertion Libraries

CPE uses `github.com/stretchr/testify` for assertions:

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestExample(t *testing.T) {
    // require: fails immediately if assertion fails
    require.NoError(t, err, "setup should not fail")

    // assert: records failure but continues execution
    assert.Equal(t, expected, actual, "values should match")
    assert.Len(t, items, 3, "should have 3 items")
    assert.Contains(t, err.Error(), "has children")
}
```

## Mocking Strategy

### Interface-Based Mocking

CPE uses interfaces to enable mocking of external dependencies. Mock types are typically defined in the test file where they are used. Here are examples from `internal/commands/generate_test.go` and `internal/commands/subagent_test.go`:

```go
// mockToolCapableGenerator is a test implementation of ToolCapableGenerator
// From internal/commands/generate_test.go
type mockToolCapableGenerator struct {
    result gai.Dialog
    err    error
}

func (m *mockToolCapableGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
    if m.err != nil {
        return m.result, m.err
    }
    result := append(dialog, gai.Message{
        Role: gai.Assistant,
        Blocks: []gai.Block{
            {
                BlockType:    gai.Content,
                ModalityType: gai.Text,
                MimeType:     "text/plain",
                Content:      gai.Str("Generated response"),
            },
        },
    })
    return result, nil
}

// mockDialogStorage is a test implementation of DialogStorage
// From internal/commands/generate_test.go
type mockDialogStorage struct {
    mostRecentID   string
    mostRecentErr  error
    dialog         gai.Dialog
    msgIDList      []string
    getDialogErr   error
    savedMessages  []gai.Message
    saveMessageErr error
    saveMessageID  string
}
```

### HTTP Mocking with httptest

For HTTP communication, use `net/http/httptest`:

```go
func TestServer(t *testing.T) {
    tests := []struct {
        name   string
        method string
        body   any
    }{
        {
            name:   "valid POST with event",
            method: http.MethodPost,
            body:   Event{SubagentName: "test-agent", Type: EventTypeToolCall},
        },
        {
            name:   "GET method not allowed",
            method: http.MethodGet,
            body:   nil,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var receivedEvent *Event
            handler := func(e Event) {
                receivedEvent = &e
            }

            ctx, cancel := context.WithCancel(context.Background())
            defer cancel()

            server := NewServer(handler)
            address, err := server.Start(ctx)
            require.NoError(t, err)

            var bodyBytes []byte
            if tt.body != nil {
                bodyBytes, _ = json.Marshal(tt.body)
            }

            req, _ := http.NewRequest(tt.method, address+"/subagent-events", bytes.NewReader(bodyBytes))
            req.Header.Set("Content-Type", "application/json")

            resp, _ := http.DefaultClient.Do(req)
            defer resp.Body.Close()

            cupaloy.SnapshotT(t, struct {
                StatusCode    int
                EventReceived bool
            }{
                StatusCode:    resp.StatusCode,
                EventReceived: receivedEvent != nil,
            })
        })
    }
}
```

### Error Simulation

Test error handling by simulating failures. From `internal/commands/subagent_test.go`:

```go
func TestExecuteSubagent_SubagentStartEmissionFailureAbortsImmediately(t *testing.T) {
    // Create a server that always returns 500 error
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
    }))
    defer server.Close()

    eventClient := subagentlog.NewClient(server.URL)

    // Use a generator that should NOT be called if start emission fails
    generatorCalled := false
    generator := &subagentContextAwareGenerator{
        generateFunc: func(ctx context.Context) (gai.Dialog, error) {
            generatorCalled = true
            return gai.Dialog{
                {
                    Role: gai.Assistant,
                    Blocks: []gai.Block{
                        {BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("response")},
                    },
                },
            }, nil
        },
    }

    userBlocks := []gai.Block{
        {BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Test prompt")},
    }

    _, err := ExecuteSubagent(context.Background(), SubagentOptions{
        UserBlocks:   userBlocks,
        Generator:    generator,
        EventClient:  eventClient,
        SubagentName: "test_subagent",
        RunID:        "run_123",
    })

    if err == nil {
        t.Fatal("expected error when subagent_start emission fails")
    }
    if !strings.Contains(err.Error(), "500") {
        t.Errorf("error message should mention status code 500, got: %s", err.Error())
    }
    if generatorCalled {
        t.Error("generator should not be called when subagent_start emission fails")
    }
}
```

## Test Data

### Testdata Directories

Static test fixtures are stored in `testdata/` directories:

```
docs/specs/testdata/
  virtual_tool_call_example.txt
  virtual_tool_sample_code.txt
```

### Inline Test Data

For small, context-specific test data, embed directly in tests:

```go
func TestConfig(t *testing.T) {
    content := `
version: "1.0"
models:
  - ref: "model"
    display_name: "Model"
    id: "id"
    type: "openai"
    api_key_env: "API_KEY"
defaults:
  model: "model"
`
    // Use content...
}
```

### Temporary Files and Directories

For tests requiring filesystem operations:

```go
func TestWithTempFiles(t *testing.T) {
    tempDir := t.TempDir() // Auto-cleaned up
    configPath := filepath.Join(tempDir, "test.yaml")

    if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
        t.Fatalf("write config: %v", err)
    }

    // Test operations on tempDir...
}
```

## Running Tests

### Basic Commands

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run specific package tests
go test ./internal/codemode/...

# Run specific test function
go test -run TestSchemaToGoType ./internal/codemode/

# Run with race detection
go test -race ./...
```

### Snapshot Management

```bash
# Update all snapshots
UPDATE_SNAPSHOTS=true go test ./...

# Update snapshots for specific package
UPDATE_SNAPSHOTS=true go test ./internal/codemode/...
```

### Test Coverage

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# View coverage in browser
go tool cover -html=coverage.out
```

## Best Practices

### From AGENTS.md

The project's testing guidelines mandate:

1. **Use table-driven tests** with descriptive `name` fields
2. **Use exact matching** for assertions—avoid `strings.Contains` or partial matching
3. **Prefer httptest** for HTTP testing—avoid real network calls
4. **Keep tests deterministic**—use short timeouts, avoid sleeping
5. **Isolate filesystem effects**—clean up temp files
6. **Use in-memory databases** for storage tests (SQLite `:memory:`)

### Additional Patterns

These patterns are commonly used throughout the codebase. For real examples, see `internal/commands/subagent_test.go`.

#### Context Cancellation Testing

From `TestExecuteSubagent_ContextCancellation`:

```go
func TestExecuteSubagent_ContextCancellation(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel() // Cancel immediately

    generator := &subagentContextAwareGenerator{
        generateFunc: func(ctx context.Context) (gai.Dialog, error) {
            return nil, ctx.Err()
        },
    }

    _, err := ExecuteSubagent(ctx, SubagentOptions{
        UserBlocks: userBlocks,
        Generator:  generator,
    })

    if err == nil {
        t.Fatal("expected error for cancelled context")
    }
    if !strings.Contains(err.Error(), "generation failed") {
        t.Errorf("unexpected error message: %v", err)
    }
}
```

#### Exit Code Classification

The `TestExecuteCode` test in `internal/codemode/executor_test.go` demonstrates exit code classification using table-driven tests with validation callbacks:

```go
tests := []struct {
    name          string
    llmCode       string
    wantExitCode  int
    wantErrType   string // "none", "recoverable", "fatal"
    validate      func(t *testing.T, result ExecutionResult, err error)
}{
    {
        name:         "Run returns error (exit 1) returns RecoverableError",
        llmCode:      errorReturningCode,
        wantExitCode: 1,
        wantErrType:  "recoverable",
        validate: func(t *testing.T, result ExecutionResult, err error) {
            var recErr RecoverableError
            if !errors.As(err, &recErr) {
                t.Fatalf("error type = %T, want RecoverableError", err)
            }
            if recErr.ExitCode != 1 {
                t.Errorf("RecoverableError.ExitCode = %d, want 1", recErr.ExitCode)
            }
        },
    },
    {
        name:         "panic (exit 2) returns RecoverableError",
        llmCode:      panicCode,
        wantExitCode: 2,
        wantErrType:  "recoverable",
        // ... validation
    },
    {
        name:         "fatalExit (exit 3) returns FatalExecutionError",
        llmCode:      fatalExitCode,
        wantExitCode: 3,
        wantErrType:  "fatal",
        // ... validation
    },
}
```

#### Validation Callbacks in Table Tests

```go
tests := []struct {
    name     string
    input    string
    validate func(t *testing.T, result Result, err error)
}{
    {
        name:  "panic produces stack trace",
        input: "panic code",
        validate: func(t *testing.T, result Result, err error) {
            assert.Contains(t, result.Output, "panic:")
            var recErr RecoverableError
            require.ErrorAs(t, err, &recErr)
            assert.Equal(t, 2, recErr.ExitCode)
        },
    },
}
```

## Related Specifications

For more detailed context on specific testing areas:

- **Code Mode Execution**: See `docs/specs/code_mode.md` for the code execution model and exit code semantics
- **Subagent Logging**: See `docs/specs/subagent_logging.md` for event emission and rendering
- **MCP Server Mode**: See `docs/specs/mcp_server_mode.md` for subagent server testing
- **Conversation Persistence**: See `docs/specs/conversation_persistence.md` for storage testing patterns

## Summary

CPE's testing strategy emphasizes:

| Aspect | Approach |
|--------|----------|
| Primary pattern | Table-driven tests |
| Output verification | Snapshot testing with cupaloy |
| HTTP mocking | `net/http/httptest` |
| Database testing | In-memory SQLite |
| Assertions | `testify/assert` and `testify/require` |
| Interface mocking | Custom mock structs |
| Filesystem isolation | `t.TempDir()` |

Tests should be deterministic, fast, and maintainable. Prefer exact matching over partial matching, and use snapshots for complex outputs that would be unwieldy to assert inline.

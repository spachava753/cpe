# CPE (Chat-based Programming Editor)

CPE is a powerful command-line tool that enables developers to leverage AI for codebase analysis, modification, and
software development through natural language interactions in your terminal. It connects multiple AI models (OpenAI,
Anthropic, Google) to your local environment through a simple CLI interface.

## Project Overview

CPE serves as an intelligent agent to assist with day-to-day software development tasks. The core functionality is
implemented in Go, with a modular architecture that supports multiple AI providers, conversation management, and
tool-based interactions.

### Key Components

- **CLI Interface**: Cobra-based command-line interface in `cmd/`
- **AI Providers**: Support for OpenAI, Anthropic Claude, and Google Gemini
- **Conversation Management**: SQLite-based storage with branching support
- **MCP Integration**: Model Context Protocol for external tool integration
- **Token Management**: Advanced token counting and context window handling
- **URL Handler**: Secure HTTP/HTTPS content downloading with size limits and retry logic

## Project Structure and Organization

```
cpe/
├── cmd/                    # CLI commands and root command
│   ├── conversation.go      # Conversation management commands
│   ├── mcp.go            # MCP-related commands
│   ├── model.go          # Model management commands
│   ├── tools.go          # Development tools commands
│   └── root.go           # Main CLI entry point
├── internal/              # Internal packages
│   ├── agent/            # AI agent core functionality
│   ├── cliopts/          # CLI option handling
│   ├── mcp/              # Model Context Protocol implementation
│   ├── storage/          # SQLite conversation storage
│   ├── tiktokenloader/   # Token counting utilities
│   ├── token/            # Token management
│   └── urlhandler/       # HTTP/HTTPS content downloading
├── scripts/               # Development and utility scripts
├── example_prompts/       # Example prompt templates
├── go.mod                # Go module definition
├── sqlc.yaml             # SQLC configuration for database
└── gen.go                # Code generation directives
```

## Build, Test, and Development Commands

### URL Handling

CPE now supports downloading and processing content directly from HTTP/HTTPS URLs:

```bash
# Process content from URL
cpe --input https://example.com/file.txt "Analyze this file"

# Multiple URLs and files
cpe --input local.go --input https://api.github.com/repos/spachava753/cpe/readme "Compare local vs remote"
```

**Features:**

- 50MB size limit for security
- Configurable timeouts and retry logic
- Exponential backoff for failed requests
- Custom user agent for identification
- MIME type detection from response headers

**Security:**

- URL validation and sanitization
- Size limits to prevent memory exhaustion
- Timeout protection against hanging requests

### CLI Flags and Options

#### Core Flags

```bash
# Input handling
--input, -i FILE/URL     Input file or URL to process (can be used multiple times)
--skip-stdin            Skip reading from stdin even if data is available

# Model configuration
--model MODEL          Specify AI model to use (default: claude-3-5-sonnet)
--no-stream            Disable streaming responses

# MCP Configuration
--mcp-config FILE      Custom path for MCP configuration file (default: .cpemcp.json)
```

#### Input Sources

CPE can accept input from multiple sources:

- Command line arguments (direct prompt)
- Files (--input file.go)
- URLs (--input https://example.com/file.txt)
- Standard input (piped content)
- Combination of the above

Examples:

```bash
# File and URL processing
cpe --input main.go --input https://raw.githubusercontent.com/spachava753/cpe/main/go.mod "Compare dependencies"

# Skip stdin when piping
echo "test" | cpe --skip-stdin "Process this without stdin"
```

#### Build Commands

```bash
# Build the binary
go build -o cpe .

# Install globally
go install github.com/spachava753/cpe@latest

# Run with go run
go run . "your prompt here"

# Cross-compile for different platforms
GOOS=linux GOARCH=amd64 go build -o cpe-linux-amd64 .
GOOS=darwin GOARCH=amd64 go build -o cpe-darwin-amd64 .
GOOS=windows GOARCH=amd64 go build -o cpe-windows-amd64.exe .
```

#### Test Commands

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for specific package
go test ./cmd/...
go test ./internal/agent/...

# Run tests with coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Run specific test
go test -run TestConversationTree ./cmd/

# Run tests with race detection
go test -race ./...

# Generate code and run tests
go generate ./...
go test ./...
```

#### Database Commands

```bash
# Generate SQLC code
go generate ./...

# Check database schema
sqlite3 .cpeconvo ".schema"

# Reset conversation database
rm .cpeconvo
```

#### Linting and Formatting

```bash
# Format code
go fmt ./...

# Run go vet
go vet ./...

# Run static analysis
go run honnef.co/go/tools/cmd/staticcheck@latest ./...

# Check for vulnerabilities
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

## Code Style and Conventions

### Go Style Guidelines

- **Format**: Use `gofmt` for consistent formatting
- **Linting**: Follow standard Go conventions with `go vet` and `golint`
- **Naming**: Use camelCase for variables and functions, PascalCase for exported types
- **Error Handling**: Always check errors explicitly, use `fmt.Errorf` with context
- **Context**: Always pass context for cancellation and timeouts
- **Modules**: Use semantic versioning, avoid breaking changes in minor releases

### Code Organization

- **Packages**: Keep packages small and focused on single responsibility
- **Interfaces**: Define interfaces close to where they're used
- **Dependencies**: Minimize external dependencies, prefer standard library
- **Testing**: Place tests in the same package as code being tested

### Documentation Conventions

- **Comments**: Use complete sentences for all exported symbols
- **Examples**: Include examples in test files (`example_test.go`)
- **README**: Keep README.md updated with the latest usage examples
- **Godoc**: Ensure all public APIs have proper godoc comments

### Logging and Output

- **CLI Output**: Use `fmt.Fprintln(os.Stderr, ...)` for diagnostic messages
- **Progress**: Use clear, concise messages for long-running operations
- **Error Messages**: Provide actionable error messages with context
- **Success**: Confirm successful operations with appropriate messages

## Architecture and Design Patterns

### Core Architecture

CPE follows a modular architecture with clear separation of concerns:

#### AI Provider Layer

- **Provider Interface**: Unified interface for different AI providers
- **Streaming Support**: Real-time response streaming for better UX
- **Tool Integration**: Support for function calling and external tools
- **Token Management**: Intelligent token counting and context management

#### Storage Layer

- **SQLite**: Local conversation storage with full-text search
- **Branching**: Support for conversation branching and merging
- **Migration**: Database schema migration support
- **Cleanup**: Automatic cleanup of old conversations

#### Input Processing Layer

- **Multi-source Input**: Support for files, URLs, and stdin
- **URL Handler**: Secure HTTP/HTTPS content downloading
- **File Type Detection**: MIME type detection from content and headers
- **Content Validation**: Size limits and security checks
- **Stream Processing**: Real-time processing for large inputs

#### CLI Layer

- **Cobra CLI**: Modern CLI framework with nested commands
- **Configuration**: Environment variable and flag-based configuration
- **Input Handling**: Support for stdin, files, and command arguments
- **Output**: Rich terminal output with colors and formatting

### Design Patterns Used

#### Command Pattern

- Each CLI command implements the `cobra.Command` interface
- Commands are organized hierarchically under the root command
- Clear separation between command definition and execution

#### Repository Pattern

- `DialogStorage` interface abstracts database operations
- Clean separation between storage and business logic
- Support for different backend implementations

#### Factory Pattern

- `InitGenerator` creates appropriate AI provider based on configuration
- Extensible for adding new AI providers
- Consistent interface across different providers

#### Adapter Pattern

- `StreamingAdapter` converts streaming interfaces to standard interfaces
- `ToolGenerator` adapts AI providers to support tool calls
- `ResponsePrinterGenerator` adds consistent output formatting

#### Observer Pattern

- Context-based cancellation for graceful shutdown
- Signal handling for interrupt signals
- Cleanup routines for resource management

## Testing Guidelines

### URL Handler Package (`internal/urlhandler/`)

The URL handler package provides secure HTTP/HTTPS content downloading with the following features:

**Security Features:**

- 50MB size limit to prevent memory exhaustion
- Configurable timeouts (default: 30s)
- Exponential backoff for retry logic
- Custom User-Agent identification
- Content-Type validation

**Usage:**

```go
import "github.com/spachava753/cpe/internal/urlhandler"

handler := urlhandler.New()
content, contentType, err := handler.Download("https://example.com/file.txt")
```

**Testing:**

```bash
# Run URL handler tests
go test ./internal/urlhandler/...
go test -v ./internal/urlhandler/...
```

#### Unit Tests

- **Location**: `*_test.go` files alongside source code
- **Coverage**: Aim for >80% code coverage
- **Naming**: `TestFunctionName_ScenarioName`
- **Table-driven tests**: Use for multiple input scenarios

#### Integration Tests

- **Database**: Test actual SQLite operations with temporary databases
- **AI Providers**: Mock external APIs for consistent testing
- **File System**: Use temporary directories for file operations

#### Test Data

- **Fixtures**: Store test data in `testdata/` directories
- **Mocking**: Use interfaces for external dependencies
- **Golden Files**: Use for complex output validation

### Test Commands

```bash
# Run all tests with race detection
go test -race ./...

# Run tests with coverage
go test -coverpkg=./... -coverprofile=coverage.out ./...

# Run specific test category
go test -tags=integration ./...

# Generate test coverage report
go tool cover -html=coverage.out -o coverage.html
```

### Test Utilities

- **Test helpers**: Use `testify` for assertions and mocks
- **Setup/Teardown**: Use `TestMain` for database setup
- **Parallel tests**: Use `t.Parallel()` where appropriate
- **Timeout handling**: Always test context cancellation

## Security Considerations

### API Key Management

- **Environment Variables**: Never hardcode API keys
- **Configuration**: Support for custom endpoints and API keys
- **Validation**: Validate API keys before making requests
- **Rotation**: Support for key rotation without restart

### Data Protection

- **Local Storage**: All conversation data stored locally
- **Encryption**: Consider encryption for sensitive conversations
- **Cleanup**: Automatic cleanup of old conversations
- **Privacy Mode**: Support for incognito mode (no storage)

### URL Security

**Content Downloading:**

- 50MB size limit prevents memory exhaustion attacks
- Configurable timeouts prevent hanging requests
- Exponential backoff prevents server overload
- URL validation prevents SSRF attacks
- Content-Type validation ensures safe processing
- Custom User-Agent for responsible crawling

**Input Validation:**

- File size limits: 50MB for URLs, 100MB for local files
- Path traversal protection for local files
- URL validation and sanitization
- Content-Type verification
- Extension-based type detection for local files

### Network Security

- **HTTPS**: Always use HTTPS for external API calls
- **Certificate Validation**: Validate SSL certificates
- **Timeouts**: Set appropriate timeouts for network requests
- **Rate Limiting**: Respect API rate limits and implement backoff

### Configuration Security

- **Secure Defaults**: Use secure defaults for all configurations
- **Validation**: Validate all configuration inputs
- **Secrets**: Never log sensitive information
- **Audit Trail**: Log security-relevant events

## Environment Setup and Configuration

### Required Environment Variables

```bash
# Required (at least one)
export ANTHROPIC_API_KEY="your_anthropic_api_key"
export OPENAI_API_KEY="your_openai_api_key"
export GEMINI_API_KEY="your_gemini_api_key"

# Optional model selection
export CPE_MODEL="claude-3-5-sonnet"  # Default model
export CPE_CUSTOM_URL="https://your-custom-endpoint.com"

# Deprecated environment variables:
# SKIP_STDIN - Use --skip-stdin flag instead
```

### Configuration Files

- **`.cpemcp.json`**: MCP server configuration (can be overridden with --mcp-config)
- **`.cpeignore`**: Files to exclude from analysis (Git-ignore format)
- **`.env`**: Local environment variables (not committed)

### Development Environment

```bash
# Quick setup
git clone https://github.com/spachava753/cpe.git
cd cpe
go mod download

# Set up environment
echo "CPE_MODEL=claude-3-5-sonnet" > .env
echo "ANTHROPIC_API_KEY=your_key_here" >> .env

# Build and test
go build -o cpe .
./cpe "Hello, CPE!"
```

### IDE Configuration

- **GoLand/VS Code**: Enable gofmt on save
- **Linting**: Install golangci-lint for IDE integration
- **Testing**: Configure test runner for table-driven tests
- **Debugging**: Use delve for debugging Go applications

### Continuous Integration

- **GitHub Actions**: Automated testing and building
- **Code Coverage**: Track coverage trends
- **Security Scanning**: Automated vulnerability scanning
- **Dependency Updates**: Automated dependency updates

## Git Workflow and Branching Strategy

### Branch Naming

- `feature/description` - New features
- `bugfix/description` - Bug fixes
- `hotfix/description` - Critical fixes
- `docs/description` - Documentation updates
- `refactor/description` - Code refactoring

### Commit Messages

Follow conventional commits format:

```
type(scope): description

[optional body]

[optional footer]
```

### Pre-commit Checks

```bash
# Always run before committing
go fmt ./...
go vet ./...
go test ./...

# Check for security issues
govulncheck ./...
```

### Release Process

1. Update version in `internal/version/version.go`
2. Update CHANGELOG.md
3. Create release branch: `release/v1.x.x`
4. Run full test suite
5. Create GitHub release with binaries
6. Update documentation

## Additional Resources

- @README.md - Main project documentation
- @ROADMAP.md - Project roadmap and future plans
- @sqlc.yaml - Database configuration
- @go.mod - Dependencies and Go version requirements
- @cmd/root.go - Main CLI implementation
- @internal/agent/ - AI agent core functionality
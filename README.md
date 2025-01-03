# CPE (Chat-based Programming Editor)

CPE is a powerful command-line tool that enables developers to leverage AI for codebase analysis, modification, and
improvement through natural language interactions. It supports multiple programming languages and seamlessly integrates
with your development workflow through standard Unix pipes and redirection.

## Features

- **Multi-Language Support**: Analyzes and modifies code in multiple programming languages including Go, Java, Python,
  and more
- Automatically identifies and selects relevant files for analysis using Tree-sitter (For supported languages)
- Token counting and visualization to understand and pinpoint large files
- **Powerful File Operations**:
    - Analyze existing code
    - Modify files with precision
    - Create new files
    - Remove files
- **Multiple LLM Providers**:
    - Anthropic
    - Google
    - OpenAI (or any OpenAI-like API)
- **Flexible Input/Output**:
  - Accept input from stdin or file
  - Pipe results to other commands
  - Integrates naturally with Unix tools
- **Highly Configurable Generation**:
    - Choice of AI model
    - Adjustable generation parameters
    - Customizable file ignore patterns

## Installation

To install CPE, make sure you have Go 1.22 or later installed on your system. Then, run the following command:

```
go install github.com/spachava753/cpe@latest
```

## Usage

If no `-model` flag is provided, Clause Sonnet 3.5 is used by default. To use CPE, run the following command:

```
cpe [flags] < input.txt
```

or

```
cpe [flags] -input input.txt
```

or

```
echo "Hi!" | cpe [flags]
```

### Examples

#### Basic Usage

1. Quick code analysis:
   ```bash
   echo "What does main.go do?" | cpe
   ```

2. Using a specific model:
   ```bash
   cpe -model claude-3-opus -input query.txt
   ```

#### Advanced Usage

1. Pipe git diff for review:
   ```bash
   echo "git diff:\n\n$(git diff)\n\nReview these changes and suggest improvements" | cpe -model gpt-4o
   ```

2. Analyze code coverage report:
   ```bash
   go test -coverprofile=coverage.out ./...
   echo "$(go tool cover -func=coverage.out)\n\nAnalyze this coverage report and suggest areas needing more tests" | cpe
   ```

3. Token counting for project analysis:
   ```bash
   cpe -token-count ./src
   ```

#### Configuration Examples

1. Configuring generation parameters:
   ```bash
   cpe -model gpt-4o -temperature 0.8 -top-p 0.9 -frequency-penalty 0.5 < prompt.txt
   ```

2. Custom API endpoint:
   ```bash
   cpe -model gemini-1.5-pro -custom-url https://custom-endpoint.com/v1 < query.txt
   ```

3. Version information:
   ```bash
   cpe -version
   ```

## Configuration and Setup

### Environment Variables

To use specific LLM providers, set the corresponding API keys:

- Anthropic: `ANTHROPIC_API_KEY`
- Google: `GEMINI_API_KEY`
- OpenAI: `OPENAI_API_KEY`

### Ignore Patterns

CPE uses a `.cpeignore` file to specify patterns for files and directories that should be ignored when executing (
Helpful to exclude large generated files). The file follows `.gitignore` syntax:

```gitignore
# Example .cpeignore
node_modules/
*.log
build/
dist/
.git/
```

### Token Counting

CPE includes a token counting feature to help you understand your codebase's size and complexity:

```bash
# Count tokens in the entire project
cpe -token-count .

# Count tokens in a specific directory
cpe -token-count ./src

# Count tokens in specific file types
find . -name "*.go" | xargs cpe -token-count
```

The token count visualization shows:

- Total tokens per file
- Directory summaries
- Token distribution across the codebase

## File Operations

CPE can perform the following file operations based on model tool calls:

- **Modify Files**: Update existing file content with precise replacements
- **Create Files**: Generate new files with specified content
- **Remove Files**: Delete existing files when necessary

All file operations:

1. Are validated before execution
2. Require explicit content or paths
3. Are logged for transparency

## License

This project is licensed under the [MIT License](LICENSE).
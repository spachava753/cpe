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
- **Conversation Management**:
    - Continue conversations with `-continue`
    - List all conversations with `-list-convo`
    - Print specific conversations with `-print-convo`
    - Delete conversations with `-delete-convo`
    - Support for conversation threading
- **Multiple LLM Providers**:
    - Anthropic
    - Google
    - OpenAI (or any OpenAI-like API)
- **Flexible Input/Output**:
  - Accept input from stdin or file
  - Pipe results to other commands
  - Integrates naturally with Unix tools
  - List all text files recursively with `-list-files`
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

If no `-model` flag is provided, Claude Sonnet 3.5 is used by default. CPE accepts input from multiple sources that can be combined:

1. Stdin (pipe or redirection):
```
cpe [flags] < input.txt
```
or
```
echo "Hi!" | cpe [flags]
```

2. Input file:
```
cpe [flags] -input input.txt
```

3. Command line arguments:
```
cpe [flags] "Your prompt here"
```

These input methods can be combined. For example:
```
echo "Context:" | cpe [flags] -input details.txt "Please analyze this"
```
In this case, the inputs will be concatenated in order (stdin, input file, command line arguments) separated by double newlines.

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

4. Continuing a conversation thread:
   ```bash
   # Start a conversation about code structure
   echo "Explain the structure of this codebase" | cpe
   
   # Continue the conversation with follow-up questions
   cpe -continue last "How can we improve the error handling?"
   
   # Branch the conversation in a different direction
   cpe -continue 01HQ6ABCDEF123456 "What about the test coverage?"
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

### Custom URL Configuration

CPE supports multiple ways to define a custom URL for API endpoints, with the following priority order (highest to lowest):

1. Command line flag: `-custom-url`
2. Model-specific environment variable (e.g., `CPE_CLAUDE_3_5_SONNET_URL` for the Claude 3.5 Sonnet model)
3. General custom URL environment variable: `CPE_CUSTOM_URL`

If multiple methods are used, the highest priority method takes precedence. For example, if both the `-custom-url` flag and environment variables are set, the flag value will be used.

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

## Conversation Management

CPE maintains a history of your conversations and allows you to manage them through various commands. Conversations are stored in a local `.cpeconvo` file in your current directory.

### Basic Conversation Commands

1. List all conversations:
   ```bash
   cpe -list-convo
   ```
   This displays a table with conversation IDs, parent IDs (for threaded conversations), models used, timestamps, and message previews.

2. Continue a conversation:
   ```bash
   # Continue from the latest conversation
   cpe -continue last "Follow up question"

   # Continue from a specific conversation by ID
   cpe -continue 01HQ6ABCDEF123456 "Follow up question"
   ```

3. Print a specific conversation:
   ```bash
   cpe -print-convo 01HQ6ABCDEF123456
   ```

4. Delete a conversation:
   ```bash
   # Delete a single conversation
   cpe -delete-convo 01HQ6ABCDEF123456

   # Delete a conversation and all its children
   cpe -delete-convo 01HQ6ABCDEF123456 -cascade
   ```

### Conversation Threading

Conversations can form a tree structure where each conversation can have a parent and multiple children. When you continue a conversation:
- A new conversation is created
- It's linked to its parent conversation
- It inherits the conversation history
- You can use `-cascade` flag when deleting to remove an entire conversation thread

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
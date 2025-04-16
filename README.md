# CPE (Chat-based Programming Editor)

CPE is a powerful command-line tool that enables developers to leverage AI for codebase analysis, modification, and
improvement through natural language interactions. It serves as an intelligent agent to assist you with day-to-day
software development tasks.

## Installation

```bash
go install github.com/spachava753/cpe@latest
```

## Quick Start Guide

1. Set up required API keys as environment variables (at least one is needed):
   ```bash
   # For Claude models (default)
   export ANTHROPIC_API_KEY=your_api_key_here
   
   # For OpenAI models
   export OPENAI_API_KEY=your_api_key_here
   
   # For Gemini models
   export GEMINI_API_KEY=your_api_key_here
   
   # For custom model endpoints (optional)
   export CPE_CUSTOM_API_KEY=your_custom_api_key_here
   ```

2. Run your first command:
   ```bash
   cpe "Tell me about yourself"
   ```

3. Customize default model (optional):
   ```bash
   # Set a different default model
   export CPE_MODEL=gpt-4o
   
   # Or specify model per command
   cpe -m gpt-4o "Explain how promises work in JavaScript"
   ```

## Features

- **Codebase Understanding**: Analyze and understand codebases of any size
- **Code Modification**: The AI model can make precise changes to your code through natural language instructions
- **Multi-Model Support**: Works with various AI models including:
  - Claude models (3.5 Sonnet, 3.5 Haiku, 3.7 Sonnet, 3 Opus, 3 Haiku)
  - OpenAI models (GPT-4o, GPT-4o-mini)
  - Gemini models (1.5 Pro, 1.5 Flash, 2.0 Flash)
- **Multi-Modal Inputs**: Support for text, images, video, and audio (depending on the model)
- **Conversation Management**: Save, continue, and manage conversations
- **AI-Powered File Operations**: The AI model can create, edit, view, and delete files through your instructions
- **Code Analysis Tools**: Get overviews of codebases, find related files, and analyze token usage

## Common Use Cases

### Code Understanding

```bash
# Understand a complex codebase quickly
cpe "Explain the architecture of this project and how the components interact"

# Analyze specific error output
go build 2>&1 | cpe "Why is this build failing and how do I fix it?"

# Get help with external API documentation
curl -s https://api.example.com/docs | cpe "Explain how to implement authentication with this API"

# Trace execution flow
cpe "Walk through what happens when a user logs in to this application"
```

### Code Improvement

```bash
# Refactoring suggestions
cpe "Suggest refactoring opportunities in the authentication module"

# Bug fixing with error context
./run_tests.sh 2>&1 | cpe "Fix the failing tests"

# Performance optimization with profiling data
go test -bench=. | cpe "Identify performance bottlenecks and suggest improvements"

# Multiple inputs for context
go build -v 2>&1 > build_log.txt
cpe -i build_log.txt -i config/settings.json -i src/main.go "Fix the build issues"
```

### Code Generation

```bash
# Generate boilerplate
cpe "Create a RESTful API endpoint for user registration with validation"

# Test generation with multiple input files for context
cpe -i src/auth/middleware.js -i src/auth/controllers.js -i src/models/user.js "Write comprehensive unit tests for the auth module"

# Documentation based on multiple inputs
cpe -i src/api/routes.js -i src/api/controllers/users.js -i src/api/models/user.js "Generate API documentation for the user endpoints"
```

## Usage

### Basic Usage

```bash
cpe "Your prompt or question here"
```

### Advanced Usage Examples

**Multiple input methods**:

```bash
# Using command line arguments
cpe "Explain the main function in this codebase"

# Using stdin
cat main.go | cpe "Explain this code"

# Using input file flag
cpe -i main.go "Explain this code"

# Using multiple input files
cpe -i main.go -i config.json "Compare these two files"

# Combining input methods
cat error.log | cpe -i main.go "Fix the error in main.go based on this log"

# Using image input (with models that support it)
cpe -i screenshot.png "What's wrong with this UI?"
```

**Using specific models and parameters**:
```bash
# Using a specific model
cpe -m claude-3-7-sonnet "Optimize this algorithm"

# Setting temperature for more creative responses
cpe -t 0.8 "Generate 5 creative names for my new app"

# Limiting token output
cpe -x 2000 "Summarize this codebase concisely"

# Setting thinking budget for enhanced reasoning (for supported models)
cpe -m claude-3-7-sonnet -b 20000 "Solve this complex algorithmic problem"
cpe -m o3-mini -b high "Design a scalable architecture for my application"

# Combined parameters
cpe -m gpt-4o -t 0.7 --top-p 0.95 -x 4000 "Refactor this code for better performance"
```

**Conversation management**:

```bash
# Start a new conversation
cpe -n "Let's design a REST API"

# Continue the most recent conversation (model is remembered automatically)
cpe "Add authentication to the API"

# Continue with modified parameters (model is remembered, but generation params must be specified)
cpe -t 0.7 -b 10000 "Make the authentication more secure"

# Continue from a specific conversation ID
cpe -c abcd1234 "Let's discuss the rate limiting"

# List all conversations in a git-like graph
cpe conversation list
# or use aliases
cpe convo ls

# View a specific conversation by message ID
cpe conversation print abcd1234
# or use aliases
cpe convo show abcd1234

# Delete a message by ID
cpe conversation delete abcd1234
# Delete a message and all its children
cpe conversation delete --cascade abcd1234
```

**Custom API Endpoints**:

```bash
# Using a custom API endpoint with an unknown model (requires CPE_CUSTOM_API_KEY)
export CPE_CUSTOM_API_KEY=your_custom_api_key_here
cpe -m unknown-model --custom-url https://your-custom-endpoint.com/v1 "Your prompt here"

# Using OpenRouter for accessing unknown models
export CPE_CUSTOM_URL=https://openrouter.ai/api/v1
export CPE_CUSTOM_API_KEY=your_openrouter_api_key_here
cpe -m mistral-large "Create a logging library in Go"

# Using Cloudflare AI Gateway for tracing and observability (uses ANTHROPIC_API_KEY)
export CPE_CLAUDE_3_7_SONNET_URL=https://gateway.ai.cloudflare.com/v1/your-account/your-gateway/anthropic
cpe "Write a sorting algorithm implementation"
```

**Offline Usage with Ollama**:

```bash
# Start ollama with your model of choice
ollama run llama3

# Configure CPE to use ollama
export CPE_CUSTOM_URL=http://localhost:11434/v1
export OPENAI_API_KEY=ollama
cpe -m llama3 "How can I create a web server in Go?"
```

## Commands

### Main Command

- `cpe [flags] [prompt]`: Interact with the AI agent

### Conversation Management

- `cpe conversation list` (aliases: `convo list`, `conv ls`): List all conversations in a git-like graph
- `cpe conversation print [id]` (aliases: `convo show`, `conv view`): Print a specific conversation
- `cpe conversation delete [id]` (aliases: `convo rm`, `conv remove`): Delete a conversation

### Tools

- `cpe tools overview`: Debug tool to see what files the LLM would analyze for a given input
- `cpe tools related-files [file1,file2,...]`: Debug tool to see what related files the LLM would find
- `cpe tools list-files`: Output all text files recursively (useful for piping to clipboard for use in other AI apps)
- `cpe tools token-count [path]`: Count tokens in files and display as a tree to understand context window usage

### Environment

- `cpe env`: Print all environment variables used by CPE

## Configuration

### Environment Variables

- `ANTHROPIC_API_KEY`: API key for Claude models
- `OPENAI_API_KEY`: API key for OpenAI models
- `GEMINI_API_KEY`: API key for Gemini models
- `CPE_MODEL`: Default model to use
- `CPE_CUSTOM_URL`: Custom base URL for model APIs
- `CPE_CUSTOM_API_KEY`: API key to use with custom model endpoints
- `CPE_[MODEL_NAME]_URL`: Model-specific custom URL (e.g., `CPE_CLAUDE_3_5_SONNET_URL`)
- `CPE_DISABLE_TOOL_USE`: When set, disables tool use for custom models (used when working with models that don't support tool use)

### Ignore Files

CPE respects `.cpeignore` files similar to `.gitignore` for excluding files from analysis. Create a `.cpeignore` file in
your project root to customize what files should be excluded.

**Warning**: The `.cpeignore` file mainly affects what files the AI model sees when using the overview and related-files
tools. The model may still view, edit, and remove files that are ignored when performing file operations.

### Command Line Flags

```
Flags:
  -c, --continue string             Continue from a specific conversation ID
  -h, --help                        Help for cpe
  -i, --input strings               Specify input files to process. Multiple files can be provided.
  -m, --model string                Specify the model to use
      --custom-url string           Specify a custom base URL for the model provider API
  -x, --max-tokens int              Maximum number of tokens to generate
  -n, --new                         Start a new conversation instead of continuing from the last one
      --number-of-responses int     Number of responses to generate
      --frequency-penalty float     Frequency penalty (-2.0 - 2.0)
      --presence-penalty float      Presence penalty (-2.0 - 2.0)
  -t, --temperature float           Sampling temperature (0.0 - 1.0)
  -b, --thinking-budget string      Budget for reasoning/thinking capabilities
      --top-k int                   Top-k sampling parameter
      --top-p float                 Nucleus sampling parameter (0.0 - 1.0)
  -v, --version                     Print the version number and exit
```

## Supported Models

- Claude models:
  - `claude-3-7-sonnet` (default)
  - `claude-3-5-sonnet`
  - `claude-3-5-haiku`
  - `claude-3-opus`
  - `claude-3-haiku`
- OpenAI models:
  - `gpt-4o`
  - `gpt-4o-mini`
  - `o3-mini`
- Gemini models:
  - `gemini-1-5-pro`
  - `gemini-1-5-flash`
  - `gemini-2-flash-exp`
  - `gemini-2-flash`
  - `gemini-2-flash-lite-preview`
  - `gemini-2-pro-exp`

## Version Compatibility

- Go version: 1.23+ is recommended (the tool uses Go 1.23.0 or later)
- OS compatibility: Linux, macOS, Windows

## Security Considerations

CPE interacts with external AI APIs and handles your codebase information. Keep in mind:

- **API Key Security**: CPE requires API keys to be set as environment variables. Never hardcode these keys into scripts
  or commit them to version control.

- **Data Privacy**: All prompts, code, and other inputs you provide to CPE are sent to the corresponding AI model
  provider (OpenAI, Anthropic, or Google). Be careful not to share sensitive or proprietary code.

- **Output Visibility**: The tool's output is visible in your terminal. Be mindful of exposing sensitive information
  when collaborating or screen sharing.

- **File Protections**: Use `.cpeignore` to exclude sensitive files from analysis. However, be aware that if you
  explicitly ask the model to interact with those files, it may still be able to do so through file operation tools.

- **Credential Scanning**: Be careful when running CPE in repositories that might contain credentials or sensitive
  tokens, as these could be picked up by the model.

## How It Works

CPE acts as an intelligent agent that:

1. Analyzes your request and breaks it down into manageable steps
2. Uses tools to understand your codebase (files overview, related files)
3. Makes precise code modifications when needed through AI-powered tools
4. Verifies changes to ensure correctness
5. Maintains conversation context so you can continue where you left off

The tool is designed to be integrated seamlessly into your development workflow, allowing you to get help with a wide
range of software development tasks without leaving your terminal.

## Roadmap

CPE is actively being developed. See the [ROADMAP.md](ROADMAP.md) file for upcoming features and improvements,
including:

- Token analysis and visualization improvements
- Enhanced code mapping for large repositories
- Support for additional LLM providers
- Conversation management enhancements
- Performance improvements for large codebases
- And more!

## License

This project is open source and available under the [MIT License](LICENSE).
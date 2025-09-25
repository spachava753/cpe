# CPE (Chat-based Programming Editor)

CPE is a powerful command-line tool that enables developers to leverage AI for codebase analysis, modification, and
software development through natural language interactions in your terminal.

## Overview

CPE serves as an intelligent agent to assist with day-to-day software development tasks by connecting multiple AI
models (OpenAI, Anthropic, Google) to your local environment through a simple CLI interface. It helps you analyze
codebases, make code modifications, debug issues, and perform various programming tasks through natural language
conversations.

## Features

- **AI-powered code assistance**: Interact with advanced AI models through natural language to analyze and modify code
- **Codebase understanding**: Process and analyze codebases of any size
- **File & folder operations**: Create, view, edit, move, and delete files and folders with AI guidance
- **Shell command execution**: Run bash commands directly through the AI
- **Multiple AI model support**:
  - OpenAI models (GPT-4o, GPT-4o Mini, etc.)
  - Anthropic Claude models (Claude 3.5 Sonnet, Claude 3 Opus, etc.)
  - Google Gemini models (Gemini 1.5 Pro, Gemini 1.5 Flash, etc.)
- **Conversation management**: Save, list, view, and continue previous conversations
- **Model Context Protocol (MCP)**: Connect to external MCP servers for enhanced functionality

## Installation

### Prerequisites

- Go 1.23+
- API key for at least one supported AI model provider:
  - OpenAI: https://platform.openai.com/
  - Anthropic: https://console.anthropic.com/
  - Google AI (Gemini): https://ai.google.dev/

### Install from source

```bash
go install github.com/spachava753/cpe@latest
```

### Environment Variables

Configure at least one of these API keys:

```bash
# Required (at least one)
export ANTHROPIC_API_KEY="your_anthropic_api_key"
export OPENAI_API_KEY="your_openai_api_key"
export GEMINI_API_KEY="your_gemini_api_key"

# Optional
export CPE_MODEL="claude-3-5-sonnet"  # Default model to use if not specified with --model
export CPE_CUSTOM_URL="https://your-custom-endpoint.com"  # For custom API endpoints
```

## Quick Start

### Basic Usage

```bash
# Ask a simple question
cpe "What is a fibonacci sequence?"

# Start a coding task
cpe "Create a simple REST API server in Go with one endpoint to return the current time"

# Analyze a specific file
cpe "Analyze this code and suggest improvements" -i path/to/your/file.js

# Analyze multiple files (either approach works)
cpe "Check these files for bugs" -i file1.js -i file2.js
cpe "Check these files for bugs" -i file1.js,file2.js

# Start a new conversation (instead of continuing the last one)
cpe -n "Let's start a new project"

# Continue a specific conversation by ID
cpe -c abc123 "Could you explain more about the previous solution?"
```

### Working with AI tools

CPE provides a set of tools that the AI can use to help you:

- **Codebase analysis**: Understanding your codebase structure
- **File operations**: Creating, editing, viewing, and deleting files
- **Folder operations**: Creating, moving, and deleting directories
- **Shell integration**: Running commands directly in your environment
- **Multimedia input support**: Process images, audio, and video files with the `-i` flag

Just ask the AI naturally, and it will use the appropriate tools to help you:

```bash
cpe "Create a basic React component that fetches data from an API"
cpe "Fix the bug in my app.js file that's causing the navbar to disappear"
cpe "Write a unit test for the getUserData function in users.js"
cpe -i screenshot.png "What's wrong with this UI layout?"
cpe -i audio_recording.mp3 "Transcribe this meeting and summarize the key points"
```

### Combining Multiple Input Sources

CPE can accept input from multiple sources simultaneously:

```bash
# Combine stdin, file input, and command-line argument
cat error_log.txt | cpe -i screenshot.png "Debug this error and explain what's happening in the screenshot"

# Process multiple files of different types
cpe -i api_spec.yaml -i current_implementation.js "Update this code to match the API spec"

# Feed complex text with special characters via a file rather than command line
cpe -i complex_query.txt "Use this as a reference for the task"
```

## Conversation Management

One of CPE's most powerful features is its sophisticated conversation management system:

### Persistent Conversations

All conversations are automatically saved to a local SQLite database (`.cpeconvo`), allowing you to:

```bash
# Continue your most recent conversation without any special flags
cpe "Can you explain that last part in more detail?"

# Start a new conversation thread
cpe -n "I want to start working on a different project"

# Continue from a specific conversation by ID
cpe -c abc123 "Let's continue with the database schema design"

# View previous conversations
cpe conversation list

# See the full dialog from a specific conversation
cpe conversation print abc123
```

### Conversation Branching

You can create branches from any point in your conversation history:

```bash
# Start a new branch from an earlier conversation point
cpe -c abc123 "What if we used MongoDB instead of PostgreSQL?"

# This creates a new branch while preserving the original conversation path
```

### Interruption Recovery

If you interrupt the model during generation (Ctrl+C):

- The partial response and all actions performed up to that point are automatically saved
- You can continue from that interrupted state without losing context
- The AI will pick up where it left off

### Privacy Mode

For sensitive or temporary inquiries:

```bash
# Use incognito mode to prevent saving the conversation
cpe -G "How do I fix this security vulnerability?"
```

This powerful conversation system allows you to maintain context across multiple sessions, explore alternative solutions
through branching, and never lose your work even if interrupted.

## Command Reference

### Main command

```bash
cpe [flags] "Your prompt here"
```

#### Common Flags

| Flag                   | Short | Description                                                                                            |
|------------------------|-------|--------------------------------------------------------------------------------------------------------|
| `--model`,             | `-m`  | Specify which AI model to use                                                                          |
| `--temperature`        | `-t`  | Control randomness (0.0-1.0)                                                                           |
| `--max-tokens`         | `-x`  | Maximum tokens to generate                                                                             |
| `--input`              | `-i`  | Input file(s) of any type (text, images, audio, video) to process (can be repeated or comma-separated) |
| `--new`                | `-n`  | Start a new conversation                                                                               |
| `--continue`           | `-c`  | Continue from specific conversation ID                                                                 |
| `--incognito`          | `-G`  | Don't save conversation history                                                                        |
| `--system-prompt-file` | `-s`  | Specify custom system prompt template                                                                  |
| `--timeout`            |       | Request timeout (default 5m)                                                                           |

#### Advanced Flags

| Flag                    | Description                                 |
|-------------------------|---------------------------------------------|
| `--top-p`               | Nucleus sampling parameter (0.0-1.0)        |
| `--top-k`               | Top-k sampling parameter                    |  
| `--frequency-penalty`   | Penalize repeated tokens (-2.0-2.0)         |
| `--presence-penalty`    | Penalize tokens already present (-2.0-2.0)  |
| `--number-of-responses` | Number of alternative responses to generate |
| `--thinking-budget`     | Budget for reasoning/thinking capabilities  |

### Model Commands

```bash
# List all supported models
cpe model list   # or cpe models list
```

### Conversation Management

```bash
# List all conversations
cpe conversation list

# View a specific conversation
cpe conversation print <id>

# Delete a specific conversation
cpe conversation delete <id>

# Delete with all child messages
cpe conversation delete <id> --cascade
```

Note: Conversations are stored in a local SQLite database file named `.cpeconvo` in your current working directory. You
can back up this file or remove it to clear all stored conversations. See
the [Conversation Management](#conversation-management) section for more details.

### Debug Tools

```bash
# Get an overview of files in a directory
cpe tools overview [path]

# Find files related to specific input files
cpe tools related-files file1.go,file2.go

# Count tokens in code files
cpe tools token-count [path]

# List all text files in directory
cpe tools list-files
```

### MCP Tools

```bash
# Initialize a new MCP configuration
cpe mcp init

# List configured MCP servers
cpe mcp list-servers

# Get information about a specific MCP server
cpe mcp info <server_name>

# List tools available from an MCP server
cpe mcp list-tools server_name

# Directly call an MCP tool
cpe mcp call-tool --server server_name --tool tool_name --args '{"param": "value"}'
```

## Customization

### Customizing CPE

#### .cpeignore

Create a `.cpeignore` file to exclude certain paths from code analysis. It supports all standard Git-ignore syntax
including globs, negation with `!`, and comments:

```
# Ignore build artifacts
node_modules/
*.log
build/
dist/

# But don't ignore specific files
!build/important.js

# Ignore big data files
**/*.csv
**/*.json
```

#### Custom System Prompt

You can customize the AI's system instructions with a template file. This is a Go template that will be filled with data
from the environment where CPE is executed:

```bash
cpe -s path/to/custom_system_prompt.txt "Your prompt"
```

The system prompt file supports Go template syntax, allowing you to include dynamic information. For example:

```
You are an AI assistant helping with a codebase.
Current working directory: {{.WorkingDirectory}}
Git branch: {{.GitBranch}}
User: {{.Username}}
Operating System: {{.OperatingSystem}}
```

This allows you to create contextual system prompts that adapt to the current environment.

### MCP Configuration

Create a `.cpemcp.json` or `.cpemcp.yml` file to configure Model Context Protocol servers:

```json
{
  "mcpServers": {
    "my_server": {
      "command": "path/to/server/binary",
      "args": [
        "--port",
        "8080"
      ],
      "type": "stdio",
      "timeout": 30,
      "env": {
        "SERVER_ENV": "production"
      }
    },
    "external_server": {
      "type": "http",
      "url": "https://my-mcp-server.example.com/api"
    }
  }
}
```

## Examples

### Code Creation

```bash
cpe "Create a Python script that reads a CSV file, calculates statistics, and generates a report"
```

### Code Improvement

```bash
cpe -i path/to/slow_function.js "This function is slow. Can you optimize it?"
```

### Project Setup

```bash
cpe "Set up a new TypeScript project with Express and MongoDB integration"
```

### Debugging

```bash
cpe "I'm getting this error when running my app: [error message]. What might be causing it?"
```

## Known Limitations

- Very large codebases might exceed token limits
- Some complex refactoring operations may require multiple steps
- File overview tool may omit some code details to stay within token limits
- Code analysis primarily supports common languages (Go, JavaScript/TypeScript, Python, Java) using Tree-sitter parsers
- Specialized or less common languages may have limited analysis capabilities
- Performance varies based on the selected AI model

## License

MIT
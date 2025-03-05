# CPE Cobra Command Structure

This implementation migrates the CPE tool from using flags to a structured command hierarchy using the Cobra package.

## Command Structure

```
cpe
├── conversation (aliases: convo, conv)
│   ├── list (alias: ls)
│   ├── print (aliases: show, view)
│   └── delete (aliases: rm, remove)
├── env
├── tools
│   ├── overview
│   ├── related-files
│   ├── list-files
│   └── token-count
└── [root command]
```

## Features

- **Root Command**: Maintains the standard functionality of taking an input and executing an executor
- **Conversation Management**: Dedicated subcommand for managing conversations
- **Environment Variables**: Dedicated subcommand for printing environment variables
- **Tools**: Dedicated subcommand for accessing utility tools
- **Shorthand Flags**: Support for both shorthand and longhand flags
- **Command Aliases**: Intuitive aliases for commonly used commands

## Usage Examples

### Root Command

```bash
# Basic usage with prompt
cpe "What does this code do?"

# Using input files
cpe -i main.go -i utils.go "Explain these files"

# Using specific model
cpe -m claude-3-opus "Optimize this algorithm"
```

### Conversation Management

```bash
# List all conversations
cpe conversation list
# or using alias
cpe convo ls

# Print a specific conversation
cpe conversation print abc123
# or using alias
cpe conv show abc123

# Delete a conversation
cpe conversation delete abc123 --cascade
# or using alias
cpe convo rm abc123 --cascade
```

### Environment Variables

```bash
# Print all environment variables
cpe env
```

### Tools

```bash
# Get an overview of all files
cpe tools overview

# Get related files
cpe tools related-files main.go,utils.go

# List all text files
cpe tools list-files

# Count tokens in files
cpe tools token-count ./src
```

## Implementation Details

- Maintained backward compatibility with existing flags
- Added shorthand versions for commonly used flags
- Organized commands into logical groups
- Added helpful aliases for common commands
- Improved help text and documentation
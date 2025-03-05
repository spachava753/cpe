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
- **Multiple LLM Providers and Models**:
    - Anthropic
        - Claude 3 Opus
        - Claude 3.5 Sonnet (default)
        - Claude 3.5 Haiku
        - Claude 3 Haiku
    - Google
        - Gemini 1.5 Flash
        - Gemini 1.5 Pro
        - Gemini 2 Flash
        - Gemini 2 Pro
    - OpenAI (or any OpenAI-like API)
        - GPT-4o
        - GPT-4o Mini
        - O1
        - O1 Mini
        - O3 Mini
    - Custom Models via OpenAI-compatible APIs
        - Local models via Ollama
        - Together.ai models
        - Any provider implementing OpenAI's API spec
- **Flexible Input/Output**:
  - Support for multiple input types (depending on model):
    - Text (all models)
    - Images (Claude 3/3.5 models, GPT-4o models, Gemini models)
    - Video (Gemini models)
    - Audio (Gemini models)
  - Accept input from stdin or file
  - Pipe results to other commands
  - Integrates naturally with Unix tools
  - List all text files recursively with `-list-files`
  - Get codebase overview with `-overview`
- **Highly Configurable Generation**:
    - Choice of AI model
    - Adjustable generation parameters
    - Customizable file ignore patterns

## Installation

To install CPE, make sure you have Go 1.23 or later installed on your system. Then, run the following command:

```
go install github.com/spachava753/cpe@latest
```

## Usage

The model to use can be specified in three ways (in order of priority):
1. `-model` flag
2. `CPE_MODEL` environment variable
3. Default model (Claude 3.5 Sonnet)

For example:
```bash
# Using -model flag (highest priority)
cpe -model claude-3-opus "Your prompt"

# Using CPE_MODEL environment variable
export CPE_MODEL="gpt-4o"
cpe "Your prompt"

# Using default model
cpe "Your prompt"  # Uses Claude 3.5 Sonnet
```

CPE accepts input from multiple sources that can be combined:

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
cpe [flags] -input file1.txt -input file2.txt
```

3. Command line arguments:
```
cpe [flags] "Your prompt here"
```

These input methods can be combined. For example:
```
echo "Context:" | cpe [flags] -input details.txt -input code.go "Please analyze this"
```
In this case, the inputs will be concatenated in order (stdin, input files, command line arguments) separated by double newlines.

### Examples

#### Basic Usage

1. Quick code analysis:
   ```bash
   # Uses default Claude 3.5 Sonnet model
   echo "What does main.go do?" | cpe
   ```

2. Using specific models:
   ```bash
   # Use Claude 3 Opus for complex tasks
   cpe -model claude-3-opus -input query.txt -input code.go

   # Use Gemini 2 Pro for visual analysis
   cpe -model gemini-2-pro-exp "Analyze this screenshot" -input screenshot.png

   # Use O1 for reasoning
   cpe -model o1 "Analyze this large codebase"
   ```

#### Advanced Usage

1. Code Understanding and File Management:
   ```bash
   # Get a high-level overview of the codebase (what the model sees)
   cpe -overview

   # List all text files recursively (useful for copying to other AI tools)
   cpe -list-files
   ```

2. Token Analysis:
   ```bash
   # Count tokens in entire project
   cpe -token-count .

   # Count tokens in specific directory
   cpe -token-count ./src

   # Count tokens in specific files
   cpe -token-count ./main.go
   ```
   Helps identify files that consume large portions of a model's context window.

3. Conversation Management:
   ```bash
   # Start a new conversation (ignores any previous conversation)
   cpe -new "Explain the structure of this codebase"
   
   # Continue from the last conversation (default behavior)
   cpe "How can we improve the error handling?"
   
   # Continue from a specific conversation
   cpe -continue abcdef "What about the test coverage?"
   
   # Continue with custom generation parameters
   cpe -temperature 0.8 -top-p 0.9 "Generate more creative solutions"
   
   # List all conversations
   cpe -list-convo
   ```

Notes on conversation continuation:
- By default, continues from the last conversation
- Model is automatically inferred from the previous conversation, no need to provide `-model` flag every time
- Generation parameters (temperature, top-p, top-k, max tokens, etc.) must be specified each time if desired
- Use `-new` to start fresh and ignore previous conversations
- Use `-continue <id>` to continue from a specific conversation

#### Configuration Examples

1. Model and Generation Parameters:
   ```bash
   # Configure GPT-4 Turbo with specific parameters
   cpe -model gpt-4o -temperature 0.8 -top-p 0.9 -frequency-penalty 0.5 < prompt.txt

   # Use O1 model with reasoning
   cpe -model o1 -max-tokens 90000 < large_analysis.txt

   # Use Gemini with custom parameters
   cpe -model gemini-2-pro-exp -temperature 0.3 -top-k 40 < prompt.txt
   ```

2. Custom API Endpoints:
   ```bash
   # Using command line flag (highest priority)
   cpe -model claude-3-5-sonnet -custom-url https://custom-anthropic-endpoint.com/v1 < query.txt

   # Using model-specific environment variable
   export CPE_CLAUDE_3_5_SONNET_URL="https://custom-anthropic-endpoint.com/v1"
   cpe -model claude-3-5-sonnet < query.txt

   # Using general custom URL (lowest priority)
   export CPE_CUSTOM_URL="https://custom-endpoint.com/v1"
   cpe < query.txt
   ```

3. Multimodal Examples:
   ```bash
   # Analyze an image with Claude 3
   cpe -model claude-3-opus "Describe this diagram" -input architecture.png

   # Process video with Gemini
   cpe -model gemini-2-pro-exp "What's happening in this video?" -input demo.mp4

   # Analyze audio with Gemini
   cpe -model gemini-2-pro-exp "Transcribe and analyze this audio" -input meeting.mp3
   ```

### Additional Commands

1. List all text files in the project:
   ```bash
   cpe -list-files
   ```

2. Get a high-level overview of the codebase:
   ```bash
   cpe -overview
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

CPE supports multiple ways to define custom API endpoints, with the following priority order (highest to lowest):

1. Command-line flag:
   ```bash
   cpe -custom-url https://your-custom-endpoint.com/v1
   ```

2. Model-specific environment variable:
   ```bash
   # Format: CPE_MODEL_NAME_URL (uppercase, hyphens replaced with underscores)
   export CPE_CLAUDE_3_5_SONNET_URL="https://your-anthropic-endpoint.com/v1"
   export CPE_GPT_4O_URL="https://your-openai-endpoint.com/v1"
   export CPE_GEMINI_1_5_PRO_URL="https://your-gemini-endpoint.com/v1"
   ```

3. General custom URL environment variable:
   ```bash
   export CPE_CUSTOM_URL="https://your-default-endpoint.com/v1"
   ```

If multiple methods are configured, the highest priority method takes precedence. For example:
- If both `-custom-url` flag and environment variables are set, the flag value is used
- If both model-specific and general environment variables are set, the model-specific one is used
- If no custom URL is specified, the default endpoint for each provider is used

### Ignore Patterns

CPE automatically includes all text files in the current directory and its subdirectories unless explicitly ignored. This can lead to issues with:
- Context window exhaustion
- Increased API costs
- Higher latency
- Including unnecessary files (like node_modules, build artifacts, etc.)

It is **highly recommended** to set up a `.cpeignore` file before using CPE. The file follows `.gitignore` syntax and can be placed in multiple directories:

```gitignore
# Example .cpeignore
node_modules/
dist/
build/
*.log
vendor/
.git/
```

`.cpeignore` files are hierarchically constructed:
- Files can exist in parent and current directories
- Patterns from all found `.cpeignore` files are combined
- Patterns from parent directories are applied to all subdirectories
- More specific patterns in child directories can override parent patterns

Example directory structure:
```
/project
  .cpeignore        # Ignore node_modules/ and dist/ for all subdirs
  /frontend
    .cpeignore      # Additional ignores for frontend-specific files
  /backend
    .cpeignore      # Additional ignores for backend-specific files
```

This hierarchical structure allows you to:
- Set global ignore patterns at the root
- Add specific ignores for different parts of your project
- Override parent patterns when needed
- Maintain consistent ignores across the project

### Custom Models

CPE supports any model that's accessible through an OpenAI-compatible API. This enables you to use:
- Local models via Ollama
- Models from Together.ai
- Any provider implementing OpenAI's API specification

To use a custom model:

1. Set the OpenAI API key (if required by your provider):
   ```bash
   export OPENAI_API_KEY="your-api-key"  # Some providers like Ollama don't require this, in which case, just set the env var to an empty string, because cpe does require this
   ```

2. Set the custom URL for your provider:
   ```bash
   # For Ollama (local models)
   export CPE_CUSTOM_URL="http://localhost:11434/v1"
   
   # For Together.ai
   export CPE_CUSTOM_URL="https://api.together.xyz/v1"
   ```

3. Specify your model name:
   ```bash
   # For Ollama
   cpe -model codellama "Your prompt"
   
   # For Together.ai
   cpe -model mistral-7b "Your prompt"
   ```

When using a custom model:
- The model name can be any string
- Must provide a custom URL (via flag or environment variable)
- The provider must implement OpenAI's API specification
- Only text input is supported

### Token Counting

CPE includes a token counting feature to help you understand your codebase's size and complexity. This is particularly useful for identifying files that would consume large portions of a model's context window, helping you make informed decisions about which files to include in your prompts or add to your `.cpeignore` file.

```bash
# Count tokens in the entire project
cpe -token-count .

# Count tokens in a specific directory
cpe -token-count ./src

# Count tokens in a specific file
cpe -token-count ./main.go
```

The token count visualization shows:

- Total tokens per file
- Token count sums of all files in a directory (not ignored by .cpeignore)
- Token distribution across the codebase

This information helps you:
- Identify large files that might need to be excluded or split
- Understand how much of a model's context window your code will use
- Make informed decisions about what to include in your `.cpeignore` file

## Conversation Management

CPE maintains a history of your conversations in a SQLite database (`.cpeconvo/conversations.db` in your current directory). 

**Important Note**: 
- The `.cpeconvo` directory is created in the directory where you run the `cpe` command
- To maintain a single conversation history for a project, always run `cpe` from the same directory (typically the project root)
- Different directories will have separate conversation databases
- To continue previous conversations, ensure you're in the same directory where those conversations were started

Each conversation is stored with:
- A unique 6-character ID (lowercase letters)
- The parent conversation ID (for threading)
- The user's message
- The model used
- Timestamp
- Conversation state data

### Basic Conversation Commands

1. Continue a conversation:
   ```bash
   # Continue from the last conversation (default behavior)
   cpe "Your follow-up question"

   # Continue with custom generation parameters
   cpe -temperature 0.7 -max-tokens 4000 "Your question"

   # Start a new conversation (ignore previous)
   cpe -new "Start fresh conversation"

   # Continue from a specific conversation ID
   cpe -continue abcdef "Your question"
   ```

   When continuing conversations:
   - By default, continues from the most recent conversation
   - The model is automatically inferred from the previous conversation
   - Generation parameters must be specified each time if needed
   - The conversation history is preserved

2. List all conversations:
   ```bash
   cpe -list-convo
   ```
   This displays a table with:
   - Conversation IDs (6 lowercase letters)
   - Parent IDs (for threaded conversations)
   - Models used
   - Timestamps
   - Message previews
   
   Conversations are listed in reverse chronological order (newest first).

3. Print a specific conversation:
   ```bash
   cpe -print-convo abcdef
   ```

4. Delete conversations:
   ```bash
   # Delete a single conversation (will fail if it has children)
   cpe -delete-convo abcdef

   # Delete a conversation and all its children
   cpe -delete-convo abcdef -cascade
   ```

### Conversation Threading

Conversations in CPE form a tree structure where:
- Each conversation has a unique ID
- Conversations can have one parent and multiple children
- When continuing a conversation:
  - A new conversation is created with a new ID
  - The new conversation is linked to its parent
  - The entire conversation history is preserved
  - The same model is used by default
- When deleting conversations:
  - Single conversations cannot be deleted if they have children
  - Use `-cascade` to delete a conversation and all its descendants
  - Deletion is performed in a transaction to maintain consistency

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
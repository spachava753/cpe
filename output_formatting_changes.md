# Output Formatting Improvements for CPE

## Changes Made

### 1. User and Assistant Message Formatting
- Added Markdown headings with emojis for users (`### 👤 USER`) and assistants (`### 🤖 ASSISTANT`)
- Added better spacing with double newlines between messages for improved readability
- Made formatting consistent across all executor types (Anthropic, OpenAI, Gemini, Deepseek)

### 2. Tool Call Formatting
- Formatted tool calls with bold headers and code formatting: `**Tool Call**: [tool name]`
- Added JSON syntax highlighting for tool inputs with code blocks:
  ```json
  {
    "parameter": "value"
  }
  ```

### 3. Tool Result Formatting
- Added code blocks (triple backticks) around all tool results for improved readability
- Added special formatting for error messages with warning emoji: `> ⚠️ Error message`
- Added consistent markdown formatting for file editor and bash tool outputs

### 4. File Content Display Improvements
- Added language-specific syntax highlighting in code blocks based on file extensions
- Improved code block formatting with proper newlines and indentation
- Added improved language detection for common file types (.go, .js, .py, .java, .ts, etc.)

## Benefits

1. **Improved Readability**: Clear visual separation between user messages, assistant responses, and tool outputs.
2. **Better Code Representation**: Syntax highlighting for code blocks based on language.
3. **Error Visibility**: Warning indicators for error messages make issues more noticeable.
4. **Consistent Styling**: Uniform formatting across all output types and executor implementations.

These improvements make the CPE output much more readable and user-friendly, with clear visual cues to distinguish between different components of the conversation.
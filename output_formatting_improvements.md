# Output Formatting Improvements for CPE

## Changes Made

### 1. User and Assistant Message Formatting
- Added consistent Markdown headings with emojis for users (`### 👤 USER`) and assistants (`### 🤖 ASSISTANT`)
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
- Added special handling for file editor success messages
- Added consistent markdown formatting for file editor and bash tool outputs

### 4. Language-Specific Syntax Highlighting
- Added syntax highlighting for bash command outputs based on content detection:
  - Go code: ```go
  - Python code: ```python
  - JavaScript code: ```javascript
  - Java code: ```java
  - HTML: ```html
  - CSS: ```css
  - and more
- Added expanded language detection for files in the file overview and related files tools
- Added XML syntax highlighting for the OpenAI reasoning executor's actions blocks

### 5. Improved Tool Output Detection
- Enhanced logic to identify output from specific tools like bash
- Used more sophisticated content analysis to apply the appropriate syntax highlighting
- Shared consistent formatting conventions across all executor implementations

## Benefits

1. **Improved Readability**: Clear visual separation between different message types makes the conversation flow easier to follow.
2. **Better Code Representation**: Language-specific syntax highlighting makes code snippets more readable and easier to understand.
3. **Error Visibility**: Warning indicators and special formatting for error messages make issues more noticeable.
4. **Consistent Styling**: Uniform formatting across all output types provides a more polished and professional appearance.
5. **Better Development Experience**: Code from file display and bash commands is now properly highlighted, making it easier to read and understand.

These improvements make the CPE output much more readable and user-friendly, with clear visual cues to distinguish between different components of the conversation.
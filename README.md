# CPE (Chat-based Programming Editor)

CPE is a Go-based tool designed to allow developers to leverage the power of AI to analyze, modify, and improve their Go codebase through natural language interactions.

## Features

- Integrates with multiple LLM providers:
  - OpenAI (GPT-4 and variants)
  - Anthropic (Claude-3 and variants)
  - Google (Gemini-1.5 and variants)
- Supports various code analysis and modification operations:
  - Analyzing existing code
  - Modifying existing files
  - Creating new files
  - Removing files
- Configurable model settings:
  - Choice of AI model
  - Adjustable generation parameters (temperature, max tokens, etc.)
- Smart code analysis:
  - Automatic selection of relevant files for analysis
  - Option to include specific files in the analysis
- Flexible input methods:
  - Accept input from stdin or file
- Debugging support with system prompt visibility
- Version information
- Intelligent file ignoring based on .cpeignore patterns (if implemented)

## Installation

To install CPE, make sure you have Go 1.22 or later installed on your system. Then, run the following command:

```
go install github.com/spachava753/cpe@latest
```

## Usage

To use CPE, run the following command:

```
cpe [flags] < input.txt
```

or

```
cpe [flags] -input input.txt
```

### Flags

- `-model <model_name>`: Specify the model to use. Supported models: claude-3-opus, claude-3-5-sonnet, claude-3-5-haiku, gemini-1.5-flash, gemini-1.5-pro, gpt-4o, gpt-4o-mini. Default is "claude-3-5-sonnet".
- `-openai-url <custom_url>`: Specify a custom base URL for the OpenAI API.
- `-max-tokens <int>`: Maximum number of tokens to generate.
- `-temperature <float>`: Sampling temperature (0.0 - 1.0).
- `-top-p <float>`: Nucleus sampling parameter (0.0 - 1.0).
- `-top-k <int>`: Top-k sampling parameter.
- `-frequency-penalty <float>`: Frequency penalty (-2.0 - 2.0).
- `-presence-penalty <float>`: Presence penalty (-2.0 - 2.0).
- `-number-of-responses <int>`: Number of responses to generate.
- `-debug`: Print the generated system prompt.
- `-input <file_path>`: Specify the input file path. Use '-' for stdin (default).
- `-include-files <file_list>`: Comma-separated list of file paths to include in the system message.
- `-version`: Print the version number and exit.

### Examples

1. Basic usage with default model:
   ```
   echo "Analyze the main.go file" | cpe
   ```

2. Using a specific model and custom temperature:
   ```
   cpe -model gpt-4o -temperature 0.8 < input.txt
   ```

3. Including specific files in the analysis:
   ```
   cpe -include-files main.go,flags.go -input instructions.txt
   ```

4. Using a custom OpenAI URL:
   ```
   cpe -model gpt-4o -openai-url https://custom-openai-endpoint.com/v1 < input.txt
   ```

CPE will analyze your project, process your request, and perform the necessary file operations based on the input provided.

## Supported LLM Providers

CPE supports the following LLM providers and models:

- Anthropic:
  - claude-3-opus
  - claude-3-5-sonnet
  - claude-3-5-haiku
- Google:
  - gemini-1.5-flash
- OpenAI:
  - gpt-4o
  - gpt-4o-mini

To use a specific provider, make sure you have the corresponding API key set in your environment variables:

- Anthropic: `ANTHROPIC_API_KEY`
- Google: `GEMINI_API_KEY`
- OpenAI: `OPENAI_API_KEY`

## File Operations

CPE can perform the following file operations based on LLM suggestions:

- Modify existing files
- Remove files
- Create new files
- Rename files
- Move files
- Create directories

These operations are executed after validation to ensure safety and correctness.

## Configuration

CPE uses a `.cpeignore` file to specify patterns for files and directories that should be ignored during project analysis. This file follows a similar format to `.gitignore`.

## Contributing

Contributions to CPE are welcome! Please follow these steps to contribute:

1. Fork the repository
2. Create a new branch for your feature or bug fix
3. Make your changes and commit them with clear, descriptive messages
4. Push your changes to your fork
5. Create a pull request to the main repository

Please ensure your code adheres to the project's coding standards and includes appropriate tests.

## License

This project is licensed under the [MIT License](LICENSE).
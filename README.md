# CPE (Chat-based Programming Editor)

CPE is a Go-based tool designed to allow developers to leverage the power of AI to analyze, modify, and improve their Go codebase through natural language interactions.

## Features

- Integrates with multiple LLM providers (OpenAI, Anthropic, Google's Gemini)
- Supports various file operations:
  - Modifying existing files
  - Creating new files
  - Removing files
  - Renaming files
  - Moving files
  - Creating directories
- Configurable model settings
- Intelligent file ignoring based on .cpeignore patterns

## Installation

To install CPE, make sure you have Go 1.22 or later installed on your system. Then, run the following command:

```
go install github.com/spachava753/cpe@latest
```

## Usage

To use CPE, run the following command:

```
cpe -model <model_name>
```

Where `<model_name>` is one of the supported models (e.g., "claude-3-5-sonnet", "gpt-4o", "gemini-1.5-flash"). If no model is specified, it defaults to "claude-3-5-sonnet".

Provide your instructions or queries via stdin. CPE will analyze your project, process your request, and perform the necessary file operations.

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
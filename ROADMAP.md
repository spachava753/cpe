# CPE (Chat-based Programming Editor) Roadmap

This document outlines the planned features, improvements, and notes for the CPE project. Items are organized by priority and status.

## Features

### Agentic flow
- [ ] Move from disparate mulit-agent to single-agent, will reduce necessary calls, as we can remove the needs codebase function call
  - [ ] Will have a separate sub-agent to retrieve relevant text snippets

### Token Analysis and Visualization
- [x] Implement basic token counting per file
- [x] Show total tokens for each directory (tree)
- [ ] Improve token count visualization
  - [ ] Add color coding for token count
    - use statistical methods to calculate outliers and highlight in red?
    - and do this recursively

### Code Map
- [ ] Some repos are too large to fit in a context window, even without function bodies, so we should process the codebase in chunks if it exceeds the context window
- [ ] Instead of detecting file extensions, try to detect if file of text context using magic bytes

### Tooling
- [ ] Add support for bash execution tool

### LLM Integration
- [ ] Support for more LLM providers
  - [ ] Mistral
  - [ ] Deepseek
  - [ ] Nous
- [ ] support multimodality
  - [ ] images
  - [ ] videos
- [ ] Use official sdks instead for openai, gemini
  - [ ] openai
  - [ ] gemini
- [ ] Use structured outputs for openai and gemini to ensure strict following of tool schemas.

### Observability
- [ ] Export each cpe command run to a tracing tool
  - [ ] jsonl files?
  - [ ] wandb

### User Experience
- [ ] Command auto-completion
- [ ] Add support for continuing a conversation if user chooses to do so
- [ ] Convert token-count flag to sub command
- [ ] Support sending requests to multiple models and picking the best one

### Performance
- [ ] Parallel processing for large codebases

### Documentation
- [ ] Comprehensive user guide
- [ ] Example use cases and tutorials
- [ ] Contributing guidelines
- [ ] Architecture documentation

## Goals

- [ ] Test against extremely large mono-lang codebases
  - [ ] cert-manager
  - [ ] kubernetes
  - [ ] SWE bench?
- [ ] Test against extremely large multi-lang codebases
  - ???
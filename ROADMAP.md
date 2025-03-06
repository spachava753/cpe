# CPE (Chat-based Programming Editor) Roadmap

This document outlines the planned features, improvements, and notes for the CPE project. Items are organized by priority and status.

## Features

### System prompt improvements

- [ ] Add system info to path
  - Current Date
  - Current working directory
  - Operating system
  - In a git repository
- [ ] Remove model preference to summarize changes at the end of assistant turn, just wastes tokens

### Agentic flow
- [x] Move from disparate mulit-agent to single-agent, will reduce necessary calls, as we can remove the needs codebase function call
- [ ] 
- [ ] Experiment with idea of sub agent creation on the fly?
  - Models like Gemini such at editing files with function calling. Maybe just have the model utilize the bash tool to give specific edit instructions to a file-editing function calling capable model? like gpt-4o-mini?

### Token Analysis and Visualization
- [x] Implement basic token counting per file
- [x] Show total tokens for each directory (tree)
- [ ] Model specific token counting
  - [ ] Use anthropic token counting endpoint
  - [ ] Default to using gpt-4o token counting

### Code Map
- [ ] Some repos are too large to fit in a context window, even without function bodies, so we should process the codebase in chunks if it exceeds the context window
  - [ ] Or maybe we can create further levels of fidelity hierarchy. Maybe something like:
    1. File paths (for extremely large repos, or maybe just in arbitrary directories?)
    2. Only type, function and method names (and class names for object-oriented langs)
    3. Code comments, global variables, full type definitions (like all struct fields, tags, field comments, etc.) (this is the current lowest fidelity level today)
    4. Full file contents
- [x] Instead of detecting file extensions, try to detect if file of text context using magic bytes
- [ ] Include code from dependencies if possible in file overview
- [ ] Use model token counting to return an error to the model if given level of file overview will exceed to context window (or maybe given threshold? 75% of context window to allow for some room?)

### Getting related files
- [ ] Transition from using tree sitter for go to using native ast pkg in stdlib
- [ ] Explore using python in wasm runtime to resolve related files (RustPython?)
- [ ] Explore using java in wasm runtime to resolve related files

### Tooling
- [x] Add support for bash execution tool
- [ ] Add autocorrection if input JSON from model does not match schema (this is mostly valid for Claude and opensource
  models, we want to enforce schema adherence with structured outputs in openai and gemini)

### LLM Integration
- [ ] Support for more LLM providers
  - [ ] Mistral
  - [x] Deepseek
  - [ ] Nous
- [x] support multimodality
  - [z] images
  - [x] videos
- [x] Use official sdks instead for openai, gemini
  - [x] openai
  - [x] gemini
  - [x] anthropic
- [ ] Use structured outputs for openai and gemini to ensure strict following of tool schemas.

### User Experience
- [ ] Command auto-completion
- [x] Add support for continuing a conversation if user chooses to do so
- [ ] Support sending requests to multiple models and picking the best one
- [ ] Add retries to increase robustness (retry on 500s, connection issues, etc.)

### Conversation managment
- [ ] need to support some method of context compression, like truncating file full contents in previous messages, remove error tool calls, summary, etc.

### Performance
- [ ] Parallel processing for large codebases

### Documentation
- [x] Comprehensive user guide
- [x] Example use cases and tutorials
- [ ] Contributing guidelines
- [ ] Architecture documentation

## Goals

- [ ] Test against extremely large mono-lang codebases
  - [ ] cert-manager
  - [ ] kubernetes
  - [ ] SWE bench?
- [ ] Test against extremely large multi-lang codebases
  - ???
- [ ] Exceed Claude Code in SWE-Verified (70%)
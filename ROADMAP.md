# CPE (Chat-based Programming Editor) Roadmap

This document outlines the planned features, improvements, and notes for the CPE project. Items are organized by priority and status.

## Features

### MCP

- [x] turn CPE into a MCP client
- [ ] export all custom tools as a mcp server

### Agentic flow
- [ ] Experiment with idea of sub agent creation on the fly?
  - Models like Gemini suck at editing files with function calling. Maybe just have the model utilize the bash tool to
    give specific edit instructions to a file-editing function calling capable model? like gpt-4o-mini?
- [ ] Allow for black listing/white listing of commands for bash to be able to execute using regex

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
- [ ] Use model token counting to return an error to the model if given level of file overview will exceed to context window (or maybe given threshold? 75% of context window to allow for some room?)

### Code Graph

- [ ] get_related_files is useful for model when doing discovery and editing files, so the model is able to retrieve a
  much more complete context, reducing the amount of hallucinations or the amount of needed tool calls required when
  editing code files. However, we can take this a step further and construct a code graph on start up to support
  features like with fuzzy symbol matching, function/method signature searching, and symbol neighbor lookup to allow for
  LLMs to really be precise in what kind of context they are looking for. This may even be extended to returning code
  from dependencies

### Editing files

- [ ] When an LLM edits files, it can frequently get mess up the edits, perhaps due to outdated in-context
  representation of the file, or just perhaps making a mistake when editing a file. We should provide a simple signal to
  the LLM if it introduces a syntax error when editing files. Read a file into memory, apply the edit, parse the file
  with tree-sitter and check for parsing errors, and if any parsing errors exist, return an erroneous result to the LLM

### LLM Integration
- [ ] Use structured outputs for openai and gemini to ensure strict following of tool schemas.

### User Experience
- [ ] Command auto-completion
- [x] Add support for continuing a conversation if user chooses to do so

### Performance
- [ ] Parallel processing for large codebases

### Documentation
- [x] Comprehensive user guide
- [x] Example use cases and tutorials
- [ ] Contributing guidelines

## Goals

- [ ] Test against extremely large mono-lang codebases
  - [ ] cert-manager
  - [ ] kubernetes
  - [ ] SWE bench?
- [ ] Test against extremely large multi-lang codebases
  - ???
- [ ] Exceed Claude Code in SWE-Verified (70%)
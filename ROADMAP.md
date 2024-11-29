# CPE (Chat-based Programming Editor) Roadmap

This document outlines the planned features, improvements, and notes for the CPE project. Items are organized by priority and status.

## Features

### Token Analysis and Visualization
- [x] Implement basic token counting per file
- [ ] Show total tokens for each directory (tree)
- [ ] Improve token count visualization
  - [ ] Add color coding for token count
    - use statistical methods to calculate outliers and highlight in red?
    - and do this recursively

### Code Map
- [ ] Some repos are too large to fit in a context window, even without function bodies, so we should process the codebase in chunks if it exceeds the context window

### Code Analysis
- [ ] Implement dependency graph visualization
- [ ] Support for more programming languages

### LLM Integration
- [ ] Support for more LLM providers
  - [ ] Mistral
  - [ ] Deepseek
  - [ ] Nous
- [ ] Add support for fine grained model selection similar to aider
  - [ ] "file selection" model for codemap -> relevant files part of flow
  - [ ] "coding" model for codebase analysis/modification

### Observability
- [ ] Export each cpe command run to a tracing tool
  - [ ] jsonl files?
  - [ ] wandb

### User Experience
- [ ] Command auto-completion

### Performance
- [ ] Parallel processing for large codebases
- [ ] Caching mechanism for token counts

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
---

Last updated: [Current Date]
Feel free to suggest additions or modifications to this roadmap by opening an issue or pull request.
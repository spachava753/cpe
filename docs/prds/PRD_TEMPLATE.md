# Product Requirements Document Template

## Executive Summary

Provide a concise overview of what this PRD aims to accomplish. State whether you are:
- Introducing a new feature
- Refactoring an existing feature
- Sunsetting a feature
- Making breaking changes

For breaking changes, clearly mark them with **BREAKING** in this section.

## Background and Problem Statement

### Current State

What is the current feature state? Ground your answers with specific references:
- Reference relevant files in the codebase (e.g., `internal/config/`, `cmd/root.go`)
- Link to existing documentation
- Describe current implementation patterns and structures

### Pain Points

From the perspective of a CPE user, what pain points led to this PRD? Number each pain point for clarity:

1. **Pain point title**: Description
2. **Pain point title**: Description
3. ...

## Goals and Outcomes

### Goals

List specific, measurable goals (numbered for clarity):

1. Goal description
2. Goal description
3. ...

### Outcomes

What specific outcomes will users experience after implementation? Write outcomes as user capabilities:

After implementation, users will be able to:
- Outcome 1
- Outcome 2
- ...

## Requirements

### Functional Requirements

List functional requirements in numbered sections with subsections for clarity:

1. **Requirement Category**
    - Specific requirement
    - Specific requirement
    - ...

2. **Requirement Category**
    - ...

**Note**: Since CPE is in early development stages, unless explicitly requested, do not consider migration support or backwards compatibility. Mark any breaking changes with **BREAKING** prefix.

### Non-Functional Requirements

List non-functional requirements relevant to the feature:

1. **Reliability**
    - Requirement
    - ...

2. **Maintainability**
    - Requirement
    - ...

**Note**: CPE is a CLI tool where execution time is dominated by network calls to AI services. Performance considerations are typically not relevant unless specifically requested.

## Technical Design

Provide technical design details including:

### Architecture Overview

High-level description of the architectural changes or additions.

### Code Structure

```go
// Provide representative code snippets showing:
// - New types/structs
// - Key interfaces
// - Critical function signatures
```

### Configuration Changes

Show configuration schema changes if applicable:

```yaml
# Example configuration format
```

### Integration Points

List where and how this integrates with existing code:
- Module 1 (`path/to/file.go`): Description
- Module 2 (`path/to/file.go`): Description

## Implementation Plan

Provide a phase-by-phase implementation plan with **NO timelines**. Each phase should have clear deliverables:

1. **Phase 1: Title**
    - Task description
    - Task description
    - ...

2. **Phase 2: Title**
    - Task description
    - ...

Focus on logical ordering and dependencies between phases.

## Risks and Mitigations

Present risks in a table format for clarity:

| Risk | Mitigation |
|------|------------|
| Risk description | Mitigation strategy |
| Risk description | Mitigation strategy |

Alternatively, use numbered sections:

### Risk 1: Title
**Risk**: Description
**Mitigation**: 
- Strategy 1
- Strategy 2

## Documentation

List all documentation that requires updates:

### Required Documentation Updates

1. **README.md**: Description of changes needed
2. **AGENT.md**: Description of changes needed
3. **New Documentation**: What new documentation is needed

Include any specific sections or examples that need to be added/updated.

## Appendix

Use the appendix for:

### Example Usage
Concrete examples showing before/after usage patterns

### Reference Information
- Supported standards/specifications
- External library documentation
- Related RFCs or design docs

### Testing Strategy
- Unit test categories
- Integration test scenarios
- Performance benchmarks

### Future Considerations
Items that are out of scope but worth noting for future work

### Breaking Changes Summary
If applicable, clearly list all breaking changes in one section
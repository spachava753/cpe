# CPE MCP Server Mode Specification

This document outlines the design for "MCP Server Mode" in CPE, a feature that allows CPE to be exposed as an MCP server, enabling composition of AI agents as tools within other MCP-compliant environments.

## Motivation

As more MCP servers, connectors, and things like agent skills are put into the context of an agent, the context window for "smart" work grows shorter and shorter. It's been noted that long context seems to blunt the intelligence of the model, and so there are various methods for context management, one such technique being subagents. Subagents are helpful for context management and compression. Due to the rise of MCP servers exposing a multitude of tools, the tools given to an agent went from a couple of simplistic tools, to tens of tools that agents might often struggle to understand when to call and how to orchestrate. However, adding these tools often consume significant amounts of the context window, leading to more frequent context compaction/handoff steps, as well possibly causing errors more often, due to miscalled tool calls. Subagents help fix this by aggregating tools together and exposing a simpler interface to the parent agent. Subagents can take on the load of properly orchestrating tools, and simply return the result back to the parent agent, saving on the tool description token hit, as well as the multiple tool call and tool result turns. This naturally gives rise to certain patterns, especially in coding, such as codebase context gathering for tasks given by a user, or creating a plan before executing, etc.

In addition, subagents enable a powerful pattern of "parallel" intelligence, where multiple subagents can be invoked in parallel. This reduces latency (due to parallelism and shorter context windows), but also enables patterns like executing subagents to extract certain content for each given file. With the combination of code mode, we are not even limited to the parallel tool calling abilities of the parent agent—we can actually just use code mode exposed tools and for loops to execute "wide" intelligence.

It is preferred to use an MCP server over simply wrapping the current CPE CLI as a bash script, as then any MCP client can take advantage of this. Also, currently there is no structured extraction in place, while we will support that in MCP server mode. As an MCP server, we can also take advantage of protocol features like logging and notifications, to let the MCP client do the hard work of figuring out how to present subagent trajectories to the user. Also, MCP servers can handle rate limiting and retries automatically, which is important especially for code mode subagents, which can spin up and invoke any number of subagents, potentially in parallel.

### Inspirations

- [Building effective agents](https://www.anthropic.com/research/building-effective-agents) - Anthropic's research on agent patterns
- [Multi-agent research systems](https://www.anthropic.com/engineering/multi-agent-research-system) - Deep research through agent composition
- [Effective harnesses for long-running agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents) - Context management techniques
- [Don't Build Multi-Agents](https://cognition.ai/blog/dont-build-multi-agents) - Cognition's perspective on when agents add value
- [Agents for the Agent](https://ampcode.com/agents-for-the-agent) - Amp's approach to composable agents
- [KTLO](https://ghuntley.com/ktlo/) - Keeping the lights on with subagents

## Design

### Overview
MCP server mode provides a single entry point for invoking a *focused* subagent from the parent CPE session. The goal of the 0→1 version is to make subagents easy to author and reliable to run, by keeping configuration and runtime behavior intentionally small and predictable.

This mode is designed around task-specific subagents: each subagent is configured in its own config file, and that config is self-contained. This makes it straightforward to create different subagents for different workflows (e.g. “write tests”, “review PR”, “triage logs”) without building a complex multi-agent orchestration layer.

### Configuration
Each MCP server configuration defines exactly **one** subagent. If the user wants multiple subagents, they create multiple MCP server configs (each pointing at its own prompt/model/tooling).

The subagent inherits the parent server execution context by default, including current working directory (CWD), environment variables, and other process-level execution context. Configuration may *add* or *override* details (for example, additional environment variables), but the baseline assumption is that running a subagent “feels like” running inside the same session.

The configuration should remain minimal in the first iteration: enough to define the subagent’s identity and how it runs, but not an extensible matrix of tuning options. If a setting is not required to run real subagents in practice, it should be omitted until a concrete need emerges.

### CLI interface
MCP server mode is invoked via the CLI. The server runs as a long-lived process and communicates with its MCP client over **stdio**.

A typical invocation looks like:

```sh
cpe mcp serve --config ./coder_agent.cpe.yaml
```

The `--config` file defines the single subagent that will be exposed as an MCP tool. The tool name is the subagent’s `name`, and the tool description comes from the subagent’s `description`.

### Stdio mode and stdout discipline
The 0→1 design supports only **stdio** transport. This has an important operational consequence: **stdout is reserved for the MCP protocol stream**.

CPE must avoid writing human-readable logs, spinners, or progress output to stdout while in server mode, since any stray bytes can corrupt the protocol stream. Diagnostic output should go to stderr (or be omitted). When the server needs to return user-visible output, it should do so only as part of the MCP tool response payload.

### Example configurations
The 0→1 design intentionally favors *one subagent per config* so that each subagent can be checked into a repo and reasoned about independently. The examples below are intentionally small; they show the shape of a config rather than an exhaustive list of knobs.

A lightweight, thinking-focused agent (no code execution):

```yaml
# review_agent.cpe.yaml
version: "1.0"

models:
  - ref: opus
    display_name: "Claude Opus"
    id: claude-opus-4-20250514
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY

# Subagent identity only—behavior comes from defaults
subagent:
  name: review_changes
  description: Review a diff and return prioritized feedback.
  outputSchemaPath: ./schemas/review.json  # optional, for structured output

defaults:
  model: opus
  systemPromptPath: ./prompts/review.prompt
  codeMode:
    enabled: false
```

A code mode subagent (intended to make changes and run commands):

```yaml
# coder_agent.cpe.yaml
version: "1.0"

models:
  - ref: opus
    display_name: "Claude Opus"
    id: claude-opus-4-20250514
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY

# Subagent identity only
subagent:
  name: implement_change
  description: Make code changes, run tests, and report results.

defaults:
  model: opus
  systemPromptPath: ./prompts/coder.prompt
  codeMode:
    enabled: true

mcpServers:
  filesystem:
    command: filesystem-mcp
    type: stdio
  shell:
    command: shell-mcp
    type: stdio
```

### Example use cases
Subagents work best when they are narrowly scoped and repeatable. In practice this often means creating subagents around a *single* kind of outcome and baking the workflow into the system prompt.

A **diff reviewer** subagent can be used to produce consistent review feedback (and optionally a structured response) without giving it filesystem access.

A **test writer** subagent can be used to add tests for a specific package or feature. If it needs to run commands or edit files, that subagent can be a code mode subagent.

A **doc editor** subagent can be tuned to your documentation style and constraints (e.g. “only nest headings to `###`”, “avoid listicles”), making repeated doc work much more consistent.

A **CI/log triager** subagent can take a failing job log and return a minimal root-cause hypothesis and the next command to run, keeping the main agent from getting bogged down.

Because the subagent inherits CWD and environment by default, these subagents can be authored assuming they run against the repository and toolchain already available to the parent session.

### Composing subagents with code mode
Subagents become particularly powerful when used as a focus and capability boundary for code execution. A common pattern is to keep the parent agent more conversational and planning-oriented, and delegate “touch the filesystem / run commands” work to a dedicated code mode subagent.

This helps keep prompts smaller and responsibilities clearer: the code mode subagent can have a system prompt that emphasizes safe edits, running tests, and reporting results, while the parent concentrates on deciding *what* should be done.

A parent CPE config can consume a subagent by running it as an MCP server:

```yaml
# parent.cpe.yaml
mcpServers:
  repo_coder:
    command: cpe
    args: ["mcp", "serve", "--config", "./coder_agent.cpe.yaml"]
    type: stdio
```

Once configured, the parent can call the exposed tool (`implement_change`) with a minimal task input:

```json
{
  "prompt": "Add a regression test for issue #112 and run the test suite."
}
```


### Execution model
When the parent invokes the MCP tool, CPE launches the configured subagent and streams it the task input. The subagent runs to completion and returns a single result back to the parent.

If the subagent fails (tool error, non-zero exit, invalid output, etc.), that error is returned directly to the parent. The 0→1 design intentionally includes **no retries** and no attempt to “self-heal” failures; early implementations should bias toward transparency and debuggability over cleverness.

Subagent execution traces are persisted into the same `.cpeconvo` file as the parent session. This includes the subagent’s messages and tool calls, annotated sufficiently to distinguish subagent activity from the parent. Storage bloat, rotation, and other lifecycle concerns are explicitly out of scope for the initial implementation.

### Recursion
The 0→1 design does not add explicit guardrails around recursive invocation (e.g. a subagent invoking another subagent that happens to expose subagent tools).

This means we do not attempt to enforce maximum depth, detect cycles, or add recursion-specific diagnostics. If recursion leads to failure (timeouts, tool errors, invalid output, etc.), the error should simply propagate to the caller without retries.

### Output and schemas
If the parent expects structured output (i.e. an output schema is present), we assume the model is capable enough to use the `final_answer` tool correctly. The initial implementation should not add additional fallback strategies, retry loops, or observability-driven heuristics to coerce schema compliance.

## Architecture

### Process shape
MCP server mode is implemented as a single long-lived CLI process (`cpe mcp serve`) that communicates with its MCP client over **stdio**. In the 0→1 design, a server process exposes exactly **one** tool: the configured subagent.

```text
Parent (MCP client)
        │
        │ MCP protocol (stdio)
        ▼
┌──────────────────────────────┐
│ cpe mcp serve                │
│   tool: <subagent.name>      │
│            │                 │
│            ▼                 │
│        Subagent run          │
│    (code mode optional)      │
│            │                 │
│            ▼                 │
│   optional MCP servers/tools │
└──────────────────────────────┘
```

Because the transport is stdio, the server must treat stdout as protocol-only output and avoid printing human-readable content to stdout.

### Server startup
On startup, `cpe mcp serve` loads the config file, validates that a subagent is defined, and registers a single MCP tool using the subagent’s `name` and `description`. Any configuration errors (missing default model, missing API keys, unreadable prompt/schema paths) are treated as startup failures.

If the config includes MCP servers in the top-level `mcpServers` section, the server prepares to initialize them for use by the subagent. The initial implementation may choose lazy initialization (first use) to keep startup fast and failure modes obvious.

### Handling a tool call
Each MCP tool call creates a fresh subagent execution. The server constructs an agent run using the model and system prompt from `defaults`, injects the call’s `prompt` as the task input, and executes until completion.

The subagent inherits the server process execution context (CWD, environment variables, etc.). This makes subagents feel “co-located” with the parent session when the parent runs the server as a child process.

If code mode is enabled in `defaults.codeMode`, the run includes the code execution toolchain. If code mode is disabled, the subagent is constrained to MCP server tools and pure reasoning.

### Tooling and MCP servers
All MCP servers defined in the top-level `mcpServers` section are exposed to the subagent during its run. This enables focused compositions such as a code mode subagent with filesystem + shell tools, or a research subagent with only a web search tool.

The 0→1 design does not require observability hooks or protocol-level logging notifications. Any diagnostics produced by the server itself should be written to stderr so the MCP protocol stream on stdout remains valid.

### Results and errors
On success, the server returns the subagent’s result as the MCP tool response. If an output schema is configured, the subagent is expected to return structured data via `final_answer`. If no output schema is configured, the server returns the final assistant text.

On failure, the server returns the error directly to the caller. There are no retries or recovery policies in the initial implementation; failures should be explicit and easy for the parent to reason about.

### Conversation persistence
Subagent execution traces are persisted into the same `.cpeconvo` file as the parent session. Practically, this means the server appends the subagent’s messages and tool calls to the shared conversation log in a way that distinguishes subagent activity from parent activity (for example, by annotating entries with the subagent name and a per-invocation run ID).

The initial design does not attempt storage management (rotation, truncation, or compaction). The goal is to capture complete trajectories first and learn from real usage.

## Implementation tasks

This section defines ordered implementation tasks. Each task is a checklist item with dependencies, completion criteria, and testing methodology.

### Task 1 — Extend configuration schema with subagent definition

- [x] **Complete**

**Depends on:** None

**Files to modify:**
- `internal/config/config.go`: Add `SubagentConfig` struct with fields: `Name`, `Description`, `OutputSchemaPath` (optional)
- `internal/config/config.go`: Add `Subagent *SubagentConfig` field to `RawConfig` struct
- `internal/config/loader.go`: Update config loading to parse and validate subagent configuration
- `schema/cpe-config-schema.json`: Add JSON schema definitions for subagent fields

**Done when:**
- A config file with a `subagent:` block parses without error
- Config validation rejects missing required fields (`name`, `description`)
- Config validation verifies `outputSchemaPath` (if present) points to a valid file path
- `cpe config lint` validates subagent configuration correctly

**Tests:**
- *Unit (`internal/config/config_test.go`):* Add table-driven tests for valid and invalid subagent configs (missing name, missing description, invalid outputSchemaPath)
- *Unit (`internal/config/loader_test.go`):* Test loading of example subagent config files from testdata
- *Manual:* Run `cpe config lint` against valid and invalid subagent config files

---

### Task 2 — Add `cpe mcp serve` CLI command skeleton

- [x] **Complete**

**Depends on:** Task 1

**Files to modify:**
- `cmd/mcp.go`: Add `mcpServeCmd` cobra command with `--config` flag (required)
- `cmd/mcp.go`: Register `mcpServeCmd` under `mcpCmd`

**Done when:**
- `cpe mcp serve --help` displays usage information
- `cpe mcp serve` without `--config` prints error and exits non-zero
- `cpe mcp serve --config ./nonexistent.yaml` prints error about missing file
- `cpe mcp serve --config ./valid.yaml` where config has no subagent prints error about missing subagent

**Tests:**
- *Unit (`cmd/mcp_test.go`):* Test command parsing and flag validation
- *Manual:* Run `go build ./... && ./cpe mcp serve --help` and verify output

---

### Task 3 — Implement MCP server transport layer (stdio)

- [x] **Complete**

**Depends on:** Task 2

**Files to create:**
- `internal/mcp/server.go`: `Server` struct with `Serve(ctx context.Context) error` method
- `internal/mcp/server.go`: `NewServer(config *config.RawConfig, opts ServerOptions) *Server` constructor

**Implementation details:**
- Use `github.com/modelcontextprotocol/go-sdk/mcp` package's `mcp.NewServer` and `mcp.StdioTransport`
- Server must not write to stdout except for MCP protocol messages
- Diagnostic output goes to stderr or is suppressed
- Implement graceful shutdown on context cancellation and SIGINT/SIGTERM
- Close all resources (child MCP servers, goroutines) on shutdown

**Done when:**
- Server starts and blocks waiting for MCP messages
- Server shuts down cleanly on context cancellation
- Server shuts down cleanly on SIGINT
- No goroutine leaks (verified with `-race` flag)
- No output written to stdout during startup/shutdown

**Tests:**
- *Unit (`internal/mcp/server_test.go`):* Test server lifecycle: start, context cancel, verify clean shutdown
- *Unit:* Test that stdout remains clean (no non-protocol bytes)
- *Integration:* Spawn server as subprocess, send MCP `initialize` request over stdin, verify response on stdout
- *Manual:* Run `cpe mcp serve --config ./test.yaml`, press Ctrl+C, verify clean exit

---

### Task 4 — Register subagent as MCP tool

- [x] **Complete**

**Depends on:** Task 3

**Files to modify:**
- `internal/mcp/server.go`: Implement tool registration in `NewServer` or `Serve`

**Implementation details:**
- Register one tool with name from `subagent.name` and description from `subagent.description`
- Tool input schema: `{ prompt: string (required), inputs: string[] (optional) }`
- If `subagent.outputSchemaPath` is defined, load the JSON schema file and set it as the tool's output schema
- Use `mcp.Server.AddTool()` or equivalent from go-sdk

**Done when:**
- MCP `tools/list` request returns exactly one tool with correct name, description, and input schema
- If `subagent.outputSchemaPath` is configured, output schema is included in tool definition
- Invalid `subagent.outputSchemaPath` (missing file, invalid JSON) causes startup failure with clear error

**Tests:**
- *Unit (`internal/mcp/server_test.go`):* Test tool registration with various subagent configs
- *Integration:* Send `tools/list` MCP request, verify response matches expected schema
- *Manual:* Use `cpe mcp list-tools` against a running server to verify tool appears correctly

---

### Task 5 — Implement subagent execution on tool call

- [x] **Complete**

**Depends on:** Task 4

**Files to modify:**
- `internal/mcp/server.go`: Implement tool call handler
- `internal/mcp/server.go`: Create function `executeSubagent(ctx, prompt, inputs) (result, error)`

**Implementation details:**
- Parse tool call arguments (`prompt`, `inputs`)
- Load system prompt from `defaults.systemPromptPath` (reuse `agent.SystemPromptTemplate`)
- Resolve model from `defaults.model` reference to `config.Model`
- Use all MCP servers defined in top-level `mcpServers`
- Apply code mode settings from `defaults.codeMode`
- Reuse `agent.CreateToolCapableGenerator` to create the generator
- Reuse `commands.Generate` or create similar execution loop
- Process `inputs` array: for each path, read file content and add as `gai.Block` (similar to `processUserInput` in `cmd/root.go`)
- Subagent inherits CWD and environment from server process

**Done when:**
- Tool call with `{prompt: "Hello"}` executes subagent and returns response
- Tool call with `{prompt: "...", inputs: ["file.txt"]}` includes file content in context
- Subagent uses model, system prompt, code mode, and MCP servers from `defaults` and top-level config
- Errors during execution are returned as MCP tool error responses

**Tests:**
- *Unit (`internal/mcp/server_test.go`):* Test input parsing and file loading
- *Integration:* Full tool call with mock LLM response (if possible) or against real API
- *Manual:* Configure parent CPE to use the server, invoke tool, verify response

---

### Task 6 — Implement structured output via `final_answer` tool

- [x] **Complete**

**Depends on:** Task 5

**Files modified:**
- `internal/commands/subagent.go`: Register `final_answer` tool with nil callback, extract parameters from dialog
- `internal/agent/tool_result_printer.go`: Pass nil callbacks through without wrapping
- `cmd/mcp.go`: Load and validate output schema at startup

**Implementation details:**
- When `subagent.outputSchemaPath` is set, create a `final_answer` tool where input schema = output schema from file
- Register `final_answer` with a nil callback (leveraging gai behavior where nil callbacks terminate)
- On execution: run agent loop until `final_answer` is called
- Extract tool call parameters from the returned dialog as structured output
- Return extracted JSON as MCP tool result

**Done when:**
- Subagent with `subagent.outputSchemaPath` has `final_answer` tool available
- Agent calling `final_answer({...})` terminates execution
- Structured data from `final_answer` call is returned as tool result
- Invalid `final_answer` calls (schema mismatch) result in error

**Tests:**
- *Unit (`internal/mcp/server_test.go`):* Test final_answer tool creation and extraction logic
- *Integration:* Configure subagent with output schema, execute, verify structured response
- *Manual:* Test with real LLM to verify schema compliance

---

### Task 7 — Persist subagent execution traces to storage

- [x] **Complete**

**Depends on:** Task 5

**Files modified:**
- `cmd/mcp.go`: Initialize `storage.DialogStorage` for `.cpeconvo`, generate run IDs, pass storage to executor
- `internal/commands/subagent.go`: Add `Storage` and `SubagentLabel` fields to `SubagentOptions`, implement `saveSubagentTrace()`

**Implementation details:**
- Storage initialized in `cmd/mcp.go` using `storage.InitDialogStorage(".cpeconvo")`
- Each subagent invocation generates a unique 8-character run ID via `gonanoid`
- Messages annotated with label format `subagent:<name>:<run_id>` in the title field
- Persistence is non-blocking: errors logged to stderr but don't fail execution
- `saveSubagentTrace()` saves user message then chains assistant messages with parent IDs

**Done when:**
- Subagent execution creates entries in `.cpeconvo`
- Entries are distinguishable from parent agent entries (via label/metadata)
- `cpe conversation list` and `cpe conversation print` can display subagent traces

**Tests:**
- *Unit (`internal/storage/dialog_storage_test.go`):* Verify subagent-annotated messages are stored correctly
- *Integration:* Execute subagent, query database, verify entries exist with correct annotations
- *Manual:* Run subagent, use `cpe conversation print <id>` to view trace

---

### Task 8 — Implement MCP logging notifications for observability (optional)

- [ ] **Complete**

**Depends on:** Task 5

**Files to modify:**
- `internal/mcp/server.go`: Implement logging notification sender
- `internal/mcp/server.go`: Emit `notifications/message` for execution events

**Implementation details:**
- On each significant event, send MCP `notifications/message` to client
- Event types: `tool_call`, `tool_result`, `response`, `message_saved`
- Log levels: `debug` (reasoning), `info` (tool calls, results), `warning` (recoverable errors), `error` (failures)
- Logger name format: `subagent.<name>`
- `message_saved` event includes `messageId`, `role`, `parentId`

**Done when:**
- Parent MCP client receives log notifications during subagent execution
- Notifications include correct level, logger, and structured data
- Notifications do not block or slow down execution (fire-and-forget)

**Tests:**
- *Unit (`internal/mcp/server_test.go`):* Test notification formatting and emission
- *Integration:* MCP client harness captures notifications, verifies expected events
- *Manual:* Use MCP client that displays logs, observe real-time subagent activity

---

### Task 9 — End-to-end integration with parent CPE

- [x] **Complete**

**Depends on:** Tasks 5, 6, 7

**Files to create:**
- `examples/subagent/`: Example subagent configurations and prompts
- `docs/subagents.md`: Documentation for subagent authoring

**Implementation details:**
- Create example subagent config (`examples/subagent/review.cpe.yaml`)
- Create example parent config that references subagent as MCP server
- Document the workflow: create subagent config → start server → configure parent

**Done when:**
- Example parent config can spawn subagent server: `mcpServers: { review: { command: cpe, args: [mcp, serve, --config, ./review.cpe.yaml] } }`
- Parent CPE can invoke subagent tool successfully
- Documentation explains subagent creation, configuration, and usage patterns
- Troubleshooting section covers common failures

**Tests:**
- *E2E:* Documented workflow runs successfully on clean checkout
- *Manual:* Follow documentation from scratch, verify subagent works
- *CI (optional):* Automated smoke test with mock or real LLM

---

### Task 10 — Error handling, edge cases, and hardening

- [x] **Complete**

**Depends on:** Tasks 5, 6

**Files to modify:**
- `internal/mcp/server.go`: Comprehensive error handling throughout

**Implementation details:**
- Tool call errors return structured MCP error responses (not panics)
- Missing/unreadable files (system prompt, output schema, input files) produce clear errors
- API errors (rate limits, auth failures) are surfaced clearly
- Timeout handling: subagent execution respects context deadline
- No retries in initial implementation (explicit failure is preferred)

**Done when:**
- All error paths return actionable error messages
- No panics from invalid inputs
- Context cancellation is handled at all blocking points
- Errors include enough context for debugging (file paths, tool names, etc.)

**Tests:**
- *Unit (`internal/mcp/server_test.go`):* Table-driven tests for error conditions
- *Integration:* Trigger various failures (missing files, invalid configs), verify error responses
- *Manual:* Test failure modes: kill API mid-request, provide invalid inputs

---

### Task 11 — Documentation and README updates

- [ ] **Complete**

**Depends on:** Task 9

**Files to modify:**
- `README.md`: Add MCP Server Mode section with quickstart
- `docs/specs/mcp_server_mode.md`: Mark specification as implemented, add any deviations

**Done when:**
- README includes minimal quickstart for server mode
- Spec document is updated with implementation notes
- Configuration reference documents `subagent` fields
- Link from README to detailed spec/docs

**Tests:**
- *Manual:* Follow README quickstart on clean system, verify it works
- *Review:* Documentation reviewed for accuracy and completeness

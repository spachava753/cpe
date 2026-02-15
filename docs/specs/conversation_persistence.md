# CPE Conversation Persistence Specification

This document specifies the design and implementation of conversation persistence in CPE, a feature that enables storing, retrieving, and managing conversation history across sessions.

## Overview

Conversation persistence allows CPE to maintain a history of interactions between users and AI models. This enables:

1. **Session Continuity**: Continue conversations from where you left off
2. **Conversation Forking**: Branch off from any point in a conversation to explore different directions
3. **History Inspection**: Review past conversations and their outcomes
4. **Subagent Tracing**: Track execution traces of subagents for debugging and observability

Conversations are stored locally in a SQLite database file (`.cpeconvo`) in the current working directory. The tree-based data model supports non-linear conversation flows where multiple branches can extend from any point.

## Storage Backend

### SQLite Database

CPE uses SQLite as the storage backend, providing:

- **Portability**: Single file that can be easily moved or backed up
- **Reliability**: ACID transactions ensure data integrity
- **Performance**: Efficient for the expected workload of conversational data
- **Simplicity**: No external database server required

The database file is named `.cpeconvo` and is created in the current working directory. This allows different projects to maintain separate conversation histories.

### Schema Design

The schema consists of two primary tables: `messages` and `blocks`. This design separates message metadata from content, allowing efficient queries on message structure while supporting rich multimedia content.

```sql
-- messages table stores conversation message metadata
CREATE TABLE IF NOT EXISTS messages
(
    id                TEXT PRIMARY KEY,          -- 6-character alphanumeric ID
    parent_id         TEXT,                      -- Reference to parent message (NULL for roots)
    title             TEXT,                      -- Optional label for conversation branches
    role              TEXT    NOT NULL,          -- 'user', 'assistant', or 'tool_result'
    tool_result_error BOOLEAN NOT NULL DEFAULT 0,-- Whether tool result indicates an error
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (parent_id) REFERENCES messages (id) ON DELETE RESTRICT
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages (created_at);
CREATE INDEX IF NOT EXISTS idx_messages_parent_id ON messages (parent_id);

-- blocks table stores message content
CREATE TABLE IF NOT EXISTS blocks
(
    id             TEXT,                      -- Optional block ID (for tool calls)
    message_id     TEXT      NOT NULL,        -- Parent message reference
    block_type     TEXT      NOT NULL,        -- 'content', 'tool_call', 'thinking', etc.
    modality_type  INTEGER   NOT NULL,        -- 0=Text, 1=Image, 2=Audio, 3=Video
    mime_type      TEXT      NOT NULL,        -- Content MIME type
    content        TEXT      NOT NULL,        -- Actual content (text or base64 for binary)
    extra_fields   TEXT,                      -- JSON-encoded additional metadata
    sequence_order INTEGER   NOT NULL,        -- Block ordering within message
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (message_id, sequence_order),
    FOREIGN KEY (message_id) REFERENCES messages (id) ON DELETE CASCADE
);
```

### Key Design Decisions

1. **Tree Structure**: Messages form trees via `parent_id` references. Root messages have `NULL` parent_id.

2. **Cascade Deletes**: Blocks are automatically deleted when their parent message is deleted (`ON DELETE CASCADE`). Messages prevent deletion if they have children (`ON DELETE RESTRICT`).

3. **Short IDs**: Message IDs are 6-character alphanumeric strings generated using nanoid, making them easy to type and reference in CLI commands.

4. **Content Separation**: Message content is stored in blocks, allowing a single message to contain multiple content types (text, images, tool calls, etc.).

## Message Model

CPE uses the message types from the [gai](https://github.com/spachava753/gai) library for representing conversations.

### Message Structure

Each message in the dialog corresponds to a single turn in the conversation (from `gai.Message`):

```go
type Message struct {
    Role            Role           // User, Assistant, or ToolResult
    Blocks          []Block        // Content blocks
    ToolResultError bool           // Whether tool result indicates an error
    ExtraFields     map[string]any // Runtime metadata (not persisted to DB)
}
```

### ExtraFields Keys

Message-level `ExtraFields` are runtime metadata — they are **not** persisted to the database. The storage package populates them when returning messages so consumers can access message identity and lineage without coupling to the storage internals.

| Constant | Value | Description |
|----------|-------|-------------|
| `storage.MessageIDKey` | `"cpe_message_id"` | The message's unique identifier. Always set on returned messages. Used by the saving middleware to track which messages have been persisted. |
| `storage.MessageParentIDKey` | `"cpe_message_parent_id"` | The ID of the parent message. Only set when the message has a parent (not set for root messages). |
| `storage.MessageCreatedAtKey` | `"cpe_message_created_at"` | The message's creation timestamp (`time.Time`). Always set on returned messages. Used by tree display for sorting. |

These constants are defined in `internal/storage/interfaces.go`.

### Roles

- **User** (`user`): Messages from the human user
- **Assistant** (`assistant`): Responses from the AI model
- **ToolResult** (`tool_result`): Results from tool executions

### Block Structure

Each block represents a unit of content within a message (from `gai.Block`):

```go
type Block struct {
    ID           string         // Optional identifier (used for tool call correlation)
    BlockType    string         // 'content', 'tool_call', 'thinking', etc.
    ModalityType Modality       // Text, Image, Audio, or Video
    MimeType     string         // Content MIME type (e.g., "text/plain", "image/png")
    Content      fmt.Stringer   // Actual content (implements fmt.Stringer interface)
    ExtraFields  map[string]any // Additional metadata (JSON-encoded in storage)
}
```

Note: Block-level `ExtraFields` **are** persisted to the database (as JSON in the `extra_fields` column), unlike message-level `ExtraFields`.

### Block Types

- **Content**: Regular message content (text, images, etc.)
- **ToolCall**: A tool invocation request from the assistant
- **Thinking**: Model reasoning/thinking traces

### Modality Types

| Value | Name  | Description |
|-------|-------|-------------|
| 0     | Text  | Plain text content |
| 1     | Image | Image data |
| 2     | Audio | Audio data |
| 3     | Video | Video data |

### Parent-Child Relationships

Messages form a tree structure:

```
Root Message (user: "Hello")
    └── Assistant Response ("Hi! How can I help?")
        ├── User Follow-up 1 ("Tell me about X")
        │   └── Assistant Response 1a
        └── User Follow-up 2 ("Tell me about Y")
            └── Assistant Response 2a
```

This allows conversation forking—branching from any point to explore different conversation paths.

### Labels (Titles)

The `title` field allows labeling conversation branches for identification. This is particularly useful for:

- **Subagent traces**: Labels formatted as `subagent:<name>:<run_id>` distinguish subagent execution traces from main conversation
- **Topic markers**: User-provided labels to mark conversation topics

## Public API

### Interface Hierarchy

The storage package exposes a `MessageDB` interface composed of four single-method interfaces. Consumers depend on the narrowest interface they need, enabling clean dependency boundaries and easy testing.

```go
// MessagesSaver persists messages to storage.
type MessagesSaver interface {
    SaveMessages(ctx context.Context, opts []SaveMessageOptions) (iter.Seq[string], error)
}

// MessagesGetter fetches specific messages by ID.
type MessagesGetter interface {
    GetMessages(ctx context.Context, messageIDs []string) (iter.Seq[gai.Message], error)
}

// MessagesLister lists messages from storage with ordering and pagination.
type MessagesLister interface {
    ListMessages(ctx context.Context, opts ListMessagesOptions) (iter.Seq[gai.Message], error)
}

// MessagesDeleter deletes messages from storage.
type MessagesDeleter interface {
    DeleteMessages(ctx context.Context, opts DeleteMessagesOptions) error
}

// MessageDB is the unified interface for message persistence operations.
type MessageDB interface {
    MessagesSaver
    MessagesDeleter
    MessagesLister
    MessagesGetter
}
```

The concrete `*DialogStorage` type implements `MessageDB`. Consumers use the subset interface they need:

| Consumer | Interface Used |
|----------|---------------|
| `agent.SavingMiddleware` | `storage.MessagesSaver` |
| `agent.WithDialogSaver()` | `storage.MessagesSaver` |
| `commands.GenerateOptions` | `storage.MessageDB` |
| `commands.SubagentOptions.Storage` | `storage.MessagesSaver` |
| `commands.ConversationListOptions` | `storage.MessagesLister` |
| `commands.ConversationDeleteOptions` | `storage.MessagesDeleter` |
| `commands.ConversationPrintOptions` | `storage.MessagesGetter` |

### Parameter Types

```go
// SaveMessageOptions describes a single message to be persisted.
type SaveMessageOptions struct {
    Message  gai.Message // The message to save (Role, Blocks, ToolResultError)
    ParentID string      // Parent message ID (empty for root messages)
    Title    string      // Optional label (e.g., "subagent:<name>:<run_id>")
}

// ListMessagesOptions configures message listing behavior.
type ListMessagesOptions struct {
    Offset         uint // Number of messages to skip (pagination)
    AscendingOrder bool // false = newest first (default), true = oldest first
}

// DeleteMessagesOptions configures a message deletion operation.
type DeleteMessagesOptions struct {
    MessageIDs []string // IDs of messages to delete
    Recursive  bool     // true = delete entire subtree, false = fail if message has children
}
```

### Saving Messages

```go
func (s *DialogStorage) SaveMessages(
    ctx context.Context,
    opts []SaveMessageOptions,
) (iter.Seq[string], error)
```

Saves one or more messages atomically in a single transaction. Returns an `iter.Seq[string]` that yields generated IDs in the same order as the input slice.

The function:
1. Begins a transaction
2. For each message: generates a unique 6-character ID, inserts the message record, inserts all blocks with sequence ordering
3. Commits the transaction
4. Returns an iterator over the generated IDs

If any message fails to save, the entire transaction is rolled back and no messages are persisted.

### Retrieving Messages

```go
func (s *DialogStorage) GetMessages(
    ctx context.Context,
    messageIDs []string,
) (iter.Seq[gai.Message], error)
```

Retrieves messages by their IDs. Each returned `gai.Message` has `MessageIDKey`, `MessageCreatedAtKey`, and (if applicable) `MessageParentIDKey` populated in its `ExtraFields`. Returns an error if any requested ID does not exist.

### Reconstructing Dialogs

```go
// Standalone function (not a method on DialogStorage)
func GetDialogForMessage(
    ctx context.Context,
    getter MessagesGetter,
    messageID string,
) (gai.Dialog, error)
```

Reconstructs the full conversation history leading up to the given message by traversing up the parent chain via `MessagesGetter`. Returns the dialog ordered from root to the target message. Each message has `MessageIDKey`, `MessageCreatedAtKey`, and `MessageParentIDKey` in its `ExtraFields`.

This is a standalone function in the `storage` package, not a method on `DialogStorage`. It operates through the `MessagesGetter` interface, making it testable with any implementation.

### Listing Messages

```go
func (s *DialogStorage) ListMessages(
    ctx context.Context,
    opts ListMessagesOptions,
) (iter.Seq[gai.Message], error)
```

Returns messages ordered by creation timestamp (descending by default, ascending if `AscendingOrder` is true). Supports pagination via `Offset`. Each yielded message has full blocks loaded and `MessageIDKey`, `MessageCreatedAtKey`, and `MessageParentIDKey` populated in `ExtraFields`.

### Continuing Conversations

There is no dedicated "get most recent message" method. Instead, callers use `ListMessages` with default options (descending order) and iterate to find the first assistant or tool_result message:

```go
msgs, err := db.ListMessages(ctx, storage.ListMessagesOptions{})
if err != nil { ... }
for msg := range msgs {
    if msg.Role == gai.Assistant || msg.Role == gai.ToolResult {
        continueID = msg.ExtraFields[storage.MessageIDKey].(string)
        break
    }
}
```

### Deleting Messages

```go
func (s *DialogStorage) DeleteMessages(
    ctx context.Context,
    opts DeleteMessagesOptions,
) error
```

Deletes the specified messages atomically. If `Recursive` is false, attempting to delete a message that has children returns an error and no messages are deleted. If `Recursive` is true, each message's entire subtree is also deleted.

## CLI Integration

### Flags

The root command provides conversation control flags:

| Flag | Short | Description |
|------|-------|-------------|
| `--continue` | `-c` | Continue from a specific message ID |
| `--new` | `-n` | Start a new conversation (ignore previous) |
| `--incognito` | `-G` | Run without saving to storage |

### Default Behavior

Without flags, CPE:
1. Lists messages in descending order and finds the most recent assistant/tool_result message
2. Reconstructs the full dialog leading to that message via `GetDialogForMessage`
3. Appends the new user message
4. Generates a response
5. The saving middleware incrementally saves both user and assistant messages

### Conversation Subcommands

```bash
# List all conversations in tree format
cpe conversation list
cpe convo ls  # aliases

# Print a conversation thread
cpe conversation print <message_id>
cpe convo show <message_id>  # aliases: show, view

# Delete a message
cpe conversation delete <message_id>
cpe convo rm <message_id>  # alias

# Delete a message and all its descendants
cpe conversation delete --cascade <message_id>
```

### Tree Display

The `conversation list` command displays messages in a git-log style tree view:

```
abc123 (2024-01-15 10:30) [USER] Hello, can you help me?
    def456 (2024-01-15 10:31) [ASSISTANT] Of course! What do you need?
        ghi789 (2024-01-15 10:32) [USER] I need help with Go
            jkl012 (2024-01-15 10:33) [ASSISTANT] Sure, what's your question?
            ------
        mno345 (2024-01-15 10:35) [USER] Actually, Python instead
            pqr678 (2024-01-15 10:36) [ASSISTANT] No problem! What about Py...
            ------
```

Trees are sorted by the most recent timestamp in each branch, with the most recently active branches displayed last.

The tree is constructed from the flat message list returned by `ListMessages` using the `buildMessageForest` helper in `internal/commands/conversation_tree.go`. This helper reads `MessageIDKey`, `MessageParentIDKey`, and `MessageCreatedAtKey` from each message's `ExtraFields` to assemble parent-child relationships and populate `MessageIdNode` structs (a display-only type owned by the `commands` package).

## Architecture

### Key Files

| File | Description |
|------|-------------|
| `internal/storage/interfaces.go` | Interface definitions (`MessageDB`, subset interfaces), constants (`MessageIDKey`, etc.), `SaveMessageOptions`/`ListMessagesOptions`/`DeleteMessagesOptions` types, and `GetDialogForMessage` standalone function |
| `internal/storage/dialog_storage.go` | `DialogStorage` implementation of `MessageDB`, including `InitDialogStorage` constructor |
| `internal/storage/dialog_storage_test.go` | Comprehensive tests for the `MessageDB` interface |
| `internal/storage/schema.sql` | SQLite schema definition |
| `internal/storage/queries.sql` | SQL queries (sqlc input) |
| `internal/storage/models.go` | Generated sqlc models |
| `internal/storage/queries.sql.go` | Generated sqlc queries |
| `internal/storage/db.go` | Generated sqlc database interface |
| `internal/agent/saving_middleware.go` | Saving middleware using `storage.MessagesSaver` |
| `internal/commands/generate.go` | Generation logic using `storage.MessageDB` |
| `internal/commands/conversation.go` | Conversation management operations using subset interfaces |
| `internal/commands/conversation_tree.go` | `MessageIdNode` type, `buildMessageForest`, and tree display formatting |
| `cmd/conversation.go` | CLI command definitions |

### Import Graph

The `storage` package is a leaf dependency with no internal imports:

```
storage  → (no internal deps — only stdlib, gai, go-nanoid)
agent    → storage     (MessagesSaver, MessageIDKey)
agent    → types       (Renderer, Generator, ToolRegistrar)
commands → storage     (InitDialogStorage, *DialogStorage, MessageDB interfaces, MessageIDKey, MessageParentIDKey, MessageCreatedAtKey, GetDialogForMessage)
commands → types       (Generator, Renderer, ToolRegistrar)
cmd      → storage     (InitDialogStorage, *DialogStorage)
```

No circular imports exist. The `types` package contains only cross-cutting non-storage concerns (`Generator`, `Renderer`, `ToolRegistrar`).

### Code Generation

The project uses [sqlc](https://sqlc.dev/) for type-safe SQL query generation:

```yaml
# sqlc.yaml
version: "2"
sql:
  - engine: "sqlite"
    queries: "internal/storage/queries.sql"
    schema: "internal/storage/schema.sql"
    gen:
      go:
        package: "storage"
        out: "internal/storage"
        emit_json_tags: true
        emit_exact_table_names: false
        emit_empty_slices: true
```

Regenerate after schema/query changes:

```bash
go generate ./...
```

### Initialization

Storage is initialized when the CLI runs (unless in incognito mode):

```go
func InitDialogStorage(ctx context.Context, dbPath string) (*DialogStorage, error)
```

This function:
1. Opens or creates the SQLite database
2. Executes the schema SQL (creates tables if needed)
3. Returns a configured `*DialogStorage` instance

The concrete `*DialogStorage` is used for lifecycle management (`Close()`), while downstream consumers receive it through the appropriate subset interface.

### Saving Middleware

The `SavingMiddleware` in `internal/agent/saving_middleware.go` incrementally saves messages as they flow through the generation pipeline:

- **Before generation**: Walks the dialog and saves any messages that don't have an ID in `ExtraFields[MessageIDKey]`, deriving the parent chain from the dialog structure.
- **After generation**: Saves the assistant response.

The middleware depends on `storage.MessagesSaver` and calls `SaveMessages` with a single-element slice per save operation. It uses `GetMessageID` and `SetMessageID` helpers to read/write `storage.MessageIDKey` in message `ExtraFields`.

## Subagent Persistence

When CPE runs as an MCP server (subagent mode), execution traces are persisted with a special label format:

```
subagent:<name>:<run_id>
```

For example: `subagent:code-review:a1b2c3d4`

This allows:
- Distinguishing subagent traces from main conversation
- Correlating all messages from a single subagent invocation
- Querying traces by subagent name or run

The `saveSubagentTrace` function in `internal/commands/subagent.go` handles this:

```go
func saveSubagentTrace(
    ctx context.Context,
    saver storage.MessagesSaver,
    userMsg gai.Message,
    assistantMsgs gai.Dialog,
    label string,
) error
```

It saves the user message first, obtains its generated ID, then saves each assistant message sequentially, chaining parent IDs so the trace forms a linear conversation branch.

## Error Handling

### Interrupted Generation

When generation is interrupted (Ctrl+C), CPE:
1. The saving middleware has already saved the user message before generation started
2. If generation completes partially, whatever was saved remains in the database
3. The database remains consistent (just an incomplete conversation)

### Concurrent Access

SQLite handles concurrent access through file locking. Multiple CPE processes can safely access the same database, though write operations will block each other.

### Missing Storage

If storage initialization fails, CPE reports the error and exits. If `--incognito` mode is enabled, storage is not initialized and no persistence occurs.

### Delete Semantics

- **Non-recursive delete**: Returns an error if the message has children. No messages are deleted.
- **Recursive delete**: Deletes the message and its entire subtree. The operation is atomic.
- **Non-existent ID**: Silently succeeds (the SQL DELETE matches no rows).

## Testing

Tests use temporary directories with file-backed SQLite databases for isolation:

```go
func newTestDB(t *testing.T) *DialogStorage {
    t.Helper()
    dbPath := filepath.Join(t.TempDir(), "test.db")
    ds, err := InitDialogStorage(context.Background(), dbPath)
    if err != nil {
        t.Fatalf("InitDialogStorage: %v", err)
    }
    t.Cleanup(func() { ds.Close() })
    return ds
}
```

Tests operate exclusively through the `MessageDB` interface — they do not access unexported methods or database internals. Test coverage includes:

- **SaveMessages**: Single root, multiple messages, parent chaining, block persistence, title persistence, atomicity
- **GetMessages**: ID retrieval, parent ID in ExtraFields, role/ToolResultError round-trip, non-existent ID errors, block ExtraFields round-trip
- **ListMessages**: Descending order (default), ascending order, offset pagination, ExtraFields population
- **DeleteMessages**: Leaf deletion, non-recursive parent rejection, recursive parent+child deletion, recursive tree deletion
- **GetDialogForMessage**: Full chain reconstruction, parent ID verification, root-only dialog, non-existent ID errors
- **Round-trip**: Varied block types (text, tool call, image) with all fields verified

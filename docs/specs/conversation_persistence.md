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
    Role            Role     // User, Assistant, or ToolResult
    Blocks          []Block  // Content blocks
    ToolResultError bool     // Whether tool result indicates an error
}
```

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

## Core Operations

### DialogStorage Interface

The `DialogStorage` struct provides the primary API for conversation persistence:

```go
type DialogStorage struct {
    db          *sql.DB
    q           *Queries
    idGenerator func() string
}
```

### Saving Messages

```go
// SaveMessage saves a message with its blocks, linking to an optional parent
func (s *DialogStorage) SaveMessage(
    ctx context.Context,
    message gai.Message,
    parentID string,  // Empty string for root messages
    title string,     // Optional label
) (string, error)
```

The function:
1. Generates a unique 6-character message ID
2. Begins a transaction
3. Inserts the message record
4. Inserts all blocks with sequence ordering
5. Commits the transaction
6. Returns the generated message ID

### Loading Messages

```go
// GetMessage retrieves a single message and its parent ID
func (s *DialogStorage) GetMessage(
    ctx context.Context,
    messageID string,
) (gai.Message, string, error)

// GetDialogForMessage reconstructs the full dialog path from root to the specified message
func (s *DialogStorage) GetDialogForMessage(
    ctx context.Context,
    messageID string,
) (gai.Dialog, []string, error)
```

`GetDialogForMessage` traverses up the tree from the specified message to the root, then reverses the order to return messages from oldest to newest. It returns both the dialog and a list of message IDs.

### Continuing Conversations

```go
// GetMostRecentAssistantMessageId finds the most recent assistant or tool_result message
func (s *DialogStorage) GetMostRecentAssistantMessageId(
    ctx context.Context,
) (string, error)
```

This enables the default behavior of continuing from the last conversation turn.

### Listing Conversations

```go
// ListMessages returns all messages as a hierarchical forest of trees
func (s *DialogStorage) ListMessages(
    ctx context.Context,
) ([]MessageIdNode, error)
```

Returns a forest of message trees, where each root message and its descendants form a tree:

```go
type MessageIdNode struct {
    ID        string          // Message ID
    ParentID  string          // Parent message ID (empty for roots)
    CreatedAt time.Time       // Creation timestamp
    Content   string          // Truncated content preview (first 50 chars)
    Role      string          // user, assistant, or tool_result
    Children  []MessageIdNode // Child messages
}
```

### Deleting Messages

```go
// DeleteMessage deletes a leaf message (no children)
func (s *DialogStorage) DeleteMessage(ctx context.Context, messageID string) error

// DeleteMessageRecursive deletes a message and all its descendants
func (s *DialogStorage) DeleteMessageRecursive(ctx context.Context, messageID string) error
```

`DeleteMessage` fails if the message has children, enforcing the tree integrity. `DeleteMessageRecursive` removes an entire subtree.

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
1. Attempts to find the most recent assistant message
2. Loads the full dialog leading to that message
3. Appends the new user message
4. Generates a response
5. Saves both user and assistant messages

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

## Architecture

### Key Files

| File | Description |
|------|-------------|
| `internal/storage/schema.sql` | SQLite schema definition |
| `internal/storage/queries.sql` | SQL queries (sqlc input) |
| `internal/storage/dialog_storage.go` | Main storage implementation |
| `internal/storage/models.go` | Generated sqlc models |
| `internal/storage/queries.sql.go` | Generated sqlc queries |
| `internal/storage/db.go` | Generated sqlc database interface |
| `internal/commands/generate.go` | Generation logic with storage integration |
| `internal/commands/conversation.go` | Conversation management operations |
| `cmd/conversation.go` | CLI command definitions |
| `cmd/conversation_tree.go` | Tree display formatting |

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

### MarkdownRenderer Interface

The `commands` package defines an interface for markdown rendering, used by the conversation print command:

```go
// MarkdownRenderer renders markdown to formatted output
type MarkdownRenderer interface {
    Render(markdown string) (string, error)
}
```

The `MarkdownDialogFormatter` uses this interface to render conversation output with syntax highlighting.

### DialogStorage Interface

The `commands` package defines an interface for storage operations, enabling testing with mocks:

```go
// DialogStorage interface used by commands package
type DialogStorage interface {
    GetMostRecentAssistantMessageId(ctx context.Context) (string, error)
    GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error)
    SaveMessage(ctx context.Context, message gai.Message, parentID string, label string) (string, error)
    Close() error
}
```

### Initialization

Storage is initialized when the CLI runs (unless in incognito mode):

```go
// InitDialogStorage creates or opens the database
func InitDialogStorage(ctx context.Context, dbPath string) (*DialogStorage, error)
```

This function:
1. Opens or creates the SQLite database
2. Executes the schema SQL (creates tables if needed)
3. Returns a configured `DialogStorage` instance

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
    storage DialogStorage,
    userMsg gai.Message,
    assistantMsgs gai.Dialog,
    label string,
) error
```

## Error Handling

### Interrupted Generation

When generation is interrupted (Ctrl+C), CPE:
1. Warns the user about partial save
2. Creates a new context for the save operation
3. Allows the user to cancel the save with another interrupt
4. Saves whatever messages were generated before interruption

### Concurrent Access

SQLite handles concurrent access through file locking. Multiple CPE processes can safely access the same database, though write operations will block each other.

### Missing Storage

If storage initialization fails, CPE reports the error and exits. If `--incognito` mode is enabled, storage errors are ignored since no persistence is expected.

## Testing

Tests use in-memory SQLite databases for isolation and speed:

```go
func setupTestDB(t *testing.T) (*sql.DB, *DialogStorage) {
    db, err := sql.Open("sqlite3", ":memory:")
    require.NoError(t, err)

    // Enable foreign keys for referential integrity
    _, err = db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON;")
    require.NoError(t, err)

    // Create tables from embedded schema
    _, err = db.ExecContext(context.Background(), schemaSQL)
    require.NoError(t, err)

    return db, &DialogStorage{db: db, q: New(db), idGenerator: generateId}
}
```

Snapshot testing via [cupaloy](https://github.com/bradleyjkemp/cupaloy) validates message serialization stability.

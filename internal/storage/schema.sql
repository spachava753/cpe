-- schema.sql
CREATE TABLE IF NOT EXISTS messages
(
    id                TEXT PRIMARY KEY,
    parent_id         TEXT,
    title             TEXT, -- Optional title for the conversation branch starting with this message
    role              TEXT    NOT NULL,
    tool_result_error BOOLEAN NOT NULL DEFAULT 0,
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (parent_id) REFERENCES messages (id) ON DELETE RESTRICT
);

-- Create an index on created_at for efficient timestamp-based queries
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages (created_at);

-- Create an index on parent_id for efficient tree traversal
CREATE INDEX IF NOT EXISTS idx_messages_parent_id ON messages (parent_id);

CREATE TABLE IF NOT EXISTS blocks
(
    id        TEXT,
    message_id     TEXT      NOT NULL,
    block_type     TEXT      NOT NULL,
    modality_type  INTEGER   NOT NULL,
    mime_type TEXT NOT NULL,
    content        TEXT      NOT NULL,
    extra_fields TEXT, -- JSON-encoded ExtraFields map, can be NULL
    sequence_order INTEGER   NOT NULL,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (message_id, sequence_order),
    FOREIGN KEY (message_id) REFERENCES messages (id) ON DELETE CASCADE
);
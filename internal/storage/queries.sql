-- queries.sql

-- Message queries
-- name: CreateMessage :exec
INSERT INTO messages (
    id,
    parent_id,
    compaction_parent_id,
    role,
    tool_result_error,
    message_extra_fields,
    model_ref,
    model_id,
    model_type,
    model_display_name,
    input_tokens,
    output_tokens,
    cache_read_tokens,
    cache_write_tokens
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMessage :one
SELECT id,
       parent_id,
       compaction_parent_id,
       role,
       tool_result_error,
       message_extra_fields,
       model_ref,
       model_id,
       model_type,
       model_display_name,
       input_tokens,
       output_tokens,
       cache_read_tokens,
       cache_write_tokens,
       created_at
FROM messages
WHERE id = ?;

-- name: ListMessages :many
SELECT id
FROM messages
ORDER BY created_at DESC, rowid DESC;

-- name: ListMessagesByParent :many
SELECT id,
       parent_id,
       compaction_parent_id,
       role,
       tool_result_error,
       message_extra_fields,
       model_ref,
       model_id,
       model_type,
       model_display_name,
       input_tokens,
       output_tokens,
       cache_read_tokens,
       cache_write_tokens,
       created_at
FROM messages
WHERE parent_id = ?
ORDER BY created_at, rowid;

-- name: DeleteMessage :exec
DELETE
FROM messages
WHERE id = ?;

-- name: ListSessionExclusiveMessageIDs :many
WITH RECURSIVE session_messages(id, parent_id) AS (
    SELECT messages.id,
           messages.parent_id
    FROM acp_sessions
    JOIN messages ON messages.id = acp_sessions.last_message_id
    WHERE acp_sessions.id = sqlc.arg(session_id)
    UNION ALL
    SELECT messages.id,
           messages.parent_id
    FROM messages
    JOIN session_messages ON messages.id = session_messages.parent_id
),
other_session_messages(id, parent_id) AS (
    SELECT messages.id,
           messages.parent_id
    FROM acp_sessions
    JOIN messages ON messages.id = acp_sessions.last_message_id
    WHERE acp_sessions.id != sqlc.arg(session_id)
    UNION ALL
    SELECT messages.id,
           messages.parent_id
    FROM messages
    JOIN other_session_messages ON messages.id = other_session_messages.parent_id
)
SELECT id
FROM session_messages
WHERE id NOT IN (SELECT id FROM other_session_messages);

-- name: CheckMessageIDExists :one
SELECT EXISTS(SELECT 1 FROM messages WHERE id = ?) as "exists";

-- name: HasChildren :one
SELECT EXISTS(SELECT 1 FROM messages WHERE parent_id = ?) as has_children;

-- name: ListMessagesDescending :many
SELECT id,
       parent_id,
       compaction_parent_id,
       role,
       tool_result_error,
       message_extra_fields,
       model_ref,
       model_id,
       model_type,
       model_display_name,
       input_tokens,
       output_tokens,
       cache_read_tokens,
       cache_write_tokens,
       created_at
FROM messages
ORDER BY created_at DESC, rowid DESC
LIMIT -1 OFFSET ?;

-- name: ListMessagesAscending :many
SELECT id,
       parent_id,
       compaction_parent_id,
       role,
       tool_result_error,
       message_extra_fields,
       model_ref,
       model_id,
       model_type,
       model_display_name,
       input_tokens,
       output_tokens,
       cache_read_tokens,
       cache_write_tokens,
       created_at
FROM messages
ORDER BY created_at ASC, rowid ASC
LIMIT -1 OFFSET ?;

-- Block queries
-- name: CreateBlock :exec
INSERT INTO blocks (id, message_id, block_type, modality_type, mime_type, content, extra_fields, sequence_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetBlock :one
SELECT *
FROM blocks
WHERE id = ?;

-- name: GetBlocksByMessage :many
SELECT *
FROM blocks
WHERE message_id = ?
ORDER BY sequence_order;

-- ACP queries
-- name: CreateSession :exec
INSERT INTO acp_sessions (id, last_message_id, cwd, title, model_ref, thinking_level)
VALUES (?, ?, ?, ?, ?, ?);

-- name: AddSessionMessage :execrows
UPDATE acp_sessions
SET last_message_id = sqlc.narg(message_id)
WHERE id = sqlc.arg(session_id)
  AND last_message_id IS sqlc.narg(expected_last_message_id);

-- name: DeleteSession :execrows
DELETE
FROM acp_sessions
WHERE id = ?;

-- name: SetSessionModelRef :execrows
UPDATE acp_sessions
SET model_ref = ?
WHERE id = ?;

-- name: SetSessionThinkingLevel :execrows
UPDATE acp_sessions
SET thinking_level = ?
WHERE id = ?;

-- name: AddSessionCost :one
UPDATE acp_sessions
SET cost_usd = cost_usd + ?
WHERE id = ?
RETURNING cost_usd;

-- name: GetSession :one
SELECT acp_sessions.id,
       acp_sessions.last_message_id,
       acp_sessions.cwd,
       acp_sessions.title,
       acp_sessions.model_ref,
       acp_sessions.thinking_level,
       acp_sessions.cost_usd,
       acp_sessions.created_at
FROM acp_sessions
WHERE acp_sessions.id = ?;

-- name: ListSessions :many
SELECT acp_sessions.id,
       acp_sessions.cwd,
       acp_sessions.title,
       acp_sessions.model_ref,
       acp_sessions.thinking_level,
       acp_sessions.created_at
FROM acp_sessions
LEFT JOIN messages ON messages.id = acp_sessions.last_message_id
WHERE sqlc.narg(cwd) IS NULL OR acp_sessions.cwd = sqlc.narg(cwd)
ORDER BY COALESCE(messages.created_at, acp_sessions.created_at) DESC, acp_sessions.rowid DESC;

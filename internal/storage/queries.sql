-- queries.sql

-- Message queries
-- name: CreateMessage :exec
INSERT INTO messages (id, parent_id, is_subagent, role, tool_result_error)
VALUES (?, ?, ?, ?, ?);

-- name: GetMessage :one
SELECT id, parent_id, is_subagent, role, tool_result_error, created_at
FROM messages
WHERE id = ?;

-- name: ListMessages :many
SELECT id
FROM messages
ORDER BY created_at DESC, rowid DESC;

-- name: ListMessagesByParent :many
SELECT id, parent_id, is_subagent, role, tool_result_error, created_at
FROM messages
WHERE parent_id = ?
ORDER BY created_at, rowid;

-- name: DeleteMessage :exec
DELETE
FROM messages
WHERE id = ?;

-- name: CheckMessageIDExists :one
SELECT EXISTS(SELECT 1 FROM messages WHERE id = ?) as "exists";

-- name: HasChildren :one
SELECT EXISTS(SELECT 1 FROM messages WHERE parent_id = ?) as has_children;

-- name: ListMessagesDescending :many
SELECT id, parent_id, is_subagent, role, tool_result_error, created_at
FROM messages
ORDER BY created_at DESC, rowid DESC
LIMIT -1 OFFSET ?;

-- name: ListMessagesAscending :many
SELECT id, parent_id, is_subagent, role, tool_result_error, created_at
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
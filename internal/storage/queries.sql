-- queries.sql

-- Message queries
-- name: CreateMessage :exec
INSERT INTO messages (id, parent_id, title, role, tool_result_error)
VALUES (?, ?, ?, ?, ?);

-- name: GetMessage :one
SELECT *
FROM messages
WHERE id = ?;

-- name: GetMostRecentUserMessage :one
SELECT *
FROM messages
WHERE role = 'user'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetChildrenCount :one
SELECT COUNT(*) as count
FROM messages
WHERE parent_id = ?;

-- name: ListMessages :many
SELECT id
FROM messages
ORDER BY created_at DESC;

-- name: ListMessagesByParent :many
SELECT *
FROM messages
WHERE parent_id = ?
ORDER BY created_at;

-- name: DeleteMessage :exec
DELETE
FROM messages
WHERE id = ?;

-- name: HasChildren :one
SELECT EXISTS(SELECT 1 FROM messages WHERE parent_id = ?) as has_children;

-- name: GetMessageChildrenId :many
SELECT id
FROM messages
WHERE parent_id = ?
ORDER BY created_at DESC;

-- Block queries
-- name: CreateBlock :exec
INSERT INTO blocks (id, message_id, block_type, modality_type, mime_type, content, sequence_order)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetBlock :one
SELECT *
FROM blocks
WHERE id = ?;

-- name: GetBlocksByMessage :many
SELECT *
FROM blocks
WHERE message_id = ?
ORDER BY sequence_order;

-- name: DeleteBlock :exec
DELETE
FROM blocks
WHERE id = ?;
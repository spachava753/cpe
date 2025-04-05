-- queries.sql

-- Message queries
-- name: CreateMessage :exec
INSERT INTO messages (id, parent_id, title, role, tool_result_error)
VALUES (?, ?, ?, ?, ?);

-- name: GetMessage :one
SELECT *
FROM messages
WHERE id = ?;

-- name: GetMostRecentMessage :one
SELECT *
FROM messages
ORDER BY created_at DESC
LIMIT 1;

-- name: GetChildrenCount :one
SELECT COUNT(*) as count
FROM messages
WHERE parent_id = ?;

-- name: ListMessages :many
SELECT *
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

-- Dialog Reconstruction
-- name: GetDialogPath :many
WITH RECURSIVE message_path AS (
    -- Start with the specified leaf message
    SELECT id, parent_id, role, tool_result_error, title, created_at, 0 as depth
    FROM messages
    WHERE messages.id = ?
    
    UNION ALL

    -- Add parent messages recursively
    SELECT m.id, m.parent_id, m.role, m.tool_result_error, m.title, m.created_at, mp.depth + 1
    FROM messages m
             JOIN message_path mp ON m.id = mp.parent_id)
SELECT mp.id,
       mp.parent_id,
       mp.role,
       mp.tool_result_error,
       mp.title,
       mp.created_at,
       mp.depth,
       b.id as block_id,
       b.block_type,
       b.modality_type,
       b.mime_type,
       b.content,
       b.sequence_order
FROM message_path mp
         LEFT JOIN blocks b ON mp.id = b.message_id
ORDER BY mp.depth DESC;

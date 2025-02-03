-- queries.sql

-- name: GetConversation :one
SELECT * FROM conversations
WHERE id = ? LIMIT 1;

-- name: GetLatestConversation :one
SELECT * FROM conversations
ORDER BY id DESC LIMIT 1;

-- name: ListConversations :many
SELECT id, parent_id, user_message, model, created_at
FROM conversations
ORDER BY created_at DESC;

-- name: CreateConversation :exec
INSERT INTO conversations (
    id, parent_id, user_message, executor_data, created_at, model
) VALUES (?, ?, ?, ?, ?, ?);

-- name: DeleteConversation :exec
DELETE FROM conversations
WHERE id = ?;

-- name: GetChildConversations :many
WITH RECURSIVE children AS (
    SELECT conversations.id FROM conversations WHERE conversations.id = ?
    UNION ALL
    SELECT c.id FROM conversations c
    INNER JOIN children ch ON c.parent_id = ch.id
)
SELECT children.id FROM children;
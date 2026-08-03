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
-- DeleteACPSession uses this query to find message rows that can be deleted
-- with the target session. This is a graph-reachability query rather than a
-- simple lookup because forks share history, compaction links otherwise
-- separate message chains, and a failed optimistic update can leave an
-- unreferenced sibling branch in the database. Deleting a parent still needed
-- by any of those branches would either lose history or fail the parent_id
-- ON DELETE RESTRICT constraint.
--
-- Each recursive common table expression (CTE) starts with known rows and then
-- repeatedly follows parent links toward older history:
--   1. session_messages collects the target session's normal and pre-compaction
--      history. depth is zero at the session head and increases toward roots.
--   2. other_session_messages collects history reachable from every other
--      session head. Those rows are shared and must not be deleted.
--   3. orphan_ancestor_messages finds target-history rows referenced by a child
--      outside session_messages, such as a losing conflict branch, then walks
--      farther back to preserve every ancestor that branch needs.
-- The final subtraction leaves only target-session history that nothing else
-- needs. Ordering by depth deletes children before parents, as required by the
-- restrictive parent_id foreign key.
WITH RECURSIVE session_messages(id, parent_id, compaction_parent_id, depth) AS (
    -- Seed the target history with its current head message.
    SELECT messages.id,
           messages.parent_id,
           messages.compaction_parent_id,
           0
    FROM acp_sessions
    JOIN messages ON messages.id = acp_sessions.last_message_id
    WHERE acp_sessions.id = sqlc.arg(session_id)
    UNION ALL
    -- Add each normal parent or compaction parent until all roots are reached.
    SELECT messages.id,
           messages.parent_id,
           messages.compaction_parent_id,
           session_messages.depth + 1
    FROM messages
    JOIN session_messages ON messages.id = session_messages.parent_id
                          OR messages.id = session_messages.compaction_parent_id
),
other_session_messages(id, parent_id, compaction_parent_id) AS (
    -- Seed protected shared history with every other session head.
    SELECT messages.id,
           messages.parent_id,
           messages.compaction_parent_id
    FROM acp_sessions
    JOIN messages ON messages.id = acp_sessions.last_message_id
    WHERE acp_sessions.id != sqlc.arg(session_id)
    UNION ALL
    -- Protect all normal and pre-compaction ancestors of those heads.
    SELECT messages.id,
           messages.parent_id,
           messages.compaction_parent_id
    FROM messages
    JOIN other_session_messages ON messages.id = other_session_messages.parent_id
                                OR messages.id = other_session_messages.compaction_parent_id
),
orphan_ancestor_messages(id, parent_id, compaction_parent_id) AS (
    -- Find target-history rows referenced by branches outside the target path.
    SELECT parent.id,
           parent.parent_id,
           parent.compaction_parent_id
    FROM messages AS child
    JOIN session_messages AS parent ON child.parent_id = parent.id
                                    OR child.compaction_parent_id = parent.id
    WHERE child.id NOT IN (SELECT id FROM session_messages)
    UNION
    -- Protect the complete ancestry of every such attachment point.
    SELECT ancestor.id,
           ancestor.parent_id,
           ancestor.compaction_parent_id
    FROM messages AS ancestor
    JOIN orphan_ancestor_messages AS orphan ON ancestor.id = orphan.parent_id
                                             OR ancestor.id = orphan.compaction_parent_id
)
SELECT id
FROM session_messages
WHERE id NOT IN (SELECT id FROM other_session_messages)
  AND id NOT IN (SELECT id FROM orphan_ancestor_messages)
ORDER BY depth;

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
ORDER BY MAX(
             julianday(acp_sessions.created_at),
             COALESCE(julianday(messages.created_at), julianday(acp_sessions.created_at))
         ) DESC,
         acp_sessions.rowid DESC;

-- name: ListSessionSummaries :many
SELECT acp_sessions.id,
       acp_sessions.title,
       acp_sessions.created_at,
       messages.created_at AS last_message_created_at
FROM acp_sessions
LEFT JOIN messages ON messages.id = acp_sessions.last_message_id
-- A fork can point to history older than the fork itself, so last modification
-- is the later of session creation and head-message creation. Empty sessions
-- have no head message and therefore fall back to session creation. julianday
-- normalizes timestamps created by SQLite and timestamps bound from Go before
-- comparing them.
ORDER BY MAX(
             julianday(acp_sessions.created_at),
             COALESCE(julianday(messages.created_at), julianday(acp_sessions.created_at))
         ) DESC,
         acp_sessions.rowid DESC
LIMIT sqlc.arg(page_size) OFFSET sqlc.arg(page_offset);

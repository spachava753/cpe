// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.28.0
// source: queries.sql

package storage

import (
	"context"
	"database/sql"
)

const createBlock = `-- name: CreateBlock :exec
INSERT INTO blocks (id, message_id, block_type, modality_type, mime_type, content, extra_fields, sequence_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`

type CreateBlockParams struct {
	ID            sql.NullString `json:"id"`
	MessageID     string         `json:"message_id"`
	BlockType     string         `json:"block_type"`
	ModalityType  int64          `json:"modality_type"`
	MimeType      string         `json:"mime_type"`
	Content       string         `json:"content"`
	ExtraFields   sql.NullString `json:"extra_fields"`
	SequenceOrder int64          `json:"sequence_order"`
}

// Block queries
func (q *Queries) CreateBlock(ctx context.Context, arg CreateBlockParams) error {
	_, err := q.db.ExecContext(ctx, createBlock,
		arg.ID,
		arg.MessageID,
		arg.BlockType,
		arg.ModalityType,
		arg.MimeType,
		arg.Content,
		arg.ExtraFields,
		arg.SequenceOrder,
	)
	return err
}

const createMessage = `-- name: CreateMessage :exec

INSERT INTO messages (id, parent_id, title, role, tool_result_error)
VALUES (?, ?, ?, ?, ?)
`

type CreateMessageParams struct {
	ID              string         `json:"id"`
	ParentID        sql.NullString `json:"parent_id"`
	Title           sql.NullString `json:"title"`
	Role            string         `json:"role"`
	ToolResultError bool           `json:"tool_result_error"`
}

// queries.sql
// Message queries
func (q *Queries) CreateMessage(ctx context.Context, arg CreateMessageParams) error {
	_, err := q.db.ExecContext(ctx, createMessage,
		arg.ID,
		arg.ParentID,
		arg.Title,
		arg.Role,
		arg.ToolResultError,
	)
	return err
}

const deleteBlock = `-- name: DeleteBlock :exec
DELETE
FROM blocks
WHERE id = ?
`

func (q *Queries) DeleteBlock(ctx context.Context, id sql.NullString) error {
	_, err := q.db.ExecContext(ctx, deleteBlock, id)
	return err
}

const deleteMessage = `-- name: DeleteMessage :exec
DELETE
FROM messages
WHERE id = ?
`

func (q *Queries) DeleteMessage(ctx context.Context, id string) error {
	_, err := q.db.ExecContext(ctx, deleteMessage, id)
	return err
}

const getBlock = `-- name: GetBlock :one
SELECT id, message_id, block_type, modality_type, mime_type, content, extra_fields, sequence_order, created_at
FROM blocks
WHERE id = ?
`

func (q *Queries) GetBlock(ctx context.Context, id sql.NullString) (Block, error) {
	row := q.db.QueryRowContext(ctx, getBlock, id)
	var i Block
	err := row.Scan(
		&i.ID,
		&i.MessageID,
		&i.BlockType,
		&i.ModalityType,
		&i.MimeType,
		&i.Content,
		&i.ExtraFields,
		&i.SequenceOrder,
		&i.CreatedAt,
	)
	return i, err
}

const getBlocksByMessage = `-- name: GetBlocksByMessage :many
SELECT id, message_id, block_type, modality_type, mime_type, content, extra_fields, sequence_order, created_at
FROM blocks
WHERE message_id = ?
ORDER BY sequence_order
`

func (q *Queries) GetBlocksByMessage(ctx context.Context, messageID string) ([]Block, error) {
	rows, err := q.db.QueryContext(ctx, getBlocksByMessage, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Block{}
	for rows.Next() {
		var i Block
		if err := rows.Scan(
			&i.ID,
			&i.MessageID,
			&i.BlockType,
			&i.ModalityType,
			&i.MimeType,
			&i.Content,
			&i.ExtraFields,
			&i.SequenceOrder,
			&i.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getChildrenCount = `-- name: GetChildrenCount :one
SELECT COUNT(*) as count
FROM messages
WHERE parent_id = ?
`

func (q *Queries) GetChildrenCount(ctx context.Context, parentID sql.NullString) (int64, error) {
	row := q.db.QueryRowContext(ctx, getChildrenCount, parentID)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const getMessage = `-- name: GetMessage :one
SELECT id, parent_id, title, role, tool_result_error, created_at
FROM messages
WHERE id = ?
`

func (q *Queries) GetMessage(ctx context.Context, id string) (Message, error) {
	row := q.db.QueryRowContext(ctx, getMessage, id)
	var i Message
	err := row.Scan(
		&i.ID,
		&i.ParentID,
		&i.Title,
		&i.Role,
		&i.ToolResultError,
		&i.CreatedAt,
	)
	return i, err
}

const getMessageChildrenId = `-- name: GetMessageChildrenId :many
SELECT id
FROM messages
WHERE parent_id = ?
ORDER BY created_at DESC
`

func (q *Queries) GetMessageChildrenId(ctx context.Context, parentID sql.NullString) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, getMessageChildrenId, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		items = append(items, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getMostRecentUserMessage = `-- name: GetMostRecentUserMessage :one
SELECT id, parent_id, title, role, tool_result_error, created_at
FROM messages
WHERE role = 'user'
ORDER BY created_at DESC
LIMIT 1
`

func (q *Queries) GetMostRecentUserMessage(ctx context.Context) (Message, error) {
	row := q.db.QueryRowContext(ctx, getMostRecentUserMessage)
	var i Message
	err := row.Scan(
		&i.ID,
		&i.ParentID,
		&i.Title,
		&i.Role,
		&i.ToolResultError,
		&i.CreatedAt,
	)
	return i, err
}

const hasChildren = `-- name: HasChildren :one
SELECT EXISTS(SELECT 1 FROM messages WHERE parent_id = ?) as has_children
`

func (q *Queries) HasChildren(ctx context.Context, parentID sql.NullString) (int64, error) {
	row := q.db.QueryRowContext(ctx, hasChildren, parentID)
	var has_children int64
	err := row.Scan(&has_children)
	return has_children, err
}

const listMessages = `-- name: ListMessages :many
SELECT id
FROM messages
ORDER BY created_at DESC
`

func (q *Queries) ListMessages(ctx context.Context) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, listMessages)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		items = append(items, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listMessagesByParent = `-- name: ListMessagesByParent :many
SELECT id, parent_id, title, role, tool_result_error, created_at
FROM messages
WHERE parent_id = ?
ORDER BY created_at
`

func (q *Queries) ListMessagesByParent(ctx context.Context, parentID sql.NullString) ([]Message, error) {
	rows, err := q.db.QueryContext(ctx, listMessagesByParent, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Message{}
	for rows.Next() {
		var i Message
		if err := rows.Scan(
			&i.ID,
			&i.ParentID,
			&i.Title,
			&i.Role,
			&i.ToolResultError,
			&i.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listRootMessages = `-- name: ListRootMessages :many
SELECT id
FROM messages
WHERE parent_id IS NULL
ORDER BY created_at
`

func (q *Queries) ListRootMessages(ctx context.Context) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, listRootMessages)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		items = append(items, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

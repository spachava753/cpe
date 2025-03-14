// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.28.0
// source: queries.sql

package db

import (
	"context"
	"database/sql"
	"time"
)

const createConversation = `-- name: CreateConversation :exec
INSERT INTO conversations (
    id, parent_id, user_message, executor_data, created_at, model
) VALUES (?, ?, ?, ?, ?, ?)
`

type CreateConversationParams struct {
	ID           string         `json:"id"`
	ParentID     sql.NullString `json:"parent_id"`
	UserMessage  string         `json:"user_message"`
	ExecutorData []byte         `json:"executor_data"`
	CreatedAt    time.Time      `json:"created_at"`
	Model        string         `json:"model"`
}

func (q *Queries) CreateConversation(ctx context.Context, arg CreateConversationParams) error {
	_, err := q.db.ExecContext(ctx, createConversation,
		arg.ID,
		arg.ParentID,
		arg.UserMessage,
		arg.ExecutorData,
		arg.CreatedAt,
		arg.Model,
	)
	return err
}

const deleteConversation = `-- name: DeleteConversation :exec
DELETE FROM conversations
WHERE id = ?
`

func (q *Queries) DeleteConversation(ctx context.Context, id string) error {
	_, err := q.db.ExecContext(ctx, deleteConversation, id)
	return err
}

const getChildConversations = `-- name: GetChildConversations :many
WITH RECURSIVE children AS (
    SELECT conversations.id FROM conversations WHERE conversations.id = ?
    UNION ALL
    SELECT c.id FROM conversations c
    INNER JOIN children ch ON c.parent_id = ch.id
)
SELECT children.id FROM children
`

func (q *Queries) GetChildConversations(ctx context.Context, id string) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, getChildConversations, id)
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

const getConversation = `-- name: GetConversation :one

SELECT id, parent_id, user_message, executor_data, created_at, model FROM conversations
WHERE id = ? LIMIT 1
`

// queries.sql
func (q *Queries) GetConversation(ctx context.Context, id string) (Conversation, error) {
	row := q.db.QueryRowContext(ctx, getConversation, id)
	var i Conversation
	err := row.Scan(
		&i.ID,
		&i.ParentID,
		&i.UserMessage,
		&i.ExecutorData,
		&i.CreatedAt,
		&i.Model,
	)
	return i, err
}

const getLatestConversation = `-- name: GetLatestConversation :one
SELECT id, parent_id, user_message, executor_data, created_at, model FROM conversations
ORDER BY created_at DESC LIMIT 1
`

func (q *Queries) GetLatestConversation(ctx context.Context) (Conversation, error) {
	row := q.db.QueryRowContext(ctx, getLatestConversation)
	var i Conversation
	err := row.Scan(
		&i.ID,
		&i.ParentID,
		&i.UserMessage,
		&i.ExecutorData,
		&i.CreatedAt,
		&i.Model,
	)
	return i, err
}

const listConversations = `-- name: ListConversations :many
SELECT id, parent_id, user_message, model, created_at
FROM conversations
ORDER BY created_at DESC
`

type ListConversationsRow struct {
	ID          string         `json:"id"`
	ParentID    sql.NullString `json:"parent_id"`
	UserMessage string         `json:"user_message"`
	Model       string         `json:"model"`
	CreatedAt   time.Time      `json:"created_at"`
}

func (q *Queries) ListConversations(ctx context.Context) ([]ListConversationsRow, error) {
	rows, err := q.db.QueryContext(ctx, listConversations)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ListConversationsRow{}
	for rows.Next() {
		var i ListConversationsRow
		if err := rows.Scan(
			&i.ID,
			&i.ParentID,
			&i.UserMessage,
			&i.Model,
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

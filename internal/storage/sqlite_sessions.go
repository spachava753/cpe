package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	acp "github.com/spachava753/acp-sdk/acp"

	"github.com/spachava753/cpe/internal/storage/sqlcgen"
)

func acpSessionInfo(id, cwd, title string, updatedAt time.Time) acp.SessionInfo {
	updatedAtText := updatedAt.UTC().Format(time.RFC3339Nano)
	return acp.SessionInfo{
		Cwd:       cwd,
		SessionID: acp.SessionId(id),
		Title:     &title,
		UpdatedAt: &updatedAtText,
	}
}

func acpSessionTitle(session acp.SessionInfo) string {
	if session.Title == nil {
		return string(session.SessionID)
	}
	return *session.Title
}

// CreateACPSession persists ACP session metadata.
func (s *Sqlite) CreateACPSession(ctx context.Context, params CreateACPSessionParams) error {
	err := s.q.CreateSession(ctx, sqlcgen.CreateSessionParams{
		ID:            string(params.Session.SessionID),
		LastMessageID: optionalString(params.LastMessageID),
		Cwd:           params.Session.Cwd,
		Title:         acpSessionTitle(params.Session),
		ModelRef:      params.ModelRef,
		ThinkingLevel: params.ThinkingLevel,
	})
	if err != nil {
		return fmt.Errorf("failed to create ACP session %s: %w", params.Session.SessionID, err)
	}
	return nil
}

// AddACPSessionMessage advances a persisted ACP session from
// expectedMessageID to messageID and returns the updated session.
func (s *Sqlite) AddACPSessionMessage(ctx context.Context, sessionID acp.SessionId, expectedMessageID, messageID string) (acp.SessionInfo, error) {
	rowsAffected, err := s.q.AddSessionMessage(ctx, sqlcgen.AddSessionMessageParams{
		MessageID:             optionalString(messageID),
		SessionID:             string(sessionID),
		ExpectedLastMessageID: optionalString(expectedMessageID),
	})
	if err != nil {
		return acp.SessionInfo{}, fmt.Errorf("failed to add message %s to ACP session %s: %w", messageID, sessionID, err)
	}
	if rowsAffected == 0 {
		current, err := s.GetACPSession(ctx, sessionID)
		if err != nil {
			return acp.SessionInfo{}, err
		}
		return acp.SessionInfo{}, fmt.Errorf(
			"ACP session %s advanced to message %q while expecting %q: %w",
			sessionID,
			current.LastMessageID,
			expectedMessageID,
			ErrSessionConflict,
		)
	}
	resp, err := s.GetACPSession(ctx, sessionID)
	if err != nil {
		return acp.SessionInfo{}, err
	}
	return resp.Session, nil
}

// DeleteACPSession removes an ACP session from the persisted session list and
// deletes the messages in its chain that are not reachable from any other ACP
// session. History shared with other sessions (for example, a fork created via
// session/fork) is preserved.
func (s *Sqlite) DeleteACPSession(ctx context.Context, sessionID acp.SessionId) error {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to begin ACP session delete transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	qtx := s.q.WithTx(tx)
	messageIDs, err := qtx.ListSessionExclusiveMessageIDs(ctx, string(sessionID))
	if err != nil {
		return fmt.Errorf("failed to list messages for ACP session %s: %w", sessionID, err)
	}
	rowsAffected, err := qtx.DeleteSession(ctx, string(sessionID))
	if err != nil {
		return fmt.Errorf("failed to delete ACP session %s: %w", sessionID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("ACP session %s not found: %w", sessionID, ErrSessionNotFound)
	}
	for _, messageID := range messageIDs {
		if err := qtx.DeleteMessage(ctx, messageID); err != nil {
			return fmt.Errorf("failed to delete message %s for ACP session %s: %w", messageID, sessionID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit ACP session %s delete: %w", sessionID, err)
	}
	committed = true
	return nil
}

// SetACPSessionModelRef marks modelRef as the selected model profile for an
// ACP session.
func (s *Sqlite) SetACPSessionModelRef(ctx context.Context, sessionID acp.SessionId, modelRef string) error {
	rowsAffected, err := s.q.SetSessionModelRef(ctx, sqlcgen.SetSessionModelRefParams{
		ModelRef: modelRef,
		ID:       string(sessionID),
	})
	if err != nil {
		return fmt.Errorf("failed to set model ref for ACP session %s: %w", sessionID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("ACP session %s not found: %w", sessionID, ErrSessionNotFound)
	}
	return nil
}

// SetACPSessionThinkingLevel marks thinkingLevel as the reasoning effort level
// for an ACP session.
func (s *Sqlite) SetACPSessionThinkingLevel(ctx context.Context, sessionID acp.SessionId, thinkingLevel string) error {
	rowsAffected, err := s.q.SetSessionThinkingLevel(ctx, sqlcgen.SetSessionThinkingLevelParams{
		ThinkingLevel: thinkingLevel,
		ID:            string(sessionID),
	})
	if err != nil {
		return fmt.Errorf("failed to set thinking level for ACP session %s: %w", sessionID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("ACP session %s not found: %w", sessionID, ErrSessionNotFound)
	}
	return nil
}

// AddACPSessionCost atomically adds costUSD (in US dollars) to an ACP
// session's persisted cumulative cost and returns the updated total.
func (s *Sqlite) AddACPSessionCost(ctx context.Context, sessionID acp.SessionId, costUSD float64) (float64, error) {
	total, err := s.q.AddSessionCost(ctx, sqlcgen.AddSessionCostParams{
		CostUsd: costUSD,
		ID:      string(sessionID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("ACP session %s not found: %w", sessionID, ErrSessionNotFound)
		}
		return 0, fmt.Errorf("failed to add cost to ACP session %s: %w", sessionID, err)
	}
	return total, nil
}

// GetACPSession returns ACP session metadata and its latest persisted message
// ID.
//
// UpdatedAt is formatted as an ISO 8601 timestamp from the session's creation
// time.
func (s *Sqlite) GetACPSession(ctx context.Context, sessionID acp.SessionId) (GetACPSessionResponse, error) {
	row, err := s.q.GetSession(ctx, string(sessionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GetACPSessionResponse{}, fmt.Errorf("ACP session %s not found: %w", sessionID, ErrSessionNotFound)
		}
		return GetACPSessionResponse{}, fmt.Errorf("failed to get ACP session %s: %w", sessionID, err)
	}
	return GetACPSessionResponse{
		Session:       acpSessionInfo(row.ID, row.Cwd, row.Title, row.CreatedAt),
		LastMessageID: row.LastMessageID.String,
		ModelRef:      row.ModelRef,
		ThinkingLevel: row.ThinkingLevel,
		CostUSD:       row.CostUsd,
	}, nil
}

func optionalString(value string) sql.NullString {
	return sql.NullString{
		String: value,
		Valid:  value != "",
	}
}

// ListACPSessions returns ACP session metadata ordered by last activity, newest
// first. When cwd is non-nil, only sessions with an exactly matching working
// directory are returned.
//
// UpdatedAt is formatted as an ISO 8601 timestamp from each session's creation
// time.
func (s *Sqlite) ListACPSessions(ctx context.Context, cwd *string) ([]acp.SessionInfo, error) {
	cwdFilter := sql.NullString{}
	if cwd != nil {
		cwdFilter = sql.NullString{String: *cwd, Valid: true}
	}
	rows, err := s.q.ListSessions(ctx, cwdFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to list ACP sessions: %w", err)
	}
	sessions := make([]acp.SessionInfo, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, acpSessionInfo(row.ID, row.Cwd, row.Title, row.CreatedAt))
	}
	return sessions, nil
}

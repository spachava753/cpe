package acp

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	acpsdk "github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

func TestListStoredSessionsPaginationAndFormatting(t *testing.T) {
	store, rawDB := newTestSqlite(t)
	olderMessageID := saveStoredDialogForCommandTest(t, store, gai.Dialog{{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock("older")},
	}})
	newerMessageID := saveStoredDialogForCommandTest(t, store, gai.Dialog{{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock("newer")},
	}})
	olderCreatedAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	newerCreatedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	olderModifiedAt := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	newerModifiedAt := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)

	for _, update := range []struct {
		messageID string
		createdAt time.Time
	}{
		{olderMessageID, olderModifiedAt},
		{newerMessageID, newerModifiedAt},
	} {
		if _, err := rawDB.ExecContext(t.Context(), "UPDATE messages SET created_at = ? WHERE id = ?", update.createdAt, update.messageID); err != nil {
			t.Fatalf("update message timestamp: %v", err)
		}
	}
	for _, session := range []struct {
		id            acpsdk.SessionId
		title         string
		lastMessageID string
		createdAt     time.Time
	}{
		{"older-session", "Older title", olderMessageID, olderCreatedAt},
		{"newer-session", "Newer title", newerMessageID, newerCreatedAt},
	} {
		if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acpsdk.SessionInfo{
				Cwd:       "/tmp/project",
				SessionID: session.id,
				Title:     new(session.title),
			},
			LastMessageID: session.lastMessageID,
		}); err != nil {
			t.Fatalf("CreateACPSession(%s): %v", session.id, err)
		}
		if _, err := rawDB.ExecContext(t.Context(), "UPDATE acp_sessions SET created_at = ? WHERE id = ?", session.createdAt, session.id); err != nil {
			t.Fatalf("update session timestamp: %v", err)
		}
	}

	var output bytes.Buffer
	if err := ListStoredSessions(t.Context(), ListStoredSessionsOptions{
		Store:    store,
		Writer:   &output,
		Page:     2,
		PageSize: 1,
	}); err != nil {
		t.Fatalf("ListStoredSessions: %v", err)
	}
	got := output.String()
	for _, want := range []string{
		"ID",
		"CREATED AT",
		"LAST MODIFIED",
		"TITLE",
		"older-session",
		olderCreatedAt.Format(time.RFC822),
		olderModifiedAt.Format(time.RFC822),
		"Older title",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "newer-session") {
		t.Fatalf("second page unexpectedly contains newer-session:\n%s", got)
	}

	if err := ListStoredSessions(t.Context(), ListStoredSessionsOptions{
		Store: store, Writer: &bytes.Buffer{}, PageSize: 20,
	}); err == nil || err.Error() != "page must be at least 1" {
		t.Fatalf("page zero error = %v", err)
	}
}

func TestListStoredSessionsRejectsUnsafePagination(t *testing.T) {
	store, _ := newTestSqlite(t)

	if err := ListStoredSessions(t.Context(), ListStoredSessionsOptions{
		Store: store, Writer: &bytes.Buffer{}, Page: 1, PageSize: 1001,
	}); err == nil || err.Error() != "page size must not exceed 1000" {
		t.Fatalf("oversized page error = %v", err)
	}
	if err := ListStoredSessions(t.Context(), ListStoredSessionsOptions{
		Store: store, Writer: &bytes.Buffer{}, Page: ^uint64(0), PageSize: 2,
	}); err == nil || err.Error() != "page and page size produce an offset that is too large" {
		t.Fatalf("overflowing offset error = %v", err)
	}
}

func TestForkStoredSessionOfOldHistoryIsListedAsNew(t *testing.T) {
	store, rawDB := newTestSqlite(t)
	oldMessageID := saveStoredDialogForCommandTest(t, store, gai.Dialog{{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock("old history")},
	}})
	recentMessageID := saveStoredDialogForCommandTest(t, store, gai.Dialog{{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock("recent history")},
	}})
	for _, session := range []struct {
		id            acpsdk.SessionId
		lastMessageID string
	}{
		{"source-session", oldMessageID},
		{"recent-session", recentMessageID},
	} {
		if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acpsdk.SessionInfo{
				Cwd:       "/tmp/project",
				SessionID: session.id,
				Title:     new(string(session.id)),
			},
			LastMessageID: session.lastMessageID,
		}); err != nil {
			t.Fatalf("CreateACPSession(%s): %v", session.id, err)
		}
	}

	var forkOutput bytes.Buffer
	if err := ForkStoredSession(t.Context(), ForkStoredSessionOptions{
		Store: store, Writer: &forkOutput, SessionID: "source-session",
	}); err != nil {
		t.Fatalf("ForkStoredSession: %v", err)
	}
	forkID := strings.TrimSpace(forkOutput.String())
	var forkCreatedAt time.Time
	if err := rawDB.QueryRowContext(t.Context(), "SELECT created_at FROM acp_sessions WHERE id = ?", forkID).Scan(&forkCreatedAt); err != nil {
		t.Fatalf("query fork creation time: %v", err)
	}
	oldTime := forkCreatedAt.Add(-365 * 24 * time.Hour)
	recentTime := forkCreatedAt.Add(-time.Minute)
	for _, update := range []struct {
		messageID string
		createdAt time.Time
	}{
		{oldMessageID, oldTime},
		{recentMessageID, recentTime},
	} {
		if _, err := rawDB.ExecContext(t.Context(), "UPDATE messages SET created_at = ? WHERE id = ?", update.createdAt, update.messageID); err != nil {
			t.Fatalf("update message timestamp: %v", err)
		}
	}
	if _, err := rawDB.ExecContext(t.Context(), "UPDATE acp_sessions SET created_at = ? WHERE id = ?", oldTime, "source-session"); err != nil {
		t.Fatalf("update source session timestamp: %v", err)
	}
	if _, err := rawDB.ExecContext(t.Context(), "UPDATE acp_sessions SET created_at = ? WHERE id = ?", recentTime, "recent-session"); err != nil {
		t.Fatalf("update recent session timestamp: %v", err)
	}

	summaries, err := store.ListACPSessionSummaries(t.Context(), storage.ListACPSessionSummariesOptions{Limit: 20})
	if err != nil {
		t.Fatalf("ListACPSessionSummaries: %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("len(summaries) = %d, want 3", len(summaries))
	}
	if summaries[0].SessionID != acpsdk.SessionId(forkID) {
		t.Fatalf("first session = %q, want new fork %q", summaries[0].SessionID, forkID)
	}
	if !summaries[0].LastModified.Equal(summaries[0].CreatedAt) {
		t.Fatalf("fork timestamps = created %v, modified %v; want equal", summaries[0].CreatedAt, summaries[0].LastModified)
	}
}

func TestShowStoredSessionRendersCompleteCompactedHistory(t *testing.T) {
	store, _ := newTestSqlite(t)
	lookupCall, err := gai.ToolCallBlock("call-lookup", "lookup", map[string]any{"query": "docs"})
	if err != nil {
		t.Fatalf("ToolCallBlock lookup: %v", err)
	}
	compactionCall, err := gai.ToolCallBlock("call-compact", "compact_conversation", map[string]any{"summary": "state"})
	if err != nil {
		t.Fatalf("ToolCallBlock compaction: %v", err)
	}

	priorLeafID := saveStoredDialogForCommandTest(t, store, gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("original question")}},
		{Role: gai.Assistant, Blocks: []gai.Block{
			{BlockType: gai.Thinking, ModalityType: gai.Text, Content: gai.Str("private reasoning")},
			lookupCall,
		}},
		{Role: gai.ToolResult, Blocks: []gai.Block{
			{ID: "call-lookup", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("lookup result")},
			{ID: "call-lookup", BlockType: gai.Content, ModalityType: gai.Image, MimeType: "image/png", Content: gai.Str("base64-image-data")},
		}},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("original answer")}},
		{Role: gai.Assistant, Blocks: []gai.Block{compactionCall}},
	})
	compactedLeafID := saveStoredDialogForCommandTest(t, store, gai.Dialog{
		{
			Role:        gai.User,
			Blocks:      []gai.Block{gai.TextBlock("compacted summary")},
			ExtraFields: map[string]any{storage.MessageCompactionParentIDKey: priorLeafID},
		},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("answer after compaction")}},
	})

	if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
		Session: acpsdk.SessionInfo{
			Cwd:       "/tmp/project",
			SessionID: "render-session",
			Title:     new("Rendered session"),
		},
		LastMessageID: compactedLeafID,
	}); err != nil {
		t.Fatalf("CreateACPSession: %v", err)
	}

	var output bytes.Buffer
	if err := ShowStoredSession(t.Context(), ShowStoredSessionOptions{
		Store: store, Writer: &output, SessionID: "render-session",
	}); err != nil {
		t.Fatalf("ShowStoredSession: %v", err)
	}
	got := output.String()
	for _, want := range []string{
		"# Rendered session",
		"## User",
		"original question",
		"### Thinking",
		"private reasoning",
		"### Tool Call: `lookup`",
		`"query": "docs"`,
		"## Tool Result",
		"lookup result",
		"image/png",
		"original answer",
		"### Tool Call: `compact_conversation`",
		"## Compaction",
		"compacted summary",
		"answer after compaction",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Markdown missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "base64-image-data") {
		t.Fatalf("Markdown contains raw binary payload:\n%s", got)
	}

	ordered := []string{"original question", "private reasoning", "lookup result", "original answer", "compact_conversation", "## Compaction", "compacted summary", "answer after compaction"}
	position := -1
	for _, text := range ordered {
		next := strings.Index(got, text)
		if next <= position {
			t.Fatalf("%q appears out of order in Markdown:\n%s", text, got)
		}
		position = next
	}
}

func TestForkAndDeleteStoredSessionPreserveSharedHistory(t *testing.T) {
	store, _ := newTestSqlite(t)
	priorLeafID := saveStoredDialogForCommandTest(t, store, gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("question")}},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("answer")}},
	})
	leafID := saveStoredDialogForCommandTest(t, store, gai.Dialog{
		{
			Role:        gai.User,
			Blocks:      []gai.Block{gai.TextBlock("compacted history")},
			ExtraFields: map[string]any{storage.MessageCompactionParentIDKey: priorLeafID},
		},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("answer after compaction")}},
	})
	if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
		Session: acpsdk.SessionInfo{
			Cwd:       "/tmp/source-project",
			SessionID: "source-session",
			Title:     new("Source"),
		},
		LastMessageID: leafID,
		ModelRef:      "model-ref",
		ThinkingLevel: "high",
	}); err != nil {
		t.Fatalf("CreateACPSession: %v", err)
	}
	if _, err := store.AddACPSessionCost(t.Context(), "source-session", 1.25); err != nil {
		t.Fatalf("AddACPSessionCost: %v", err)
	}

	var output bytes.Buffer
	if err := ForkStoredSession(t.Context(), ForkStoredSessionOptions{
		Store: store, Writer: &output, SessionID: "source-session",
	}); err != nil {
		t.Fatalf("ForkStoredSession: %v", err)
	}
	forkID := acpsdk.SessionId(strings.TrimSpace(output.String()))
	if forkID == "" || forkID == "source-session" {
		t.Fatalf("fork ID = %q", forkID)
	}
	fork, err := store.GetACPSession(t.Context(), forkID)
	if err != nil {
		t.Fatalf("GetACPSession fork: %v", err)
	}
	if fork.LastMessageID != leafID || fork.Session.Cwd != "/tmp/source-project" || fork.ModelRef != "model-ref" || fork.ThinkingLevel != "high" {
		t.Fatalf("fork metadata = %#v", fork)
	}
	if fork.CostUSD != 0 {
		t.Fatalf("fork CostUSD = %v, want 0", fork.CostUSD)
	}
	if fork.Session.Title == nil || *fork.Session.Title != string(forkID) {
		t.Fatalf("fork title = %v, want %q", fork.Session.Title, forkID)
	}

	if err := DeleteStoredSession(t.Context(), store, "source-session"); err != nil {
		t.Fatalf("DeleteStoredSession source: %v", err)
	}
	if _, err := store.GetACPSession(t.Context(), "source-session"); !errors.Is(err, storage.ErrSessionNotFound) {
		t.Fatalf("GetACPSession source error = %v, want ErrSessionNotFound", err)
	}
	fork, err = store.GetACPSession(t.Context(), forkID)
	if err != nil {
		t.Fatalf("GetACPSession fork after source delete: %v", err)
	}
	dialog, err := storage.GetDialogWithCompactions(t.Context(), store, fork.LastMessageID)
	if err != nil {
		t.Fatalf("GetDialogWithCompactions fork: %v", err)
	}
	if len(dialog) != 4 ||
		dialog[0].Blocks[0].Content.String() != "question" ||
		dialog[1].Blocks[0].Content.String() != "answer" ||
		dialog[2].Blocks[0].Content.String() != "compacted history" ||
		dialog[3].Blocks[0].Content.String() != "answer after compaction" {
		t.Fatalf("fork dialog = %#v", dialog)
	}
	messageIDs := make([]string, 0, len(dialog))
	for _, message := range dialog {
		messageIDs = append(messageIDs, storage.GetMessageID(message))
	}

	if err := DeleteStoredSession(t.Context(), store, forkID); err != nil {
		t.Fatalf("DeleteStoredSession fork: %v", err)
	}
	for _, messageID := range messageIDs {
		if _, err := store.GetMessages(t.Context(), []string{messageID}); !errors.Is(err, storage.ErrMessageNotFound) {
			t.Fatalf("GetMessages(%s) after final delete error = %v, want ErrMessageNotFound", messageID, err)
		}
	}
}

func saveStoredDialogForCommandTest(t *testing.T, store *storage.Sqlite, dialog gai.Dialog) string {
	t.Helper()
	lastMessageID := ""
	for message, err := range store.SaveDialog(t.Context(), slices.Values(dialog)) {
		if err != nil {
			t.Fatalf("SaveDialog: %v", err)
		}
		lastMessageID = storage.GetMessageID(message)
	}
	if lastMessageID == "" {
		t.Fatal("SaveDialog returned no messages")
	}
	return lastMessageID
}

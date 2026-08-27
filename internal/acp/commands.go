package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	acpsdk "github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

// ListStoredSessionsOptions configures terminal session listing.
type ListStoredSessionsOptions struct {
	Store    *storage.Sqlite
	Writer   io.Writer
	Page     uint64
	PageSize uint64
}

const maxStoredSessionsPageSize uint64 = 1000

// ListStoredSessions validates bounded pagination, queries one page, then writes
// the persisted ACP sessions as a table after every row renders successfully.
func ListStoredSessions(ctx context.Context, opts ListStoredSessionsOptions) error {
	if opts.Store == nil {
		return errors.New("provided conversation store cannot be nil")
	}
	if opts.Writer == nil {
		return errors.New("provided output writer cannot be nil")
	}
	if opts.Page == 0 {
		return errors.New("page must be at least 1")
	}
	if opts.PageSize == 0 {
		return errors.New("page size must be at least 1")
	}
	if opts.PageSize > maxStoredSessionsPageSize {
		return fmt.Errorf("page size must not exceed %d", maxStoredSessionsPageSize)
	}
	const maxSQLiteInteger = uint64(1<<63 - 1)
	pageIndex := opts.Page - 1
	if pageIndex > maxSQLiteInteger/opts.PageSize {
		return errors.New("page and page size produce an offset that is too large")
	}

	summaries, err := opts.Store.ListACPSessionSummaries(ctx, storage.ListACPSessionSummariesOptions{
		Limit:  opts.PageSize,
		Offset: pageIndex * opts.PageSize,
	})
	if err != nil {
		return fmt.Errorf("list ACP sessions: %w", err)
	}

	var output strings.Builder
	table := tabwriter.NewWriter(&output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "ID\tCREATED AT\tLAST MODIFIED\tTITLE"); err != nil {
		return fmt.Errorf("render ACP session table: %w", err)
	}
	for _, summary := range summaries {
		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\n",
			summary.SessionID,
			summary.CreatedAt.UTC().Format(time.RFC822),
			summary.LastModified.UTC().Format(time.RFC822),
			summary.Title,
		); err != nil {
			return fmt.Errorf("render ACP session table: %w", err)
		}
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("render ACP session table: %w", err)
	}
	if _, err := io.WriteString(opts.Writer, output.String()); err != nil {
		return fmt.Errorf("write ACP session table: %w", err)
	}
	return nil
}

// ShowStoredSessionOptions configures Markdown session rendering.
type ShowStoredSessionOptions struct {
	Store     *storage.Sqlite
	Writer    io.Writer
	SessionID acpsdk.SessionId
}

// ShowStoredSession writes the complete persisted session history as Markdown,
// including history from before each compaction.
func ShowStoredSession(ctx context.Context, opts ShowStoredSessionOptions) error {
	if opts.Store == nil {
		return errors.New("provided conversation store cannot be nil")
	}
	if opts.Writer == nil {
		return errors.New("provided output writer cannot be nil")
	}

	session, err := opts.Store.GetACPSession(ctx, opts.SessionID)
	if err != nil {
		return fmt.Errorf("get ACP session %s: %w", opts.SessionID, err)
	}
	var dialog gai.Dialog
	if session.LastMessageID != "" {
		dialog, err = storage.GetDialogWithCompactions(ctx, opts.Store, session.LastMessageID)
		if err != nil {
			return fmt.Errorf("get ACP session %s history: %w", opts.SessionID, err)
		}
	}

	markdown, err := renderSessionMarkdown(session, dialog)
	if err != nil {
		return fmt.Errorf("render ACP session %s: %w", opts.SessionID, err)
	}
	if _, err := io.WriteString(opts.Writer, markdown); err != nil {
		return fmt.Errorf("write ACP session %s: %w", opts.SessionID, err)
	}
	return nil
}

// renderSessionMarkdown writes session metadata, then renders each message and block according to its role, content kind, and compaction boundary.
func renderSessionMarkdown(session storage.GetACPSessionResponse, dialog gai.Dialog) (string, error) {
	title := string(session.Session.SessionID)
	if session.Session.Title != nil && *session.Session.Title != "" {
		title = *session.Session.Title
	}

	var markdown strings.Builder
	fmt.Fprintf(&markdown, "# %s\n\nSession ID: `%s`\n\n", title, session.Session.SessionID)
	if len(dialog) == 0 {
		markdown.WriteString("_No messages._\n")
		return markdown.String(), nil
	}

	for _, message := range dialog {
		if compactionParentID, _ := message.ExtraFields[storage.MessageCompactionParentIDKey].(string); compactionParentID != "" {
			markdown.WriteString("---\n\n## Compaction\n\n")
		}

		switch message.Role {
		case gai.User:
			markdown.WriteString("## User\n\n")
		case gai.Assistant:
			markdown.WriteString("## Assistant\n\n")
		case gai.ToolResult:
			markdown.WriteString("## Tool Result\n\n")
			if message.ToolResultError {
				markdown.WriteString("Status: error\n\n")
			} else {
				markdown.WriteString("Status: success\n\n")
			}
		default:
			return "", fmt.Errorf("unknown message role %v", message.Role)
		}

		for _, block := range message.Blocks {
			switch {
			case message.Role == gai.ToolResult:
				if block.ID != "" {
					fmt.Fprintf(&markdown, "### Tool Call `%s`\n\n", block.ID)
				}
				writeMarkdownContent(&markdown, block)
			case block.BlockType == gai.Thinking:
				markdown.WriteString("### Thinking\n\n")
				writeMarkdownContent(&markdown, block)
			case block.BlockType == gai.ToolCall:
				if err := writeMarkdownToolCall(&markdown, block); err != nil {
					return "", err
				}
			case block.BlockType == gai.Content:
				writeMarkdownContent(&markdown, block)
			default:
				fmt.Fprintf(&markdown, "### %s\n\n", block.BlockType)
				writeMarkdownContent(&markdown, block)
			}
		}
	}
	return markdown.String(), nil
}

func writeMarkdownToolCall(markdown *strings.Builder, block gai.Block) error {
	if block.Content == nil {
		return errors.New("tool call has no input")
	}
	var input gai.ToolCallInput
	if err := json.Unmarshal([]byte(block.Content.String()), &input); err != nil {
		return fmt.Errorf("decode tool call %s: %w", block.ID, err)
	}
	parameters := input.Parameters
	if parameters == nil {
		parameters = map[string]any{}
	}
	encoded, err := json.MarshalIndent(parameters, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tool call %s parameters: %w", block.ID, err)
	}

	fmt.Fprintf(markdown, "### Tool Call: `%s`\n\n", input.Name)
	if block.ID != "" {
		fmt.Fprintf(markdown, "ID: `%s`\n\n", block.ID)
	}
	fence := "```"
	for strings.Contains(string(encoded), fence) {
		fence += "`"
	}
	fmt.Fprintf(markdown, "%sjson\n%s\n%s\n\n", fence, encoded, fence)
	return nil
}

func writeMarkdownContent(markdown *strings.Builder, block gai.Block) {
	if block.ModalityType == gai.Text {
		if block.Content == nil || block.Content.String() == "" {
			markdown.WriteString("_Empty._\n\n")
			return
		}
		markdown.WriteString(block.Content.String())
		markdown.WriteString("\n\n")
		return
	}

	kind := block.ModalityType.String()
	if block.MimeType == pdfMIMEType {
		kind = "PDF"
	}
	fmt.Fprintf(markdown, "[%s content", kind)
	if block.MimeType != "" {
		fmt.Fprintf(markdown, ": %s", block.MimeType)
	}
	if filename, _ := block.ExtraFields[gai.BlockFieldFilenameKey].(string); filename != "" {
		fmt.Fprintf(markdown, ", %s", filename)
	}
	markdown.WriteString("]\n\n")
}

// DeleteStoredSession deletes one persisted ACP session and its unshared
// message history.
func DeleteStoredSession(ctx context.Context, store *storage.Sqlite, sessionID acpsdk.SessionId) error {
	if store == nil {
		return errors.New("provided conversation store cannot be nil")
	}
	if err := store.DeleteACPSession(ctx, sessionID); err != nil {
		return fmt.Errorf("delete ACP session %s: %w", sessionID, err)
	}
	return nil
}

// ForkStoredSessionOptions configures a persisted session fork.
type ForkStoredSessionOptions struct {
	Store     *storage.Sqlite
	Writer    io.Writer
	SessionID acpsdk.SessionId
}

// ForkStoredSession creates a session that shares the source history and writes
// only the new session ID to the configured output.
func ForkStoredSession(ctx context.Context, opts ForkStoredSessionOptions) error {
	if opts.Store == nil {
		return errors.New("provided conversation store cannot be nil")
	}
	if opts.Writer == nil {
		return errors.New("provided output writer cannot be nil")
	}

	source, err := opts.Store.GetACPSession(ctx, opts.SessionID)
	if err != nil {
		return fmt.Errorf("get ACP session %s: %w", opts.SessionID, err)
	}
	forkID := acpsdk.SessionId(storage.GenerateId())
	if err := opts.Store.CreateACPSession(ctx, storage.CreateACPSessionParams{
		Session: acpsdk.SessionInfo{
			Cwd:       source.Session.Cwd,
			SessionID: forkID,
			Title:     new(string(forkID)),
		},
		LastMessageID: source.LastMessageID,
		ModelRef:      source.ModelRef,
		ThinkingLevel: source.ThinkingLevel,
	}); err != nil {
		return fmt.Errorf("create fork of ACP session %s: %w", opts.SessionID, err)
	}
	if _, err := fmt.Fprintln(opts.Writer, forkID); err != nil {
		return fmt.Errorf("write forked ACP session ID: %w", err)
	}
	return nil
}

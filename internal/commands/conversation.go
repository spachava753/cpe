package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/render"
	"github.com/spachava753/cpe/internal/storage"
)

// ConversationListOptions configures ConversationList.
type ConversationListOptions struct {
	// Storage provides message listing and must return storage metadata keys
	// (MessageIDKey and MessageParentIDKey) required for tree reconstruction.
	Storage storage.MessagesLister
	// Writer receives the rendered conversation tree.
	Writer io.Writer
	// TreePrinter formats the reconstructed message forest.
	TreePrinter TreePrinter
}

// TreePrinter renders a forest of message-ID nodes.
type TreePrinter interface {
	// PrintMessageForest writes a complete rendering of roots to w.
	PrintMessageForest(w io.Writer, roots []MessageIdNode)
}

// ConversationList lists all stored conversations as a message forest.
//
// It requests ascending-order messages so parent nodes are encountered before
// descendants when building the forest.
func ConversationList(ctx context.Context, opts ConversationListOptions) error {
	msgs, err := opts.Storage.ListMessages(ctx, storage.ListMessagesOptions{AscendingOrder: true})
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	var allMsgs []gai.Message
	for msg := range msgs {
		allMsgs = append(allMsgs, msg)
	}

	if len(allMsgs) == 0 {
		fmt.Fprintln(opts.Writer, "No messages found.")
		return nil
	}

	forest := buildMessageForest(allMsgs)
	opts.TreePrinter.PrintMessageForest(opts.Writer, forest)
	return nil
}

// ConversationDeleteOptions configures ConversationDelete.
type ConversationDeleteOptions struct {
	// Storage performs actual message deletion.
	Storage storage.MessagesDeleter
	// MessageIDs are deleted one-by-one in the provided order.
	MessageIDs []string
	// Cascade maps to storage.DeleteMessagesOptions.Recursive.
	Cascade bool
	// Stdout receives per-ID success messages.
	Stdout io.Writer
	// Stderr receives per-ID failure messages.
	Stderr io.Writer
}

// ConversationDelete deletes requested message IDs with best-effort reporting.
//
// Deletions are attempted independently per ID: a failure for one ID is
// written to stderr and does not stop processing subsequent IDs.
func ConversationDelete(ctx context.Context, opts ConversationDeleteOptions) error {
	var errs []error

	for _, messageID := range opts.MessageIDs {
		delErr := opts.Storage.DeleteMessages(ctx, storage.DeleteMessagesOptions{
			MessageIDs: []string{messageID},
			Recursive:  opts.Cascade,
		})

		if delErr != nil {
			fmt.Fprintf(opts.Stderr, "Error deleting message %s: %v\n", messageID, delErr)
			errs = append(errs, fmt.Errorf("delete message %s: %w", messageID, delErr))
		} else {
			fmt.Fprintf(opts.Stdout, "Successfully deleted message %s", messageID)
			if opts.Cascade {
				fmt.Fprint(opts.Stdout, " and all its descendants")
			}
			fmt.Fprintln(opts.Stdout)
		}
	}

	return errors.Join(errs...)
}

// ConversationPrintOptions configures ConversationPrint.
type ConversationPrintOptions struct {
	// Storage fetches messages used to reconstruct the ancestor chain.
	Storage storage.MessagesGetter
	// MessageID is the leaf (or intermediate) message whose full chain is printed.
	MessageID string
	// Writer receives formatted output.
	Writer io.Writer
	// DialogFormatter controls markdown/text rendering.
	DialogFormatter DialogFormatter
}

// DialogFormatter formats a dialog for display.
type DialogFormatter interface {
	// FormatDialog receives messages in root-to-leaf order and optional message
	// IDs aligned by index with dialog.
	FormatDialog(dialog gai.Dialog, msgIds []string) (string, error)
}

// ConversationPrint loads and prints the root-to-leaf thread ending at
// opts.MessageID.
//
// Message IDs are extracted from ExtraFields[storage.MessageIDKey] and passed
// to the formatter as index-aligned metadata. Missing IDs are left empty.
func ConversationPrint(ctx context.Context, opts ConversationPrintOptions) error {
	dialog, err := storage.GetDialogForMessage(ctx, opts.Storage, opts.MessageID)
	if err != nil {
		return fmt.Errorf("failed to get dialog: %w", err)
	}

	// Extract message IDs from ExtraFields
	msgIds := make([]string, len(dialog))
	for i, msg := range dialog {
		if id, ok := msg.ExtraFields[storage.MessageIDKey].(string); ok {
			msgIds[i] = id
		}
	}

	formatted, err := opts.DialogFormatter.FormatDialog(dialog, msgIds)
	if err != nil {
		return fmt.Errorf("failed to format dialog: %w", err)
	}

	fmt.Fprint(opts.Writer, formatted)
	return nil
}

// MarkdownDialogFormatter formats dialogs into markdown and optionally renders
// them for terminal display.
type MarkdownDialogFormatter struct {
	Renderer render.Iface
}

// FormatDialog implements DialogFormatter.
//
// Formatting assumptions:
//   - dialog is ordered root-to-leaf.
//   - msgIds, when provided, is index-aligned with dialog.
//   - Tool-result code-mode detection relies on a tool_result message carrying
//     the originating tool-call block ID in its first block.
func (f *MarkdownDialogFormatter) FormatDialog(dialog gai.Dialog, msgIds []string) (string, error) {
	if len(dialog) == 0 {
		return "Empty conversation\n", nil
	}

	var md strings.Builder
	md.WriteString("# Conversation History\n\n")

	for i, message := range dialog {
		if i > 0 {
			md.WriteString("\n---\n\n")
		}

		var roleLabel string
		switch message.Role {
		case gai.User:
			roleLabel = "🧑 USER"
		case gai.Assistant:
			roleLabel = "🤖 ASSISTANT"
		case gai.ToolResult:
			statusLabel := "✓"
			if message.ToolResultError {
				statusLabel = "✗"
			}
			roleLabel = fmt.Sprintf("🔧 TOOL RESULT %s", statusLabel)
		default:
			roleLabel = fmt.Sprintf("UNKNOWN ROLE (%d)", message.Role)
		}

		var msgIdStr string
		if len(msgIds) > 0 && i < len(msgIds) {
			msgIdStr = fmt.Sprintf(" [`%s`]", msgIds[i])
		}

		md.WriteString(fmt.Sprintf("## %s%s\n\n", roleLabel, msgIdStr))

		for _, block := range message.Blocks {
			switch block.ModalityType {
			case gai.Text:
				content := block.Content.String()

				switch block.BlockType {
				case gai.ToolCall:
					md.WriteString(formatToolCallMarkdown(content))
				case gai.Thinking:
					md.WriteString("> **Thinking:**\n>\n")
					for line := range strings.SplitSeq(content, "\n") {
						md.WriteString(fmt.Sprintf("> %s\n", line))
					}
					md.WriteString("\n")
				default:
					if message.Role == gai.ToolResult {
						// Check if this is an execute_go_code result
						if isCodeModeResult(dialog, i) {
							md.WriteString(formatCodeModeResultMarkdown(content))
						} else if isJSON(content) {
							md.WriteString(formatJSONMarkdown(content))
						} else {
							md.WriteString(content)
							md.WriteString("\n\n")
						}
					} else {
						md.WriteString(content)
						md.WriteString("\n\n")
					}
				}
			default:
				md.WriteString(fmt.Sprintf("*[%s content, type: %s]*\n\n",
					formatModality(block.ModalityType),
					block.MimeType))
			}
		}
	}

	if rendered, err := f.Renderer.Render(md.String()); err == nil {
		return rendered, nil
	}
	// Fallback to plain markdown if rendering fails
	return md.String(), nil
}

// isCodeModeResult reports whether dialog[toolResultIndex] is an
// execute_go_code tool result.
//
// It assumes the result message's first block ID matches the originating
// assistant tool-call block ID in the immediately preceding message.
func isCodeModeResult(dialog gai.Dialog, toolResultIndex int) bool {
	if toolResultIndex <= 0 {
		return false
	}

	// Get the tool result message and its tool call ID from the first block
	resultMsg := dialog[toolResultIndex]
	if len(resultMsg.Blocks) == 0 {
		return false
	}
	toolCallID := resultMsg.Blocks[0].ID

	// Look at the previous message for tool calls
	prevMsg := dialog[toolResultIndex-1]
	if prevMsg.Role != gai.Assistant {
		return false
	}

	// Find the matching tool call block by ID
	for _, block := range prevMsg.Blocks {
		if block.BlockType == gai.ToolCall && block.ModalityType == gai.Text && block.ID == toolCallID {
			content := block.Content.String()
			if _, ok := codemode.ParseToolCall(content); ok {
				return true
			}
		}
	}

	return false
}

// formatCodeModeResultMarkdown formats an execute_go_code tool result as markdown.
// Uses the shared formatting function with no truncation for conversation history viewing.
func formatCodeModeResultMarkdown(content string) string {
	return codemode.FormatResultMarkdown(content, 0) + "\n\n"
}

// formatToolCallMarkdown formats serialized tool-call payloads as markdown.
//
// For execute_go_code payloads, it renders extracted Go input; otherwise it
// pretty-prints JSON when possible and falls back to raw content.
func formatToolCallMarkdown(content string) string {
	unescaped := unescapeJSONString(content)

	// Check if this is an execute_go_code tool call
	if input, ok := codemode.ParseToolCall(unescaped); ok {
		return codemode.FormatToolCallMarkdown(input) + "\n\n"
	}

	// Generic tool call formatting
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(unescaped), "", "  "); err != nil {
		return fmt.Sprintf("**Tool Call:**\n\n```json\n%s\n```\n\n", content)
	}
	return fmt.Sprintf("**Tool Call:**\n\n```json\n%s\n```\n\n", buf.String())
}

// formatJSONMarkdown formats a JSON string as a markdown code block
func formatJSONMarkdown(content string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(content), "", "  "); err != nil {
		return fmt.Sprintf("```json\n%s\n```\n\n", content)
	}
	return fmt.Sprintf("```json\n%s\n```\n\n", buf.String())
}

// isJSON checks if a string is valid JSON
func isJSON(str string) bool {
	str = strings.TrimSpace(str)
	if len(str) == 0 {
		return false
	}
	var js any
	return json.Unmarshal([]byte(str), &js) == nil
}

// unescapeJSONString unescapes special characters in JSON content
func unescapeJSONString(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if i+5 < len(s) && s[i:i+2] == "\\u" {
			hex := s[i+2 : i+6]
			var code int
			_, err := fmt.Sscanf(hex, "%04x", &code)
			if err == nil {
				r := rune(code)
				switch r {
				case '"':
					result.WriteString(`\"`)
				case '\\':
					result.WriteString(`\\`)
				case '\b':
					result.WriteString(`\b`)
				case '\f':
					result.WriteString(`\f`)
				case '\n':
					result.WriteString(`\n`)
				case '\r':
					result.WriteString(`\r`)
				case '\t':
					result.WriteString(`\t`)
				default:
					result.WriteRune(r)
				}
				i += 6
				continue
			}
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// formatModality converts a modality constant to a user-friendly string
func formatModality(modality gai.Modality) string {
	switch modality {
	case gai.Text:
		return "Text"
	case gai.Image:
		return "Image"
	case gai.Audio:
		return "Audio"
	case gai.Video:
		return "Video"
	default:
		return fmt.Sprintf("Unknown (%d)", modality)
	}
}

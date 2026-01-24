package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/storage"
)

// ConversationListOptions contains parameters for listing conversations
type ConversationListOptions struct {
	Storage interface {
		ListMessages(ctx context.Context) ([]storage.MessageIdNode, error)
	}
	Writer      io.Writer
	TreePrinter TreePrinter
}

// TreePrinter is an interface for printing message trees
type TreePrinter interface {
	PrintMessageForest(w io.Writer, roots []storage.MessageIdNode)
}

// ConversationList lists all conversations in tree format
func ConversationList(ctx context.Context, opts ConversationListOptions) error {
	messageNodes, err := opts.Storage.ListMessages(ctx)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	if len(messageNodes) == 0 {
		fmt.Fprintln(opts.Writer, "No messages found.")
		return nil
	}

	opts.TreePrinter.PrintMessageForest(opts.Writer, messageNodes)
	return nil
}

// ConversationDeleteOptions contains parameters for deleting conversations
type ConversationDeleteOptions struct {
	Storage interface {
		HasChildrenByID(ctx context.Context, messageID string) (bool, error)
		DeleteMessage(ctx context.Context, messageID string) error
		DeleteMessageRecursive(ctx context.Context, messageID string) error
	}
	MessageIDs []string
	Cascade    bool
	Stdout     io.Writer
	Stderr     io.Writer
}

// ConversationDelete deletes one or more messages
func ConversationDelete(ctx context.Context, opts ConversationDeleteOptions) error {
	for _, messageID := range opts.MessageIDs {
		hasChildren, err := opts.Storage.HasChildrenByID(ctx, messageID)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "Error checking if message %s has children: %v\n", messageID, err)
			continue
		}

		if hasChildren && !opts.Cascade {
			fmt.Fprintf(opts.Stderr, "Error: message %s has children. Use --cascade to delete it and all its descendants.\n", messageID)
			continue
		}

		var delErr error
		if opts.Cascade {
			delErr = opts.Storage.DeleteMessageRecursive(ctx, messageID)
		} else {
			delErr = opts.Storage.DeleteMessage(ctx, messageID)
		}

		if delErr != nil {
			fmt.Fprintf(opts.Stderr, "Error deleting message %s: %v\n", messageID, delErr)
		} else {
			fmt.Fprintf(opts.Stdout, "Successfully deleted message %s", messageID)
			if opts.Cascade && hasChildren {
				fmt.Fprint(opts.Stdout, " and all its descendants")
			}
			fmt.Fprintln(opts.Stdout)
		}
	}

	return nil
}

// ConversationPrintOptions contains parameters for printing a conversation
type ConversationPrintOptions struct {
	Storage interface {
		GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error)
	}
	MessageID       string
	Writer          io.Writer
	DialogFormatter DialogFormatter
}

// DialogFormatter formats a dialog for display
type DialogFormatter interface {
	FormatDialog(dialog gai.Dialog, msgIds []string) (string, error)
}

// ConversationPrint prints a conversation thread
func ConversationPrint(ctx context.Context, opts ConversationPrintOptions) error {
	dialog, msgIds, err := opts.Storage.GetDialogForMessage(ctx, opts.MessageID)
	if err != nil {
		return fmt.Errorf("failed to get dialog: %w", err)
	}

	formatted, err := opts.DialogFormatter.FormatDialog(dialog, msgIds)
	if err != nil {
		return fmt.Errorf("failed to format dialog: %w", err)
	}

	fmt.Fprint(opts.Writer, formatted)
	return nil
}

// MarkdownDialogFormatter formats dialogs as markdown with glamour rendering
type MarkdownDialogFormatter struct {
	Renderer MarkdownRenderer
}

// FormatDialog implements DialogFormatter
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

// isCodeModeResult checks if a tool result at the given index is from an execute_go_code tool call.
// It matches the tool result's ID with the corresponding tool call in the previous assistant message.
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
			if _, ok := agent.ParseExecuteGoCodeToolCall(content); ok {
				return true
			}
		}
	}

	return false
}

// formatCodeModeResultMarkdown formats an execute_go_code tool result as markdown.
// Uses the shared formatting function with no truncation for conversation history viewing.
func formatCodeModeResultMarkdown(content string) string {
	return agent.FormatExecuteGoCodeResultMarkdown(content, 0) + "\n\n"
}

// formatToolCallMarkdown formats a tool call JSON string as a markdown code block.
// For execute_go_code tool calls, displays the Go code with syntax highlighting.
func formatToolCallMarkdown(content string) string {
	unescaped := unescapeJSONString(content)

	// Check if this is an execute_go_code tool call
	if input, ok := agent.ParseExecuteGoCodeToolCall(unescaped); ok {
		return agent.FormatExecuteGoCodeToolCallMarkdown(input) + "\n\n"
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

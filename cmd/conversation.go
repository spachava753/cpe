package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// convoCmd represents the conversation management command
var convoCmd = &cobra.Command{
	Use:     "conversation",
	Short:   "Manage conversations",
	Long:    `Manage conversations stored in the database.`,
	Aliases: []string{"convo", "conv"},
}

// listConvoCmd represents the conversation list command
var listConvoCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all messages in a git commit graph style",
	Long:    `Display all messages in the database with parent-child relationships in a git commit graph style.`,
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize the database connection
		dbPath := ".cpeconvo"
		dialogStorage, err := storage.InitDialogStorage(dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize dialog storage: %v", err)
		}
		defer dialogStorage.Close()

		// Fetch all messages as a hierarchical structure
		messageNodes, err := dialogStorage.ListMessages(cmd.Context())
		if err != nil {
			return fmt.Errorf("failed to list messages: %v", err)
		}

		if len(messageNodes) == 0 {
			fmt.Println("No messages found.")
			return nil
		}

		PrintMessageForest(os.Stdout, messageNodes)
		return nil
	},
}

// deleteConvoCmd represents the conversation delete command
var deleteConvoCmd = &cobra.Command{
	Use:     "delete [messageID...]",
	Short:   "Delete one or more messages",
	Long:    `Delete one or more messages by their ID. If a message has children, you must use the --cascade flag to delete it and all its descendants.`,
	Aliases: []string{"rm", "remove"},
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get cascade flag
		cascade, _ := cmd.Flags().GetBool("cascade")

		// Initialize the database connection
		dbPath := ".cpeconvo"
		dialogStorage, err := storage.InitDialogStorage(dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize dialog storage: %v", err)
		}
		defer dialogStorage.Close()

		for _, messageID := range args {
			// Check if the message has children
			hasChildren, err := dialogStorage.HasChildrenByID(cmd.Context(), messageID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error checking if message %s has children: %v\n", messageID, err)
				continue
			}

			if hasChildren && !cascade {
				fmt.Fprintf(os.Stderr, "Error: message %s has children. Use --cascade to delete it and all its descendants.\n", messageID)
				continue
			}

			var delErr error
			if cascade {
				delErr = dialogStorage.DeleteMessageRecursive(cmd.Context(), messageID)
			} else {
				delErr = dialogStorage.DeleteMessage(cmd.Context(), messageID)
			}

			if delErr != nil {
				fmt.Fprintf(os.Stderr, "Error deleting message %s: %v\n", messageID, delErr)
			} else {
				fmt.Printf("Successfully deleted message %s", messageID)
				if cascade && hasChildren {
					fmt.Printf(" and all its descendants")
				}
				fmt.Println()
			}
		}

		return nil
	},
}

// printConvoCmd represents the conversation print command
var printConvoCmd = &cobra.Command{
	Use:     "print [messageID]",
	Short:   "Print conversation history",
	Long:    `Print the entire conversation history leading up to the specified message ID.`,
	Aliases: []string{"show", "view"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		messageID := args[0]

		// Initialize the database connection
		dbPath := ".cpeconvo"
		dialogStorage, err := storage.InitDialogStorage(dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize dialog storage: %v", err)
		}
		defer dialogStorage.Close()

		dialog, msgIds, err := dialogStorage.GetDialogForMessage(cmd.Context(), messageID)
		if err != nil {
			return fmt.Errorf("failed to get dialog: %v", err)
		}
		printDialog(dialog, msgIds)

		return nil
	},
}

// printDialog prints the full dialog to stdout in a readable format
func printDialog(dialog gai.Dialog, msgIds []string) {
	if len(dialog) == 0 {
		fmt.Println("Empty conversation")
		return
	}

	// Build markdown document
	var md strings.Builder
	md.WriteString("# Conversation History\n\n")

	for i, message := range dialog {
		// Add separator between messages
		if i > 0 {
			md.WriteString("\n---\n\n")
		}

		// Format based on role
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

		// Add message ID if available
		// Dialog is in oldest message first order, so map to the correct ID
		var msgIdStr string
		if len(msgIds) > 0 {
			msgIdStr = fmt.Sprintf(" [`%s`]", msgIds[i])
		}

		md.WriteString(fmt.Sprintf("## %s%s\n\n", roleLabel, msgIdStr))

		// Print each block in the message
		for _, block := range message.Blocks {
			switch block.ModalityType {
			case gai.Text:
				content := block.Content.String()

				// Handle different block types
				switch block.BlockType {
				case gai.ToolCall:
					// Pretty-print tool call JSON
					md.WriteString(formatToolCallMarkdown(content))
				case gai.Thinking:
					// Render thinking as a quote
					md.WriteString("> **Thinking:**\n>\n")
					for _, line := range strings.Split(content, "\n") {
						md.WriteString(fmt.Sprintf("> %s\n", line))
					}
					md.WriteString("\n")
				default:
					// For tool results, check if content is JSON and format it
					if message.Role == gai.ToolResult && isJSON(content) {
						md.WriteString(formatJSONMarkdown(content))
					} else {
						// Regular text content
						md.WriteString(content)
						md.WriteString("\n\n")
					}
				}
			default:
				// Non-text modality
				md.WriteString(fmt.Sprintf("*[%s content, type: %s]*\n\n",
					formatModality(block.ModalityType),
					block.MimeType))
			}
		}
	}

	// Render the markdown with glamour
	renderer := createGlamourRenderer()
	rendered, err := renderer.Render(md.String())
	if err != nil {
		// Fallback to plain markdown if rendering fails
		fmt.Print(md.String())
		return
	}

	fmt.Print(rendered)
}

// createGlamourRenderer creates a glamour renderer with appropriate styling
func createGlamourRenderer() *glamour.TermRenderer {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		style := styles.NoTTYStyleConfig
		style.Document.BlockPrefix = ""
		renderer, _ := glamour.NewTermRenderer(
			glamour.WithStyles(style),
			glamour.WithWordWrap(120),
		)
		return renderer
	}

	style := styles.LightStyleConfig
	if termenv.HasDarkBackground() {
		style = styles.DarkStyleConfig
	}

	style.Document.BlockPrefix = ""
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(120),
	)
	return renderer
}

// formatToolCallMarkdown formats a tool call JSON string as a markdown code block
func formatToolCallMarkdown(content string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(unescapeJSONString(content)), "", "  "); err != nil {
		// If indent fails, return as-is
		return fmt.Sprintf("```json\n%s\n```\n\n", content)
	}
	return fmt.Sprintf("**Tool Call:**\n\n```json\n%s\n```\n\n", buf.String())
}

// formatJSONMarkdown formats a JSON string as a markdown code block
func formatJSONMarkdown(content string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(content), "", "  "); err != nil {
		// If indent fails, return as-is
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
	var js interface{}
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

func init() {
	rootCmd.AddCommand(convoCmd)
	convoCmd.AddCommand(listConvoCmd)
	convoCmd.AddCommand(deleteConvoCmd)
	convoCmd.AddCommand(printConvoCmd)

	// Add cascade flag to delete command
	deleteConvoCmd.Flags().Bool("cascade", false, "Cascade delete all child messages too")
}

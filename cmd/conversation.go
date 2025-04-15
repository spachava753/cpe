package cmd

import (
	"fmt"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
	"os"
	"strings"
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

		dialog, _, err := dialogStorage.GetDialogForMessage(cmd.Context(), messageID)
		if err != nil {
			return fmt.Errorf("failed to get dialog: %v", err)
		}
		printDialog(dialog)

		return nil
	},
}

// printDialog prints the full dialog to stdout in a readable format
func printDialog(dialog gai.Dialog) {
	if len(dialog) == 0 {
		fmt.Println("Empty conversation")
		return
	}

	fmt.Println("\n=== Conversation History ===")

	for i, message := range dialog {
		// Print a separator between messages
		if i > 0 {
			fmt.Println("\n" + strings.Repeat("-", 80))
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

		fmt.Printf("\n%s\n\n", roleLabel)

		// Print each block in the message
		for _, block := range message.Blocks {
			// Only print text content directly
			if block.ModalityType == gai.Text {
				fmt.Println(block.Content.String())
			} else {
				fmt.Printf("[%s content, type: %s]\n",
					formatModality(block.ModalityType),
					block.MimeType)
			}
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
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

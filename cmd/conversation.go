package cmd

import (
	"fmt"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
	"os"
	"sort"
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

		// Fetch all messages
		messages, err := dialogStorage.ListMessages(cmd.Context())
		if err != nil {
			return fmt.Errorf("failed to list messages: %v", err)
		}

		if len(messages) == 0 {
			fmt.Println("No messages found.")
			return nil
		}

		// Build a tree structure from the messages
		// The key is message ID, the value is a list of child message IDs
		messageChildren := make(map[string][]string)
		// Track which messages are root messages (no parent)
		rootMessages := make(map[string]bool)
		// Store message content by ID for easy access
		messageContent := make(map[string]storage.Message)
		// Map to store message blocks for content display
		messageBlocks := make(map[string][]storage.Block)

		for _, msg := range messages {
			messageContent[msg.ID] = msg

			// Get blocks for this message for later content display
			blocks, err := dialogStorage.GetBlocksByMessage(cmd.Context(), msg.ID)
			if err == nil && len(blocks) > 0 {
				messageBlocks[msg.ID] = blocks
			}

			if msg.ParentID.Valid {
				// Add this message as a child of its parent
				parentID := msg.ParentID.String
				messageChildren[parentID] = append(messageChildren[parentID], msg.ID)
			} else {
				// Mark this as a root message (no parent)
				rootMessages[msg.ID] = true
			}
		}

		// Sort children by creation time to ensure consistent display order
		for parentID, children := range messageChildren {
			sort.Slice(children, func(i, j int) bool {
				return messageContent[children[i]].CreatedAt.Before(messageContent[children[j]].CreatedAt)
			})
			messageChildren[parentID] = children
		}

		// Sort root messages by creation time (oldest first)
		var rootIDs []string
		for id := range rootMessages {
			rootIDs = append(rootIDs, id)
		}
		sort.Slice(rootIDs, func(i, j int) bool {
			return messageContent[rootIDs[i]].CreatedAt.Before(messageContent[rootIDs[j]].CreatedAt)
		})

		// Keep track of columns used in the graph
		var outputLines []string
		var inProgress []string // tracks the current branch lines

		// Start with root messages
		for _, msgID := range rootIDs {
			// Generate the commit graph for this root message and all its descendants
			printMessageGraph(msgID, "", messageContent, messageChildren, messageBlocks, &outputLines, &inProgress)
		}

		// Print the final graph
		fmt.Println("Conversation Message Graph:")
		for _, line := range outputLines {
			fmt.Println(line)
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

// printMessageGraph creates the git-like graph for a message and its descendants
func printMessageGraph(messageID string, prefix string, messageContent map[string]storage.Message,
	messageChildren map[string][]string, messageBlocks map[string][]storage.Block,
	outputLines *[]string, inProgress *[]string) {

	// Get the message content
	msg, exists := messageContent[messageID]
	if !exists {
		return
	}

	// Get message content for display
	role := msg.Role
	content := formatMessageContent(msg, messageBlocks[messageID])

	// Build the graph line
	graphLine := buildGraphLine(*inProgress, prefix, messageID, role, content)

	// Add to output
	*outputLines = append(*outputLines, graphLine)

	// Check for children
	children, hasChildren := messageChildren[messageID]
	if !hasChildren || len(children) == 0 {
		return
	}

	// Process children
	for i, childID := range children {
		isLast := i == len(children)-1

		// Determine the prefix for the child
		var childPrefix string
		var nextBranchPrefix string
		if isLast {
			childPrefix = prefix + "└── "
			nextBranchPrefix = prefix + "    "
		} else {
			childPrefix = prefix + "├── "
			nextBranchPrefix = prefix + "│   "
		}

		// Update inProgress to show the current branch state
		*inProgress = append(*inProgress, nextBranchPrefix)

		// Process the child
		printMessageGraph(childID, childPrefix, messageContent, messageChildren, messageBlocks, outputLines, inProgress)

		// Remove the last element from inProgress after processing this child
		if len(*inProgress) > 0 {
			*inProgress = (*inProgress)[:len(*inProgress)-1]
		}
	}
}

// buildGraphLine constructs a line of the git-like graph
func buildGraphLine(inProgress []string, prefix, id, role, content string) string {
	// Format role to be more readable
	roleDisplay := role
	switch role {
	case "user":
		roleDisplay = "User"
	case "assistant":
		roleDisplay = "Assistant"
	case "tool_result":
		roleDisplay = "Tool"
	}

	// Calculate how much space we need for ID and role
	idRolePart := fmt.Sprintf("%s (%s): ", id, roleDisplay)

	// Build the graph line with clean prefix
	graphLine := prefix + idRolePart + content

	return graphLine
}

// formatMessageContent truncates and formats message content for display
func formatMessageContent(msg storage.Message, blocks []storage.Block) string {
	// Default content if we can't extract from blocks
	content := fmt.Sprintf("Message from %s", msg.CreatedAt.Format("2006-01-02 15:04:05"))

	// Try to extract actual content from blocks
	if len(blocks) > 0 {
		// Find text blocks, prioritize content blocks
		for _, block := range blocks {
			if block.BlockType == gai.Content && block.ModalityType == int64(gai.Text) {
				content = block.Content
				break
			}
		}

		// If no suitable block found, use the first one
		if content == fmt.Sprintf("Message from %s", msg.CreatedAt.Format("2006-01-02 15:04:05")) && len(blocks) > 0 {
			content = blocks[0].Content
		}
	}

	// Strip newlines and extra whitespace
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.Join(strings.Fields(content), " ")

	// Truncate to 50 chars if needed
	if len(content) > 50 {
		content = content[:47] + "..."
	}

	return content
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

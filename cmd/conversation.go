package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spf13/cobra"
)

var (
	// Flags for the conversation command
	deleteCascade bool
)

// conversationCmd represents the conversation command
var conversationCmd = &cobra.Command{
	Use:     "conversation",
	Short:   "Manage conversations",
	Long:    `Manage conversations with list, print, and delete operations.`,
	Aliases: []string{"convo", "conv"},
}

// listConversationCmd represents the list subcommand
var listConversationCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all conversations",
	Long:    `List all conversations with their IDs, parent IDs, models, creation times, and message previews.`,
	Aliases: []string{"ls"},
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize conversation manager
		dbPath := ".cpeconvo"
		convoManager, err := conversation.NewManager(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize conversation manager: %v\n", err)
			os.Exit(1)
		}
		defer convoManager.Close()

		conversations, err := convoManager.ListConversations(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to list conversations: %v\n", err)
			os.Exit(1)
		}

		// Create and configure table
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"ID", "Parent ID", "Model", "Created At", "Message"})
		table.SetAutoWrapText(false)
		table.SetAutoFormatHeaders(true)
		table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
		table.SetAlignment(tablewriter.ALIGN_LEFT)
		table.SetCenterSeparator("")
		table.SetColumnSeparator("")
		table.SetRowSeparator("")
		table.SetHeaderLine(false)
		table.SetBorder(false)
		table.SetTablePadding("\t")
		table.SetNoWhiteSpace(true)

		// Add rows to table
		for _, conv := range conversations {
			parentID := "-"
			if conv.ParentID.Valid {
				parentID = conv.ParentID.String
			}
			// Truncate user message if too long
			message := conv.UserMessage
			if len(message) > 50 {
				message = message[:47] + "..."
			}
			table.Append([]string{
				conv.ID,
				parentID,
				conv.Model,
				conv.CreatedAt.Format("2006-01-02 15:04:05"),
				message,
			})
		}

		// Render table
		table.Render()
	},
}

// printConversationCmd represents the print subcommand
var printConversationCmd = &cobra.Command{
	Use:     "print [conversation-id]",
	Short:   "Print a specific conversation",
	Long:    `Print the details and content of a specific conversation by ID.`,
	Args:    cobra.ExactArgs(1),
	Aliases: []string{"show", "view"},
	Run: func(cmd *cobra.Command, args []string) {
		conversationID := args[0]

		// Initialize conversation manager
		dbPath := ".cpeconvo"
		convoManager, err := conversation.NewManager(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize conversation manager: %v\n", err)
			os.Exit(1)
		}
		defer convoManager.Close()

		conv, err := convoManager.GetConversation(context.Background(), conversationID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get conversation: %v\n", err)
			os.Exit(1)
		}

		// Print conversation metadata
		fmt.Printf("Conversation ID: %s\n", conv.ID)
		if conv.ParentID.Valid {
			fmt.Printf("Parent ID: %s\n", conv.ParentID.String)
		}
		fmt.Printf("Model: %s\n", conv.Model)
		fmt.Printf("Created At: %s\n\n", conv.CreatedAt.Format(time.RFC3339))

		// Create an executor of the appropriate type to print messages
		executor, err := agent.InitExecutor(log.New(os.Stderr, "", 0), agent.ModelOptions{
			Continue: conversationID,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize executor: %v\n", err)
			os.Exit(1)
		}

		// Print conversation messages
		fmt.Println("Messages:")
		fmt.Println("=========")
		fmt.Print(executor.PrintMessages())
	},
}

// deleteConversationCmd represents the delete subcommand
var deleteConversationCmd = &cobra.Command{
	Use:     "delete [conversation-id]",
	Short:   "Delete a specific conversation",
	Long:    `Delete a specific conversation by ID. Use --cascade to also delete child conversations.`,
	Args:    cobra.ExactArgs(1),
	Aliases: []string{"rm", "remove"},
	Run: func(cmd *cobra.Command, args []string) {
		conversationID := args[0]

		// Initialize conversation manager
		dbPath := ".cpeconvo"
		convoManager, err := conversation.NewManager(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize conversation manager: %v\n", err)
			os.Exit(1)
		}
		defer convoManager.Close()

		if err := convoManager.DeleteConversation(context.Background(), conversationID, deleteCascade); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to delete conversation: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully deleted conversation %s\n", conversationID)
		if deleteCascade {
			fmt.Println("All child conversations were also deleted.")
		}
	},
}

func init() {
	rootCmd.AddCommand(conversationCmd)

	// Add subcommands to conversation command
	conversationCmd.AddCommand(listConversationCmd)
	conversationCmd.AddCommand(printConversationCmd)
	conversationCmd.AddCommand(deleteConversationCmd)

	// Add flags to delete subcommand
	deleteConversationCmd.Flags().BoolVar(&deleteCascade, "cascade", false, "When deleting a conversation, also delete its children")
}

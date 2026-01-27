package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/storage"
)

const defaultDBPath = ".cpeconvo"

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
		dbPath := defaultDBPath
		dialogStorage, err := storage.InitDialogStorage(cmd.Context(), dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize dialog storage: %w", err)
		}
		defer dialogStorage.Close()

		return commands.ConversationList(cmd.Context(), commands.ConversationListOptions{
			Storage:     dialogStorage,
			Writer:      os.Stdout,
			TreePrinter: &commands.DefaultTreePrinter{},
		})
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
		cascade, _ := cmd.Flags().GetBool("cascade")

		dbPath := defaultDBPath
		dialogStorage, err := storage.InitDialogStorage(cmd.Context(), dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize dialog storage: %w", err)
		}
		defer dialogStorage.Close()

		return commands.ConversationDelete(cmd.Context(), commands.ConversationDeleteOptions{
			Storage:    dialogStorage,
			MessageIDs: args,
			Cascade:    cascade,
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
		})
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
		dbPath := defaultDBPath
		dialogStorage, err := storage.InitDialogStorage(cmd.Context(), dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize dialog storage: %w", err)
		}
		defer dialogStorage.Close()

		return commands.ConversationPrint(cmd.Context(), commands.ConversationPrintOptions{
			Storage:         dialogStorage,
			MessageID:       args[0],
			Writer:          os.Stdout,
			DialogFormatter: &commands.MarkdownDialogFormatter{Renderer: agent.NewRenderer()},
		})
	},
}

func init() {
	rootCmd.AddCommand(convoCmd)
	convoCmd.AddCommand(listConvoCmd)
	convoCmd.AddCommand(deleteConvoCmd)
	convoCmd.AddCommand(printConvoCmd)

	deleteConvoCmd.Flags().Bool("cascade", false, "Cascade delete all child messages too")
}

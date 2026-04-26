package cmd

import (
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/commands"
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
		return commands.ConversationListFromConfig(cmd.Context(), commands.ConversationListFromConfigOptions{
			ConversationStoragePath: conversationStoragePath,
			Writer:                  cmd.OutOrStdout(),
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

		return commands.ConversationDeleteFromConfig(cmd.Context(), commands.ConversationDeleteFromConfigOptions{
			ConversationStoragePath: conversationStoragePath,
			MessageIDs:              args,
			Cascade:                 cascade,
			Stdout:                  cmd.OutOrStdout(),
			Stderr:                  cmd.ErrOrStderr(),
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
		return commands.ConversationPrintFromConfig(cmd.Context(), commands.ConversationPrintFromConfigOptions{
			ConversationStoragePath: conversationStoragePath,
			MessageID:               args[0],
			Writer:                  cmd.OutOrStdout(),
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

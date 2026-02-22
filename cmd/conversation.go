package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
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
		db, dialogStorage, err := openConversationStorage(cmd.Context(), configPath)
		if err != nil {
			return err
		}
		defer db.Close()

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

		db, dialogStorage, err := openConversationStorage(cmd.Context(), configPath)
		if err != nil {
			return err
		}
		defer db.Close()

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
		db, dialogStorage, err := openConversationStorage(cmd.Context(), configPath)
		if err != nil {
			return err
		}
		defer db.Close()

		return commands.ConversationPrint(cmd.Context(), commands.ConversationPrintOptions{
			Storage:         dialogStorage,
			MessageID:       args[0],
			Writer:          os.Stdout,
			DialogFormatter: &commands.MarkdownDialogFormatter{Renderer: agent.NewRenderer()},
		})
	},
}

func resolveConversationDBPath(explicitConfigPath string) (string, error) {
	rawCfg, resolvedConfigPath, err := config.LoadRawConfigWithPath(explicitConfigPath)
	if err != nil {
		if explicitConfigPath == "" && errors.Is(err, config.ErrConfigNotFound) {
			return config.DefaultConversationStoragePath, nil
		}
		return "", err
	}

	dbPath, err := config.ResolveConversationStoragePath(rawCfg.Defaults, resolvedConfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve conversation storage path: %w", err)
	}
	return dbPath, nil
}

func openConversationStorage(ctx context.Context, explicitConfigPath string) (*sql.DB, *storage.Sqlite, error) {
	dbPath, err := resolveConversationDBPath(explicitConfigPath)
	if err != nil {
		return nil, nil, err
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %w", err)
	}

	dialogStorage, err := storage.NewSqlite(ctx, db)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to initialize dialog storage: %w", err)
	}

	return db, dialogStorage, nil
}

func init() {
	rootCmd.AddCommand(convoCmd)
	convoCmd.AddCommand(listConvoCmd)
	convoCmd.AddCommand(deleteConvoCmd)
	convoCmd.AddCommand(printConvoCmd)

	deleteConvoCmd.Flags().Bool("cascade", false, "Cascade delete all child messages too")
}

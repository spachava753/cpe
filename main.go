package main

import (
	"context"
	_ "embed"
	"fmt"
	"github.com/olekukonko/tablewriter"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/cliopts"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/tokentree"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// getVersion returns the version of the application from build info
func getVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(unknown version)"
}

func main() {
	startTime := time.Now()
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
	defer func() {
		elapsed := time.Since(startTime)
		log.Printf("finished execution, elapsed: %s", elapsed)
	}()

	config, err := parseConfig()
	if err != nil {
		log.Fatalf("fatal error: %s", err)
	}

	// Handle conversation commands if any
	if err := handleConversationCommands(config); err != nil {
		log.Fatalf("fatal error: %s", err)
	}

	// Initialize ignorer
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		log.Fatalf("fatal error: %s", err)
	}
	if ignorer == nil {
		log.Fatal("git ignorer was nil")
	}

	if config.TokenCountPath != "" {
		if err := tokentree.PrintTokenTree(os.DirFS("."), ignorer); err != nil {
			log.Fatalf("fatal error: %s", err)
		}
		return
	}

	if config.Overview {
		result, err := agent.ExecuteFilesOverviewTool(ignorer)
		if err != nil {
			log.Fatalf("fatal error: %s", err)
		}
		if _, writeErr := fmt.Fprint(os.Stdout, result.Content); writeErr != nil {
			log.Fatalf("fatal error: %s", writeErr)
		}
		return
	}

	if config.RelatedFiles != "" {
		// Split the comma-separated list of files
		inputFiles := strings.Split(config.RelatedFiles, ",")
		// Trim whitespace from each file path
		for i := range inputFiles {
			inputFiles[i] = strings.TrimSpace(inputFiles[i])
		}
		result, err := agent.ExecuteGetRelatedFilesTool(inputFiles, ignorer)
		if err != nil {
			log.Fatalf("fatal error: %s", err)
		}
		if _, writeErr := fmt.Fprint(os.Stdout, result.Content); writeErr != nil {
			log.Fatalf("fatal error: %s", writeErr)
		}
		return
	}

	if config.ListFiles {
		files, err := agent.ListTextFiles(ignorer)
		if err != nil {
			log.Fatalf("fatal error: %s", err)
		}
		for _, file := range files {
			if _, writeErr := fmt.Fprintf(os.Stdout, "File: %s\nContent:\n%s\n\n", file.Path, file.Content); writeErr != nil {
				log.Fatalf("fatal error: %s", writeErr)
			}
		}
		return
	}

	executor, err := agent.InitExecutor(log.Default(), agent.ModelOptions{
		Model:              config.Model,
		CustomURL:          config.CustomURL,
		MaxTokens:          config.MaxTokens,
		Temperature:        config.Temperature,
		TopP:               config.TopP,
		TopK:               config.TopK,
		FrequencyPenalty:   config.FrequencyPenalty,
		PresencePenalty:    config.PresencePenalty,
		NumberOfResponses:  config.NumberOfResponses,
		Input:              config.Input,
		Version:            config.Version,
		Continue:           config.Continue,
		ListConversations:  config.ListConversations,
		DeleteConversation: config.DeleteConversation,
		DeleteCascade:      config.DeleteCascade,
		PrintConversation:  config.PrintConversation,
	})
	if err != nil {
		slog.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}

	input, err := readInput(config.Input)
	if err != nil {
		slog.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}

	if err := executor.Execute(input); err != nil {
		slog.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}
}

func parseConfig() (cliopts.Options, error) {
	cliopts.ParseFlags()

	if cliopts.Opts.Version {
		fmt.Printf("cpe version %s\n", getVersion())
		os.Exit(0)
	}

	if cliopts.Opts.Model != "" && cliopts.Opts.Model != agent.DefaultModel {
		_, ok := agent.ModelConfigs[cliopts.Opts.Model]
		if !ok && cliopts.Opts.CustomURL == "" {
			return cliopts.Options{}, fmt.Errorf("unknown model '%s' requires -custom-url flag", cliopts.Opts.Model)
		}
	}

	return cliopts.Opts, nil
}

func readInput(inputPath string) (string, error) {
	var inputs []string

	// Check if there is any input from stdin by checking if stdin is a pipe or redirection
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Stdin has data available
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("error reading from stdin: %w", err)
		}
		if len(content) > 0 {
			inputs = append(inputs, string(content))
		}
	}

	// Check if there is input from the -input flag
	if inputPath != "" {
		content, err := os.ReadFile(inputPath)
		if err != nil {
			return "", fmt.Errorf("error opening input file %s: %w", inputPath, err)
		}
		if len(content) > 0 {
			inputs = append(inputs, string(content))
		}
	}

	// Check if there is input from command line arguments
	if cliopts.Opts.Prompt != "" {
		inputs = append(inputs, cliopts.Opts.Prompt)
	}

	// Combine all inputs with double newlines
	input := strings.Join(inputs, "\n\n")

	if input == "" {
		return "", fmt.Errorf("no input provided. Please provide input via stdin, input file, or as a command line argument")
	}

	return input, nil
}

func handleConversationCommands(config cliopts.Options) error {
	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	// Initialize conversation manager
	dbPath := filepath.Join(homeDir, ".config", "cpe", "conversations.db")
	manager, err := conversation.NewManager(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize conversation manager: %w", err)
	}
	defer manager.Close()

	ctx := context.Background()

	if config.ListConversations {
		// List all conversations
		conversations, err := manager.ListConversations(ctx)
		if err != nil {
			return fmt.Errorf("failed to list conversations: %w", err)
		}

		if len(conversations) == 0 {
			fmt.Println("No conversations found.")
			return nil
		}

		// Create and configure the table writer
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"ID", "Parent ID", "Model", "Message", "Created At"})
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

		// Add rows to the table
		for _, conv := range conversations {
			parentID := ""
			if conv.ParentID.Valid {
				parentID = conv.ParentID.String
			}
			
			// Truncate user message if it's too long
			message := conv.UserMessage
			if len(message) > 50 {
				message = message[:47] + "..."
			}

			table.Append([]string{
				conv.ID,
				parentID,
				conv.Model,
				message,
				conv.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}

		// Render the table
		table.Render()
		return nil
	}

	if config.DeleteConversation != "" {
		// Delete conversation
		err := manager.DeleteConversation(ctx, config.DeleteConversation, config.DeleteCascade)
		if err != nil {
			return fmt.Errorf("failed to delete conversation: %w", err)
		}
		fmt.Printf("Successfully deleted conversation %s\n", config.DeleteConversation)
		if config.DeleteCascade {
			fmt.Println("All child conversations were also deleted.")
		}
		return nil
	}

	if config.PrintConversation != "" {
		// Get conversation
		conv, err := manager.GetConversation(ctx, config.PrintConversation)
		if err != nil {
			return fmt.Errorf("failed to get conversation: %w", err)
		}

		// Print conversation details
		fmt.Printf("ID: %s\n", conv.ID)
		if conv.ParentID.Valid {
			fmt.Printf("Parent ID: %s\n", conv.ParentID.String)
		}
		fmt.Printf("Model: %s\n", conv.Model)
		fmt.Printf("Created At: %s\n", conv.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("Message: %s\n", conv.UserMessage)
		return nil
	}

	return nil
}

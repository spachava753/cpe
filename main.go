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
		Version:            config.Version,
		Continue:           config.Continue,
		ListConversations:  config.ListConversations,
		DeleteConversation: config.DeleteConversation,
		DeleteCascade:      config.DeleteCascade,
		PrintConversation:  config.PrintConversation,
		New:                config.New,
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

	// Get model config to validate input types
	modelConfig, ok := agent.ModelConfigs[config.Model]
	if !ok {
		// For unknown models, default to text only
		modelConfig = agent.ModelConfig{
			Name:            config.Model,
			IsKnown:         false,
			SupportedInputs: []agent.InputType{agent.InputTypeText},
		}
	}

	// Validate input types against model capabilities
	for _, input := range input {
		supported := false
		for _, t := range modelConfig.SupportedInputs {
			if t == input.Type {
				supported = true
				break
			}
		}
		if !supported {
			slog.Error("model does not support input type",
				slog.String("model", config.Model),
				slog.String("input_type", string(input.Type)),
				slog.String("file", input.FilePath),
			)
			os.Exit(1)
		}
	}

	if err := executor.Execute(input); err != nil {
		slog.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}
}

func parseConfig() (cliopts.Options, error) {
	cliopts.ParseFlags()

	if cliopts.Opts.Version {
		log.Printf("cpe version %s\n", getVersion())
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

func readInput(inputFlag bool) ([]agent.Input, error) {
	var inputs []agent.Input

	// Check if there is any input from stdin by checking if stdin is a pipe or redirection
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Stdin has data available
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("error reading from stdin: %w", err)
		}
		if len(content) > 0 {
			inputs = append(inputs, agent.Input{
				Type: agent.InputTypeText,
				Text: string(content),
			})
		}
	}

	// Check if there are input files from command line arguments
	if inputFlag {
		args := cliopts.Opts.Args
		if len(args) < 1 {
			return nil, fmt.Errorf("when using -input flag, need at least one input file")
		}
		// All arguments are treated as input files, except the last one if it's not a file
		lastIdx := len(args)
		lastArg := args[lastIdx-1]
		if _, err := os.Stat(lastArg); err != nil {
			// Last argument doesn't exist as a file, treat it as prompt text
			lastIdx--
			inputs = append(inputs, agent.Input{
				Type: agent.InputTypeText,
				Text: lastArg,
			})
		}
		// Process all other arguments as files
		for _, path := range args[:lastIdx] {
			// We already validated file existence in ParseFlags()
			inputType, err := agent.DetectInputType(path)
			if err != nil {
				return nil, fmt.Errorf("error detecting input type for file %s: %w", path, err)
			}

			if inputType == agent.InputTypeText {
				// For text files, read the content and use it as text input
				content, err := os.ReadFile(path)
				if err != nil {
					return nil, fmt.Errorf("error reading file %s: %w", path, err)
				}
				inputs = append(inputs, agent.Input{
					Type: agent.InputTypeText,
					Text: string(content),
				})
			} else {
				// For non-text files, pass the file path
				inputs = append(inputs, agent.Input{
					Type:     inputType,
					FilePath: path,
				})
			}
		}
	} else if cliopts.Opts.Prompt != "" {
		// Without -input flag, the single argument is treated as prompt text
		inputs = append(inputs, agent.Input{
			Type: agent.InputTypeText,
			Text: cliopts.Opts.Prompt,
		})
	}

	if len(inputs) == 0 {
		return nil, fmt.Errorf("no input provided. Please provide input via stdin, input file, or as a command line argument")
	}

	return inputs, nil
}

func handleConversationCommands(config cliopts.Options) error {
	if !config.ListConversations && config.DeleteConversation == "" && config.PrintConversation == "" {
		return nil
	}

	// Initialize conversation manager
	dbPath := ".cpeconvo"
	convoManager, err := conversation.NewManager(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize conversation manager: %w", err)
	}
	defer convoManager.Close()

	// Handle conversation management commands
	if config.ListConversations {
		conversations, err := convoManager.ListConversations(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list conversations: %w", err)
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
		os.Exit(0)
	}

	if config.DeleteConversation != "" {
		if err := convoManager.DeleteConversation(context.Background(), config.DeleteConversation, config.DeleteCascade); err != nil {
			return fmt.Errorf("failed to delete conversation: %w", err)
		}
		os.Exit(0)
	}

	if config.PrintConversation != "" {
		conv, err := convoManager.GetConversation(context.Background(), config.PrintConversation)
		if err != nil {
			return fmt.Errorf("failed to get conversation: %w", err)
		}

		genConfig, err := agent.GetConfig(agent.ModelOptions{Model: config.Model})
		if err != nil {
			return fmt.Errorf("failed to get config for model %s: %w", config.Model, err)
		}
		if conv.Model != genConfig.Model {
			return fmt.Errorf("cannot print conversation for model %s: expected %s, got %s", config.Model, conv.Model, genConfig.Model)
		}

		// Print conversation metadata
		log.Printf("Conversation ID: %s\n", conv.ID)
		if conv.ParentID.Valid {
			log.Printf("Parent ID: %s\n", conv.ParentID.String)
		}
		log.Printf("Model: %s\n", conv.Model)
		log.Printf("Created At: %s\n\n", conv.CreatedAt.Format(time.RFC3339))

		// Create an executor of the appropriate type to print messages
		executor, err := agent.InitExecutor(log.Default(), agent.ModelOptions{
			Model:    config.Model,
			Continue: conv.ID,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize executor: %w", err)
		}

		// Print conversation messages
		log.Println("Messages:")
		log.Println("=========")
		log.Print(executor.PrintMessages())
		os.Exit(0)
	}

	return nil
}

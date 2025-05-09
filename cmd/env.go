package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

// envCmd represents the env command
var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Print environment variables",
	Long:  `Print all environment variables used by CPE.`,
	Run: func(cmd *cobra.Command, args []string) {
		printEnvironmentVariables()
	},
}

func init() {
	rootCmd.AddCommand(envCmd)
}

func printEnvironmentVariables() {
	fmt.Println("CPE Environment Variables:")
	fmt.Println("=========================")

	// Helper function to mask sensitive values
	maskSensitive := func(value string) string {
		if value == "" {
			return "(not set)"
		}
		if len(value) <= 8 {
			return "********"
		}
		return value[:4] + "..." + value[len(value)-4:]
	}

	// Helper function to print environment variable
	printVar := func(name, description string, sensitive bool) {
		value := os.Getenv(name)
		displayValue := value
		if sensitive && value != "" {
			displayValue = maskSensitive(value)
		}
		if value == "" {
			displayValue = "(not set)"
		}
		fmt.Printf("  %-24s - %s\n    Value: %s\n\n", name, description, displayValue)
	}

	// API Keys
	fmt.Println("\nAPI Keys:")
	printVar("ANTHROPIC_API_KEY", "Required for Claude models", true)
	printVar("OPENAI_API_KEY", "Required for OpenAI models", true)
	printVar("GEMINI_API_KEY", "Required for Google Gemini models", true)

	// Model Selection
	fmt.Println("\nModel Selection:")
	printVar("CPE_MODEL", "Specify the model to use (overridden by -model flag)", false)

	// Custom API Endpoints
	fmt.Println("\nCustom API Endpoints:")
	printVar("CPE_CUSTOM_URL", "Default custom URL for API endpoints", false)
}

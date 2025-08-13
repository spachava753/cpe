package cmd

import (
	"fmt"
	"os"

	"github.com/spachava753/cpe/internal/modelcatalog"
	"github.com/spf13/cobra"
)

var modelCmd = &cobra.Command{
	Use:     "model",
	Short:   "Manage LLM models",
	Long:    `Show and interact with models defined in a JSON catalog via --model-catalog.`,
	Aliases: []string{"models"},
}

var listModelCmd = &cobra.Command{
	Use:     "list",
	Short:   "List models from catalog",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		if modelCatalogPath == "" {
			return nil
		}
		models, err := modelcatalog.Load(modelCatalogPath)
		if err != nil {
			return err
		}
		for _, m := range models {
			line := m.Name
			if DefaultModel != "" && m.Name == DefaultModel {
				line += " (default)"
			}
			fmt.Println(line)
		}
		return nil
	},
}

var infoModelCmd = &cobra.Command{
	Use:   "info",
	Short: "Show model details by name",
	Example: `# Show model details by name
cpe model init --model-catalog gpt5
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if modelCatalogPath == "" {
			return nil
		}
		models, err := modelcatalog.Load(modelCatalogPath)
		if err != nil {
			return err
		}
		if len(args) != 1 {
			return cmd.Usage()
		}
		name := args[0]
		if name == "" {
			name = os.Getenv("CPE_MODEL")
		}
		if name == "" {
			return fmt.Errorf("no model name provided")
		}
		for _, m := range models {
			if m.Name == name {
				fmt.Printf("Name: %s\nType: %s\nID: %s\nContext: %d\nMaxOutput: %d\nInputCostPerMillion: %.6f\nOutputCostPerMillion: %.6f\nSupportsReasoning: %t\nDefaultReasoningEffort: %s\n",
					m.Name, m.Type, m.ID, m.ContextWindow, m.MaxOutput, m.InputCostPerMillion, m.OutputCostPerMillion, m.SupportsReasoning, m.DefaultReasoningEffort,
				)
				return nil
			}
		}
		return fmt.Errorf("model %q not found", name)
	},
}

func init() {
	modelCmd.AddCommand(listModelCmd)
	modelCmd.AddCommand(infoModelCmd)
	rootCmd.AddCommand(modelCmd)
}

package cli

import (
	"context"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/skills"
)

func skillsUpdateCmd(parent *cobra.Command, manager *skills.Manager, run func(context.Context, *skills.Manager, skills.UpdateOptions, commandio.IO) error) {
	var asJSON bool
	c := &cobra.Command{
		Use:   "update [SKILL...]",
		Short: "Update installed agent skills",
		Long: dedent.Dedent(`
		Update installed skills from the Git sources recorded in their lock file.
		Without names, update all installed skills in project and global scope.
		--scope restricts updates to that scope; --dir selects only that directory.

		Named skills found in both scopes are skipped with a warning; use --scope
		to choose one. Missing skills and skills absent from the lock file are
		also warned about and skipped.

		Updates replace the complete skill bundle, including local edits, and
		remove obsolete files. Unsupported recorded sources are skipped.
		Output is a JSON array when stdout is not a terminal or --json is set.
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			so, err := scopeOptsFromFlags(cmd.Flags())
			if err != nil {
				return err
			}
			return run(
				cmd.Context(),
				manager,
				skills.UpdateOptions{
					Scope:  so,
					Skills: args,
					JSON:   asJSON,
				},
				commandio.IO{
					In:  cmd.InOrStdin(),
					Out: cmd.OutOrStdout(),
					Err: cmd.ErrOrStderr(),
				},
			)
		},
	}
	c.Flags().BoolVar(
		&asJSON,
		"json",
		false,
		"Output a JSON array (automatic when stdout is not a terminal)",
	)
	parent.AddCommand(c)
}

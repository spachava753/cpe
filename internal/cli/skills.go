package cli

import (
	"context"
	"fmt"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/skills"
)

// Keep command adapters testable without running skill operations.
type skillActions struct {
	install func(context.Context, *skills.Manager, skills.InstallOptions, commandio.IO) error
	list    func(context.Context, *skills.Manager, skills.ListOptions, commandio.IO) error
	remove  func(context.Context, *skills.Manager, skills.RemoveOptions, commandio.IO) error
	update  func(context.Context, *skills.Manager, skills.UpdateOptions, commandio.IO) error
}

func skillsCmd(parent *cobra.Command, manager *skills.Manager) {
	skillsCmdWithActions(
		parent,
		manager,
		skillActions{
			install: skills.Install,
			list:    skills.List,
			remove:  skills.Remove,
			update:  skills.Update,
		},
	)
}

func skillsCmdWithActions(parent *cobra.Command, manager *skills.Manager, actions skillActions) {
	c := &cobra.Command{
		Use:   "skills",
		Short: "Manage agent skills",
		Long: dedent.Dedent(`
		Install, update, list, and remove agent skills from Git sources.

		The default skill directories are ~/.agents/skills for global scope and
		./.agents/skills for project scope.

		--dir selects an exact custom skill directory; .agents/skills is not
		appended. Only that directory is selected, even when scope is omitted.
		--dir cannot be combined with --scope global. Its lock file is
		<directory>/skills-lock.json, separate from the default project inventory.

		Result output is a styled table on a terminal and a JSON array otherwise.
		--json forces JSON output, including when stdout is a terminal.
		`),
	}
	c.PersistentFlags().String(
		scopeFlag,
		"",
		"Skill scope: project or global; required for install",
	)
	c.PersistentFlags().String(
		dirFlag,
		"",
		"Exact custom skill directory; selects only this directory (not with --scope global)",
	)
	parent.AddCommand(c)
	skillsInstallCmd(
		c,
		manager,
		actions.install,
	)
	skillsListCmd(
		c,
		manager,
		actions.list,
	)
	skillsRemoveCmd(
		c,
		manager,
		actions.remove,
	)
	skillsUpdateCmd(
		c,
		manager,
		actions.update,
	)
}

func skillsInstallCmd(parent *cobra.Command, manager *skills.Manager, run func(context.Context, *skills.Manager, skills.InstallOptions, commandio.IO) error) {
	var asJSON bool
	c := &cobra.Command{
		Use: "install SOURCE",
		Aliases: []string{
			"i",
			"add",
		},
		Short: "Install agent skills",
		Args:  cobra.ExactArgs(1),
		Long: dedent.Dedent(`
		Install agent skills from a Git source. GitHub repositories accept the
		<owner>/<repo> shorthand; other Git sources require a full URL.
		Without --skill or --path, install all skills found in the source repository.
		Repeat --skill to select specific skills by name. Ambiguous names report the
		matching source directories; select one with --path instead.
		--path and --skill are mutually exclusive.

		--path selects one exact source-relative skill directory, not SKILL.md.
		Use forward-slash separators, or --path . for a skill at the source root.
		The selected directory is read directly, regardless of discovery depth.

		--scope is required. Global selects ~/.agents/skills; project selects
		./.agents/skills. With --scope project, --dir instead selects the exact
		custom skill directory.

		Existing skills are preserved unless --override is set. With --override,
		replace the complete selected skill directory, removing obsolete files.

		Installations are recorded in the scope's lock file for skills update.

		Search up to five directory levels by default. --full-depth removes
		that depth limit.

		Output is a JSON array when stdout is not a terminal or --json is set.
		Each object has name, description, and path to its SKILL.md.
		`),
		Example: dedent.Dedent(fmt.Sprintf(
			`
        %[1]s install vercel-labs/find-skill --scope global
        %[1]s install https://github.enterprise.com/my-company/custom-skills --scope project
        %[1]s i mattpocock/skills --skill grill-with-docs --skill write-a-skill --scope project
        %[1]s i ggprompts/tfe --skill bubbletea --scope project
        %[1]s i shadcn-ui/ui --skill shadcn --scope project --dir ./custom/dir
        %[1]s i owner/repo --path variants/react/frontend-design --scope project
		`,
			parent.CommandPath(),
		)),
	}
	var skillArgs []string
	c.Flags().StringArrayVar(
		&skillArgs,
		"skill",
		nil,
		"Skill to install (can be specified multiple times)",
	)
	var sourcePath string
	c.Flags().StringVar(
		&sourcePath,
		"path",
		"",
		"Exact source-relative skill directory (exclusive with --skill; . selects the source root)",
	)
	c.MarkFlagsMutuallyExclusive(
		"skill",
		"path",
	)
	var override bool
	c.Flags().BoolVar(
		&override,
		"override",
		false,
		"Replace existing skill directories with the fetched bundles",
	)
	var fullDepth bool
	c.Flags().BoolVar(
		&fullDepth,
		"full-depth",
		false,
		"Scan full depth of source for skills",
	)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		so, err := scopeOptsFromFlags(cmd.Flags())
		if err != nil {
			return err
		}
		if !so.Project && !so.Global {
			return fmt.Errorf("--scope is required for install")
		}
		var selectedPath *string
		if cmd.Flags().Changed("path") {
			selectedPath = &sourcePath
		}
		return run(
			cmd.Context(),
			manager,
			skills.InstallOptions{
				Scope:     so,
				Source:    args[0],
				Skills:    skillArgs,
				Path:      selectedPath,
				Override:  override,
				FullDepth: fullDepth,
				JSON:      asJSON,
			},
			commandio.IO{
				In:  cmd.InOrStdin(),
				Out: cmd.OutOrStdout(),
				Err: cmd.ErrOrStderr(),
			},
		)
	}
	c.Flags().BoolVar(
		&asJSON,
		"json",
		false,
		"Output a JSON array (automatic when stdout is not a terminal)",
	)
	parent.AddCommand(c)
}

func skillsListCmd(parent *cobra.Command, manager *skills.Manager, run func(context.Context, *skills.Manager, skills.ListOptions, commandio.IO) error) {
	var asJSON bool
	c := &cobra.Command{
		Use: "list [SOURCE]",
		Aliases: []string{
			"ls",
		},
		Short: "List agent skills",
		Long: dedent.Dedent(`
		List installed skills, including skills copied manually or by another tool.
		With neither --scope nor --dir, list both global and project directories.
		--scope restricts inventory to that scope; --dir selects only the exact
		custom skill directory.

		With SOURCE, list available skills in that Git source instead of installed
		skills. Scope and directory flags select installed inventory only.

		Model invocation shows whether a skill can be invoked by the model.
		Skills with disable-model-invocation: true are user-only.
		Output is a JSON array when stdout is not a terminal or --json is set.
		Each object has name, description, path to its SKILL.md, and
		disable-model-invocation.
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			so, err := scopeOptsFromFlags(cmd.Flags())
			if err != nil {
				return err
			}
			var source *string
			if len(args) == 1 {
				source = &args[0]
			}
			return run(
				cmd.Context(),
				manager,
				skills.ListOptions{
					Scope:  so,
					Source: source,
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

func skillsUpdateCmd(parent *cobra.Command, manager *skills.Manager, run func(context.Context, *skills.Manager, skills.UpdateOptions, commandio.IO) error) {
	var asJSON, override bool
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

		Installed contents must match the lock hash; mismatches are warned about
		and skipped before fetching. --override permits replacing those bundles.
		Updates replace complete bundles and remove obsolete files. Each eligible
		Git source is fetched once. Unsupported recorded sources are skipped.
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
					Scope:    so,
					Skills:   args,
					Override: override,
					JSON:     asJSON,
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
	c.Flags().BoolVar(
		&override,
		"override",
		false,
		"Replace skills whose installed contents do not match the lock hash",
	)
	parent.AddCommand(c)
}

func skillsRemoveCmd(parent *cobra.Command, manager *skills.Manager, run func(context.Context, *skills.Manager, skills.RemoveOptions, commandio.IO) error) {
	var asJSON bool
	c := &cobra.Command{
		Use: "remove SKILL...",
		Aliases: []string{
			"rm",
		},
		Short: "Remove agent skills",
		Long: dedent.Dedent(`
		Remove named installed skills, including manually copied skills.
		Damaged skill definitions can be removed without requiring valid frontmatter.
		If a lock record exists, it is removed with the bundle.

		Without --scope or --dir, find each name in the global and project
		directories. If a name exists in both, --scope is required to choose one.
		--dir selects only that exact custom directory, so no other directory is
		searched or modified.

		Output is a JSON array when stdout is not a terminal or --json is set.
		Each object has name, description, and the removed SKILL.md path.
		Unreadable frontmatter falls back to the directory name and an empty
		description; it does not block removal.
		`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			so, err := scopeOptsFromFlags(cmd.Flags())
			if err != nil {
				return err
			}
			return run(
				cmd.Context(),
				manager,
				skills.RemoveOptions{
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

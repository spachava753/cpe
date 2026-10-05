package cli

import (
	"context"
	"fmt"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/spachava753/cpe/internal/skills"
)

const (
	scopeFlag    = "scope"
	dirFlag      = "dir"
	projectScope = "project"
	globalScope  = "global"
)

// Keep command adapters testable without running skill operations.
type skillActions struct {
	install func(context.Context, *skills.Manager, skills.InstallOptions, skills.IO) error
	list    func(context.Context, *skills.Manager, skills.ListOptions, skills.IO) error
	remove  func(context.Context, *skills.Manager, skills.RemoveOptions, skills.IO) error
}

func scopeOptsFromFlags(p *pflag.FlagSet) (skills.ScopeOptions, error) {
	scope, err := p.GetString(scopeFlag)
	if err != nil {
		return skills.ScopeOptions{}, err
	}
	if (p.Changed(scopeFlag) && scope == "") || (scope != "" && scope != projectScope && scope != globalScope) {
		return skills.ScopeOptions{}, fmt.Errorf("--scope must be project or global")
	}

	var dir *string
	if p.Changed(dirFlag) {
		d, err := p.GetString(dirFlag)
		if err != nil {
			return skills.ScopeOptions{}, err
		}
		if d == "" {
			return skills.ScopeOptions{}, fmt.Errorf("--dir must not be empty")
		}
		if scope == globalScope {
			return skills.ScopeOptions{}, fmt.Errorf("--dir cannot be combined with --scope global")
		}
		dir = &d
	}

	return skills.ScopeOptions{
		Directory: dir,
		Project:   scope == projectScope,
		Global:    scope == globalScope,
	}, nil
}

func skillsCmd(parent *cobra.Command, manager *skills.Manager) {
	skillsCmdWithActions(
		parent,
		manager,
		skillActions{
			install: skills.Install,
			list:    skills.List,
			remove:  skills.Remove,
		},
	)
}

func skillsCmdWithActions(parent *cobra.Command, manager *skills.Manager, actions skillActions) {
	c := &cobra.Command{
		Use:   "skills",
		Short: "Manage agent skills",
		Long: dedent.Dedent(`
		Install, list, and remove agent skills from Git sources.

		The default skill directories are ~/.agents/skills for global scope and
		./.agents/skills for project scope.

		--dir selects an exact custom skill directory; .agents/skills is not
		appended. Only that directory is selected, even when scope is omitted.
		--dir cannot be combined with --scope global.
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
}

func skillsInstallCmd(parent *cobra.Command, manager *skills.Manager, run func(context.Context, *skills.Manager, skills.InstallOptions, skills.IO) error) {
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

		Search up to five directory levels by default. --full-depth removes
		that depth limit.
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
				JSON:      false,
			},
			skills.IO{
				In:  cmd.InOrStdin(),
				Out: cmd.OutOrStdout(),
				Err: cmd.ErrOrStderr(),
			},
		)
	}
	parent.AddCommand(c)
}

func skillsListCmd(parent *cobra.Command, manager *skills.Manager, run func(context.Context, *skills.Manager, skills.ListOptions, skills.IO) error) {
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
					JSON:   false,
				},
				skills.IO{
					In:  cmd.InOrStdin(),
					Out: cmd.OutOrStdout(),
					Err: cmd.ErrOrStderr(),
				},
			)
		},
	}
	parent.AddCommand(c)
}

func skillsRemoveCmd(parent *cobra.Command, manager *skills.Manager, run func(context.Context, *skills.Manager, skills.RemoveOptions, skills.IO) error) {
	c := &cobra.Command{
		Use: "remove SKILL...",
		Aliases: []string{
			"rm",
		},
		Short: "Remove agent skills",
		Long: dedent.Dedent(`
		Remove named installed skills, including manually copied skills.
		Damaged skill definitions can be removed without parsing their frontmatter.

		Without --scope or --dir, find each name in the global and project
		directories. If a name exists in both, --scope is required to choose one.
		--dir selects only that exact custom directory, so no other directory is
		searched or modified.
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
					JSON:   false,
				},
				skills.IO{
					In:  cmd.InOrStdin(),
					Out: cmd.OutOrStdout(),
					Err: cmd.ErrOrStderr(),
				},
			)
		},
	}
	parent.AddCommand(c)
}

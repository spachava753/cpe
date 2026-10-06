package cli

import (
	"context"
	"fmt"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/spachava753/cpe/internal/plugins"
)

// Keep command adapters testable without running plugin operations.
type pluginActions struct {
	install func(context.Context, *plugins.Manager, plugins.InstallOptions, plugins.IO) error
	list    func(context.Context, *plugins.Manager, plugins.ListOptions, plugins.IO) error
	remove  func(context.Context, *plugins.Manager, plugins.RemoveOptions, plugins.IO) error
}

func pluginScopeOptsFromFlags(p *pflag.FlagSet) (plugins.ScopeOptions, error) {
	scope, err := p.GetString(scopeFlag)
	if err != nil {
		return plugins.ScopeOptions{}, err
	}
	if (p.Changed(scopeFlag) && scope == "") || (scope != "" && scope != projectScope && scope != globalScope) {
		return plugins.ScopeOptions{}, fmt.Errorf("--scope must be project or global")
	}

	var dir *string
	if p.Changed(dirFlag) {
		d, err := p.GetString(dirFlag)
		if err != nil {
			return plugins.ScopeOptions{}, err
		}
		if d == "" {
			return plugins.ScopeOptions{}, fmt.Errorf("--dir must not be empty")
		}
		if scope == globalScope {
			return plugins.ScopeOptions{}, fmt.Errorf("--dir cannot be combined with --scope global")
		}
		dir = &d
	}

	return plugins.ScopeOptions{
		Directory: dir,
		Project:   scope == projectScope,
		Global:    scope == globalScope,
	}, nil
}

func pluginsCmd(parent *cobra.Command, manager *plugins.Manager) {
	pluginsCmdWithActions(
		parent,
		manager,
		pluginActions{
			install: plugins.Install,
			list:    plugins.List,
			remove:  plugins.Remove,
		},
	)
}

func pluginsCmdWithActions(parent *cobra.Command, manager *plugins.Manager, actions pluginActions) {
	c := &cobra.Command{
		Use:   "plugins",
		Short: "Manage agent plugins",
		Long: dedent.Dedent(`
		Install, list, and remove agent plugins from Git sources.

		The default plugin directories are ~/.agents/plugins for global scope and
		./.agents/plugins for project scope.

		--dir selects an exact custom plugin directory; .agents/plugins is not
		appended. Only that directory is selected, even when scope is omitted.
		--dir cannot be combined with --scope global.

		Result output is a styled table on a terminal and a JSON array otherwise.
		--json forces JSON output, including when stdout is a terminal.
		`),
	}
	c.PersistentFlags().String(
		scopeFlag,
		"",
		"Plugin scope: project or global; required for install",
	)
	c.PersistentFlags().String(
		dirFlag,
		"",
		"Exact custom plugin directory; selects only this directory (not with --scope global)",
	)
	parent.AddCommand(c)
	pluginsInstallCmd(
		c,
		manager,
		actions.install,
	)
	pluginsListCmd(
		c,
		manager,
		actions.list,
	)
	pluginsRemoveCmd(
		c,
		manager,
		actions.remove,
	)
}

func pluginsInstallCmd(parent *cobra.Command, manager *plugins.Manager, run func(context.Context, *plugins.Manager, plugins.InstallOptions, plugins.IO) error) {
	var asJSON bool
	c := &cobra.Command{
		Use: "install SOURCE",
		Aliases: []string{
			"i",
			"add",
		},
		Short: "Install agent plugins",
		Args:  cobra.ExactArgs(1),
		Long: dedent.Dedent(`
		Install agent plugins from a Git source. GitHub repositories accept the
		<owner>/<repo> shorthand; other Git sources require a full URL.
		Without --plugin or --path, install all plugins found in the source repository.
		Repeat --plugin to select specific plugins by name. Ambiguous names report the
		matching source directories; select one with --path instead.
		--path and --plugin are mutually exclusive.

		--path selects one exact source-relative plugin directory, not plugin.json.
		Use forward-slash separators, or --path . for a plugin at the source root.
		Only native Agent Plugins 1.0.0 packages are supported. Installing does not
		activate MCP servers or execute bundled code.
		The selected directory is read directly, regardless of discovery depth.

		--scope is required. Global selects ~/.agents/plugins; project selects
		./.agents/plugins. With --scope project, --dir instead selects the exact
		custom plugin directory.

		Existing plugins are preserved unless --override is set. With --override,
		replace the complete selected plugin directory, removing obsolete files.

		Search up to five directory levels by default. --full-depth removes
		that depth limit.

		Output is a JSON array when stdout is not a terminal or --json is set.
		Each object has name, description, and path to its package directory.
		`),
		Example: dedent.Dedent(fmt.Sprintf(
			`
        %[1]s install stripe/ai --path providers/agent-plugins/plugin --scope project
        %[1]s install stripe/ai --plugin stripe --scope global
        %[1]s install https://github.enterprise.com/my-company/custom-plugins --scope project
        %[1]s i owner/repo --path . --scope project --dir ./custom/dir
		`,
			parent.CommandPath(),
		)),
	}
	var pluginArgs []string
	c.Flags().StringArrayVar(
		&pluginArgs,
		"plugin",
		nil,
		"Plugin to install (can be specified multiple times)",
	)
	var sourcePath string
	c.Flags().StringVar(
		&sourcePath,
		"path",
		"",
		"Exact source-relative plugin directory (exclusive with --plugin; . selects the source root)",
	)
	c.MarkFlagsMutuallyExclusive(
		"plugin",
		"path",
	)
	var override bool
	c.Flags().BoolVar(
		&override,
		"override",
		false,
		"Replace existing plugin directories with the fetched bundles",
	)
	var fullDepth bool
	c.Flags().BoolVar(
		&fullDepth,
		"full-depth",
		false,
		"Scan full depth of source for plugins",
	)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		so, err := pluginScopeOptsFromFlags(cmd.Flags())
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
			plugins.InstallOptions{
				Scope:     so,
				Source:    args[0],
				Plugins:   pluginArgs,
				Path:      selectedPath,
				Override:  override,
				FullDepth: fullDepth,
				JSON:      asJSON,
			},
			plugins.IO{
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

func pluginsListCmd(parent *cobra.Command, manager *plugins.Manager, run func(context.Context, *plugins.Manager, plugins.ListOptions, plugins.IO) error) {
	var asJSON bool
	c := &cobra.Command{
		Use: "list [SOURCE]",
		Aliases: []string{
			"ls",
		},
		Short: "List agent plugins",
		Long: dedent.Dedent(`
		List installed plugins, including plugins copied manually or by another tool.
		With neither --scope nor --dir, list both global and project directories.
		--scope restricts inventory to that scope; --dir selects only the exact
		custom plugin directory.

		With SOURCE, list available plugins in that Git source instead of installed
		plugins. Scope and directory flags select installed inventory only.

		Output is a JSON array when stdout is not a terminal or --json is set.
		Each object has name, description, and path to its package directory.
		Component diagnostics are written separately to stderr.
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			so, err := pluginScopeOptsFromFlags(cmd.Flags())
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
				plugins.ListOptions{
					Scope:  so,
					Source: source,
					JSON:   asJSON,
				},
				plugins.IO{
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

func pluginsRemoveCmd(parent *cobra.Command, manager *plugins.Manager, run func(context.Context, *plugins.Manager, plugins.RemoveOptions, plugins.IO) error) {
	var asJSON bool
	c := &cobra.Command{
		Use: "remove PLUGIN...",
		Aliases: []string{
			"rm",
		},
		Short: "Remove agent plugins",
		Long: dedent.Dedent(`
		Remove named installed plugins, including manually copied plugins.
		Damaged packages can be removed without requiring a valid manifest.

		Without --scope or --dir, find each name in the global and project
		directories. If a name exists in both, --scope is required to choose one.
		--dir selects only that exact custom directory, so no other directory is
		searched or modified.

		Output is a JSON array when stdout is not a terminal or --json is set.
		Each object has name, description, and the removed package directory path.
		Unreadable manifests fall back to the directory name and an empty
		description; it does not block removal.
		`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			so, err := pluginScopeOptsFromFlags(cmd.Flags())
			if err != nil {
				return err
			}
			return run(
				cmd.Context(),
				manager,
				plugins.RemoveOptions{
					Scope:   so,
					Plugins: args,
					JSON:    asJSON,
				},
				plugins.IO{
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

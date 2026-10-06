package cli

import (
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/skills"
)

func Root(version string, skillManager *skills.Manager, pluginManager *plugins.Manager) *cobra.Command {
	r := &cobra.Command{
		Use:     "cpe",
		Short:   "Another ACP agent server that comes with builtin TUI",
		Version: version,
	}
	skillsCmd(
		r,
		skillManager,
	)
	pluginsCmd(
		r,
		pluginManager,
	)
	return r
}

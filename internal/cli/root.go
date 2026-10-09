package cli

import (
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/skills"
)

// Root builds CPE's command tree with the supplied dependencies and authentication configuration.
func Root(version string, skillManager *skills.Manager, pluginManager *plugins.Manager, authStore *auth.Store, authOptions auth.LoginOptions) *cobra.Command {
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
	authCmd(
		r,
		authStore,
		authOptions,
	)
	return r
}

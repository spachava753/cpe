package cli

import (
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/skills"
)

func Root(version string, manager *skills.Manager) *cobra.Command {
	r := &cobra.Command{
		Use:     "cpe",
		Short:   "Another ACP agent server that comes with builtin TUI",
		Version: version,
	}
	skillsCmd(r, manager)
	return r
}

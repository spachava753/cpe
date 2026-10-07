package cli

import (
	"fmt"

	"github.com/spf13/pflag"

	"github.com/spachava753/cpe/internal/commandio"
)

const (
	scopeFlag    = "scope"
	dirFlag      = "dir"
	projectScope = "project"
	globalScope  = "global"
)

// Parse only Cobra's shared scope flags here. Domain actions resolve their own
// default roots using lower-level command inputs; no domain imports this package.
func scopeOptsFromFlags(flags *pflag.FlagSet) (commandio.ScopeOptions, error) {
	scope, err := flags.GetString(scopeFlag)
	if err != nil {
		return commandio.ScopeOptions{}, err
	}
	if (flags.Changed(scopeFlag) && scope == "") || (scope != "" && scope != projectScope && scope != globalScope) {
		return commandio.ScopeOptions{}, fmt.Errorf("--scope must be project or global")
	}

	var directory *string
	if flags.Changed(dirFlag) {
		value, err := flags.GetString(dirFlag)
		if err != nil {
			return commandio.ScopeOptions{}, err
		}
		if value == "" {
			return commandio.ScopeOptions{}, fmt.Errorf("--dir must not be empty")
		}
		if scope == globalScope {
			return commandio.ScopeOptions{}, fmt.Errorf("--dir cannot be combined with --scope global")
		}
		directory = &value
	}

	return commandio.ScopeOptions{
		Directory: directory,
		Project:   scope == projectScope,
		Global:    scope == globalScope,
	}, nil
}

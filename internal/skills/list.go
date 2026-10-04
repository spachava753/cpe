package skills

import (
	"context"
	"encoding/json"
	"fmt"
)

// ListOptions is unparsed listing input. List owns destination resolution and
// text/JSON formatting; Manager owns source discovery and installed inventory.
type ListOptions struct {
	Scope ScopeOptions
	// Source selects available skills in a Git source instead of installed
	// inventory. Nil means omitted; non-nil preserves an explicit argument.
	// Scope applies only to installed inventory.
	Source *string
	JSON   bool
}

// List is the high-level listing entry point for a command adapter. It can
// list available source skills or installed skills, including unmanaged ones,
// and write results through streams.
func List(ctx context.Context, manager *Manager, opts ListOptions, streams IO) error {
	if opts.Source != nil {
		result, err := manager.ListSource(
			ctx,
			*opts.Source,
			DiscoveryOptions{},
		)
		if err != nil {
			return err
		}
		output := outputStream(streams)
		if opts.JSON {
			if result == nil {
				result = []Skill{}
			}
			return json.NewEncoder(output).Encode(result)
		}
		for _, skill := range result {
			if _, err := fmt.Fprintf(
				output,
				"%s\t%s\t%s\n",
				terminalText(skill.Name),
				terminalText(skill.Description),
				terminalText(skill.Path),
			); err != nil {
				return err
			}
		}
		return nil
	}
	directories, err := resolveDirectories(
		opts.Scope,
		false,
	)
	if err != nil {
		return err
	}
	var result []InstalledSkill
	for _, directory := range directories {
		installed, err := manager.ListInstalled(
			ctx,
			directory,
		)
		if err != nil {
			return err
		}
		result = append(
			result,
			installed...,
		)
	}
	return writeInstalled(
		streams,
		opts.JSON,
		result,
	)
}

package skills

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/spachava753/cpe/internal/terminaltext"
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

type listedSkill struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	Path               string `json:"path"`
	InvocationDisabled bool   `json:"disable-model-invocation"`
}

// List lists available source skills or installed skills, including manually
// copied ones. JSON exposes skill summaries; warnings are written separately.
func List(ctx context.Context, manager *Manager, opts ListOptions, streams IO) error {
	var result []Skill
	if opts.Source != nil {
		available, err := manager.ListSource(
			ctx,
			*opts.Source,
			DiscoveryOptions{},
		)
		if err != nil {
			return err
		}
		result = available
	} else {
		directories, err := resolveDirectories(
			opts.Scope,
			false,
		)
		if err != nil {
			return err
		}
		for _, directory := range directories {
			installed, err := manager.ListInstalled(
				ctx,
				directory,
			)
			if err != nil {
				return err
			}
			for _, entry := range installed {
				result = append(
					result,
					entry.Skill,
				)
			}
		}
	}
	var violationErr error
	for _, skill := range result {
		if violationErr = writeViolations(
			streams,
			skill,
		); violationErr != nil {
			break
		}
	}
	output := outputStream(streams)
	if jsonOutput(
		output,
		opts.JSON,
	) {
		entries := make(
			[]listedSkill,
			0,
			len(result),
		)
		for _, skill := range result {
			entries = append(
				entries,
				listedSkill{
					Name:               skill.Name,
					Description:        skill.Description,
					Path:               skill.Path,
					InvocationDisabled: skill.InvocationDisabled,
				},
			)
		}
		return errors.Join(
			violationErr,
			json.NewEncoder(output).Encode(entries),
		)
	}
	var rows [][]string
	for _, skill := range result {
		invocation := "Enabled"
		if skill.InvocationDisabled {
			invocation = "Disabled"
		}
		location := skill.Path
		if opts.Source == nil {
			location = filepath.Dir(location)
		}
		rows = append(
			rows,
			[]string{
				strings.TrimSpace(terminaltext.Sanitize(skill.Name)),
				strings.TrimSpace(terminaltext.Sanitize(skill.Description)),
				invocation,
				terminaltext.Sanitize(location),
			},
		)
	}
	return errors.Join(
		violationErr,
		writeTable(
			output,
			[]string{
				"Skill",
				"Description",
				"Model invocation",
				locationColumn,
			},
			rows,
		),
	)
}

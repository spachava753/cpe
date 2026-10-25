package skills

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/terminaltext"
)

// ListOptions selects installed or source skills and their output format.
type ListOptions struct {
	// Source selects source discovery when non-nil, ignoring Scope.
	Source *string
	// Scope selects installed roots; its zero value lists project and global roots.
	Scope commandio.ScopeOptions
	// JSON forces JSON results even when output is a terminal.
	JSON bool
}

type listedSkill struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	Path               string `json:"path"`
	InvocationDisabled bool   `json:"disable-model-invocation"`
}

// List writes source or installed skill summaries to streams.Out and warnings
// to streams.Err. JSON includes name, description, SKILL.md path, and
// disable-model-invocation; terminal output also shows bundle locations.
func List(ctx context.Context, manager *Manager, opts ListOptions, streams commandio.IO) error {
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
		directories, err := opts.Scope.Directories(
			"skills",
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
	output := streams.Output()
	if commandio.UseJSON(
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
		commandio.WriteTable(
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

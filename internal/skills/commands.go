package skills

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/terminaltext"
)

const locationColumn = "Location"

type skillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

func writeViolations(streams commandio.IO, skill Skill) error {
	if len(skill.Violations) == 0 {
		return nil
	}
	label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Yellow).Render("Warning:")
	for _, violation := range skill.Violations {
		if _, err := lipgloss.Fprintln(
			streams.ErrorOutput(),
			label+" "+terminaltext.Sanitize(skill.Name)+": "+terminaltext.Sanitize(violation),
		); err != nil {
			return err
		}
	}
	return nil
}

func writeInstalled(streams commandio.IO, asJSON bool, result []InstalledSkill) error {
	var violationErr error
	for _, entry := range result {
		if violationErr = writeViolations(
			streams,
			entry.Skill,
		); violationErr != nil {
			break
		}
	}
	output := streams.Output()
	if commandio.UseJSON(
		output,
		asJSON,
	) {
		summaries := make(
			[]skillSummary,
			0,
			len(result),
		)
		for _, entry := range result {
			summaries = append(
				summaries,
				skillSummary{
					Name:        entry.Skill.Name,
					Description: entry.Skill.Description,
					Path:        entry.Skill.Path,
				},
			)
		}
		return errors.Join(
			violationErr,
			json.NewEncoder(output).Encode(summaries),
		)
	}
	var rows [][]string
	for _, entry := range result {
		rows = append(
			rows,
			[]string{
				strings.TrimSpace(terminaltext.Sanitize(entry.Skill.Name)),
				strings.TrimSpace(terminaltext.Sanitize(entry.Skill.Description)),
				terminaltext.Sanitize(entry.Directory),
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
				locationColumn,
			},
			rows,
		),
	)
}

func writeRemoved(streams commandio.IO, asJSON bool, result []RemovedSkill) error {
	output := streams.Output()
	if commandio.UseJSON(
		output,
		asJSON,
	) {
		summaries := make(
			[]skillSummary,
			0,
			len(result),
		)
		for _, entry := range result {
			summaries = append(
				summaries,
				skillSummary(entry),
			)
		}
		return json.NewEncoder(output).Encode(summaries)
	}
	var rows [][]string
	for _, entry := range result {
		filename := filepath.Dir(entry.Path)
		rows = append(
			rows,
			[]string{
				strings.TrimSpace(terminaltext.Sanitize(filepath.Base(filename))),
				terminaltext.Sanitize(filename),
			},
		)
	}
	return commandio.WriteTable(
		output,
		[]string{
			"Removed",
			locationColumn,
		},
		rows,
	)
}

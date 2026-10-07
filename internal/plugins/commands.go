package plugins

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

type pluginSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

func writeDiagnostics(streams commandio.IO, plugin Plugin) error {
	label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Yellow).Render("Warning:")
	var messages []string
	for _, diagnostic := range plugin.Diagnostics {
		messages = append(
			messages,
			diagnostic.Error(),
		)
	}
	for _, skill := range plugin.Skills {
		for _, violation := range skill.Violations {
			messages = append(
				messages,
				skill.Name+": "+violation,
			)
		}
	}
	for _, message := range messages {
		if _, err := lipgloss.Fprintln(
			streams.ErrorOutput(),
			label+" "+terminaltext.Sanitize(plugin.Name)+": "+terminaltext.Sanitize(message),
		); err != nil {
			return err
		}
	}
	return nil
}

func writeInstalled(streams commandio.IO, asJSON bool, result []InstalledPlugin) error {
	var violationErr error
	for _, entry := range result {
		if violationErr = writeDiagnostics(
			streams,
			entry.Plugin,
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
			[]pluginSummary,
			0,
			len(result),
		)
		for _, entry := range result {
			summaries = append(
				summaries,
				pluginSummary{
					Name:        entry.Plugin.Name,
					Description: entry.Plugin.Description,
					Path:        entry.Plugin.Path,
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
				strings.TrimSpace(terminaltext.Sanitize(entry.Plugin.Name)),
				strings.TrimSpace(terminaltext.Sanitize(entry.Plugin.Description)),
				terminaltext.Sanitize(entry.Directory),
			},
		)
	}
	return errors.Join(
		violationErr,
		commandio.WriteTable(
			output,
			[]string{
				"Plugin",
				"Description",
				locationColumn,
			},
			rows,
		),
	)
}

func writeRemoved(streams commandio.IO, asJSON bool, result []RemovedPlugin) error {
	output := streams.Output()
	if commandio.UseJSON(
		output,
		asJSON,
	) {
		summaries := make(
			[]pluginSummary,
			0,
			len(result),
		)
		for _, entry := range result {
			summaries = append(
				summaries,
				pluginSummary(entry),
			)
		}
		return json.NewEncoder(output).Encode(summaries)
	}
	var rows [][]string
	for _, entry := range result {
		filename := entry.Path
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

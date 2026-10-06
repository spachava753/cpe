package plugins

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/term"

	"github.com/spachava753/cpe/internal/terminaltext"
)

const locationColumn = "Location"

type pluginSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

func resolveDirectories(scope ScopeOptions, requireScope bool) ([]string, error) {
	if scope.Project && scope.Global {
		return nil, errors.New("conflicting scope selections")
	}
	if requireScope && !scope.Project && !scope.Global {
		return nil, errors.New("scope is required for install")
	}
	if scope.Directory != nil {
		if *scope.Directory == "" {
			return nil, errors.New("directory must not be empty")
		}
		if scope.Global {
			return nil, errors.New("custom directory cannot be combined with global scope")
		}
		directory := *scope.Directory
		if !filepath.IsAbs(directory) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			directory = cwd + string(filepath.Separator) + directory
		}
		return []string{
			directory,
		}, nil
	}
	var directories []string
	if scope.Project || !scope.Global {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		directories = append(
			directories,
			ProjectPath(cwd),
		)
	}
	if scope.Global || !scope.Project {
		directory, err := GlobalPath()
		if err != nil {
			return nil, err
		}
		if len(directories) == 0 || directory != directories[0] {
			directories = append(
				directories,
				directory,
			)
		}
	}
	return directories, nil
}

func outputStream(streams IO) io.Writer {
	if streams.Out != nil {
		return streams.Out
	}
	return os.Stdout
}

// Format follows the result stream, not stdin or stderr. Color settings never
// turn a redirected stream into a terminal.
func jsonOutput(output io.Writer, requested bool) bool {
	if requested {
		return true
	}
	terminal, ok := output.(interface{ Fd() uintptr })
	return !ok || !term.IsTerminal(terminal.Fd())
}

func writeDiagnostics(streams IO, plugin Plugin) error {
	output := streams.Err
	if output == nil {
		output = os.Stderr
	}
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
			output,
			label+" "+terminaltext.Sanitize(plugin.Name)+": "+terminaltext.Sanitize(message),
		); err != nil {
			return err
		}
	}
	return nil
}

func writeInstalled(streams IO, asJSON bool, result []InstalledPlugin) error {
	var violationErr error
	for _, entry := range result {
		if violationErr = writeDiagnostics(
			streams,
			entry.Plugin,
		); violationErr != nil {
			break
		}
	}
	output := outputStream(streams)
	if jsonOutput(
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
		writeTable(
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

// Lip Gloss owns layout and styling. Cells are sanitized before rendering, so
// only our own styles can introduce terminal escape sequences.
func writeTable(output io.Writer, headers []string, rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	cell := lipgloss.NewStyle().PaddingRight(2)
	view := table.New().
		Border(lipgloss.Border{}).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		BorderHeader(false).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, column int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return cell.Bold(true).Foreground(lipgloss.Cyan)
			case column == 0:
				return cell.Foreground(lipgloss.Cyan)
			case column == len(headers)-1:
				return cell.Faint(true)
			default:
				return cell
			}
		})
	if terminal, ok := output.(interface{ Fd() uintptr }); ok {
		if width, _, err := term.GetSize(terminal.Fd()); err == nil && width > 0 {
			view.Width(width)
		}
	}
	_, err := lipgloss.Fprintln(
		output,
		view.Render(),
	)
	return err
}

func writeRemoved(streams IO, asJSON bool, result []RemovedPlugin) error {
	output := outputStream(streams)
	if jsonOutput(
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
	return writeTable(
		output,
		[]string{
			"Removed",
			locationColumn,
		},
		rows,
	)
}

package skills

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const locationColumn = "Location"

type skillSummary struct {
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

func terminalText(value string) string {
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}

// displayText removes skill-controlled terminal commands before styling. Keep
// layout whitespace for Lip Gloss, which handles multiline cells and tabs.
func displayText(value string) string {
	return strings.Map(
		func(character rune) rune {
			if character != '\n' && character != '\t' && unicode.IsControl(character) {
				return -1
			}
			return character
		},
		ansi.Strip(value),
	)
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

func writeViolations(streams IO, skill Skill) error {
	if len(skill.Violations) == 0 {
		return nil
	}
	output := streams.Err
	if output == nil {
		output = os.Stderr
	}
	label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Yellow).Render("Warning:")
	for _, violation := range skill.Violations {
		if _, err := lipgloss.Fprintln(
			output,
			label+" "+displayText(skill.Name)+": "+displayText(violation),
		); err != nil {
			return err
		}
	}
	return nil
}

func writeInstalled(streams IO, asJSON bool, result []InstalledSkill) error {
	var violationErr error
	for _, entry := range result {
		if violationErr = writeViolations(
			streams,
			entry.Skill,
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
				strings.TrimSpace(displayText(entry.Skill.Name)),
				strings.TrimSpace(displayText(entry.Skill.Description)),
				displayText(entry.Directory),
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

func writeRemoved(streams IO, asJSON bool, result []RemovedSkill) error {
	output := outputStream(streams)
	if jsonOutput(
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
				strings.TrimSpace(displayText(filepath.Base(filename))),
				displayText(filename),
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

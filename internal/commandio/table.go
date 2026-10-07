package commandio

import (
	"io"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/term"
)

// WriteTable writes a styled, borderless table with the given headers and rows.
// Empty rows produce no output. Terminal writers exposing Fd use the terminal width.
// Callers must sanitize untrusted cell text before rendering. Write errors are returned.
func WriteTable(output io.Writer, headers []string, rows [][]string) error {
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

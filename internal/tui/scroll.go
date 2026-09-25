package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// textAnchor identifies the start of a rendered row within the unwrapped text.
// Cell offsets retain the same text across reflow, including wide characters.
type textAnchor struct {
	line, cell int
}

type transcriptScroll struct {
	following bool
	rows      []textAnchor
	anchor    textAnchor
	offset    int
}

// setViewportContent preserves a logical reading position, rather than a row
// number that changes with terminal width. Geometry may clamp the position but
// cannot change following: only user scrolling or explicit navigation does.
func (m *model) setViewportContent(content string, following bool) {
	anchor := m.scroll.anchor
	if row := m.viewport.YOffset(); row != m.scroll.offset && row < len(m.scroll.rows) {
		anchor = m.scroll.rows[row]
	}
	var rows []string
	var anchors []textAnchor
	for line, text := range strings.Split(content, "\n") {
		cell := 0
		for row := range strings.SplitSeq(ansi.Hardwrap(text, max(1, m.viewport.Width()), true), "\n") {
			rows = append(rows, row)
			anchors = append(anchors, textAnchor{line: line, cell: cell})
			cell += ansi.StringWidth(row)
		}
	}
	m.viewport.SetContentLines(rows)
	m.scroll = transcriptScroll{following: following, rows: anchors, anchor: anchor}
	if following {
		m.viewport.GotoBottom()
		m.scroll.offset = m.viewport.YOffset()
		m.scroll.anchor = anchors[m.scroll.offset]
		return
	}
	row := 0
	for i, position := range anchors {
		if position.line > anchor.line || position.line == anchor.line && position.cell > anchor.cell {
			break
		}
		row = i
	}
	m.viewport.SetYOffset(row)
	m.scroll.offset = m.viewport.YOffset()
	if m.scroll.offset != row {
		m.scroll.anchor = anchors[m.scroll.offset]
	}
}

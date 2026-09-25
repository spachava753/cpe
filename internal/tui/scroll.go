package tui

import (
	"fmt"
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

const toolPreviewRows = 20

// toolPreview marks a tool result's logical lines, excluding its label and
// separator. Rendering can elide rows without changing conversation anchors.
type toolPreview struct {
	start, end int
}

// setViewportContent preserves a logical reading position, rather than a row
// number that changes with terminal width. Geometry may clamp the position but
// cannot change following: only user scrolling or explicit navigation does.
func (m *model) setViewportContent(content string, following bool, previews ...toolPreview) {
	anchor := m.scroll.anchor
	if row := m.viewport.YOffset(); row != m.scroll.offset && row < len(m.scroll.rows) {
		anchor = m.scroll.rows[row]
	}
	var rows []string
	var anchors []textAnchor
	preview, shown, hidden := 0, 0, 0
	width := max(1, m.viewport.Width())
	for line, text := range strings.Split(content, "\n") {
		limited := preview < len(previews) && line >= previews[preview].start && line < previews[preview].end
		cell := 0
		for row := range strings.SplitSeq(ansi.Hardwrap(text, width, true), "\n") {
			if !limited || shown < toolPreviewRows {
				rows = append(rows, row)
				anchors = append(anchors, textAnchor{line: line, cell: cell})
			} else {
				hidden++
			}
			if limited {
				shown++
			}
			cell += ansi.StringWidth(row)
		}
		if limited && line == previews[preview].end-1 {
			if hidden > 0 {
				unit := "lines"
				if hidden == 1 {
					unit = "line"
				}
				notice := fmt.Sprintf("… %d more %s · full result in session", hidden, unit)
				rows = append(rows, m.styles.muted.Render(ansi.Truncate(notice, width, "…")))
				anchors = append(anchors, textAnchor{line: line, cell: cell})
			}
			preview++
			shown, hidden = 0, 0
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

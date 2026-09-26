package tui

// A read-only panel replaces the transcript without owning the composer or
// canceling work. Returning restores the conversation's logical scroll position.
type panelKind uint8

const (
	conversationPanel panelKind = iota
	usagePanel
	treePanel
	queuePanel
)

type panelState struct {
	kind      panelKind
	text      string
	anchor    textAnchor
	following bool
}

func (m *model) openPanel(kind panelKind, text string) {
	if m.panel.kind == conversationPanel {
		m.panel.anchor = m.scroll.anchor
		if row := m.viewport.YOffset(); row != m.scroll.offset && row < len(m.scroll.rows) {
			m.panel.anchor = m.scroll.rows[row]
		}
		m.panel.following = m.scroll.following
	}
	m.panel.kind, m.panel.text = kind, text
	m.refresh(false)
	m.viewport.GotoTop()
}

func (m *model) closePanel() {
	saved := m.panel
	m.panel = panelState{}
	m.notice = ""
	m.scroll.anchor = saved.anchor
	// The visible rows belong to the panel, not the conversation. Prevent their
	// offset from overriding the saved conversation anchor during refresh.
	m.scroll.offset = m.viewport.YOffset()
	m.refresh(saved.following)
}

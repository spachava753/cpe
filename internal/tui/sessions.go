package tui

import (
	"context"
	"fmt"
	"path/filepath"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/session"
)

func (m model) changingSession() bool {
	return m.busy && (m.operation == sessionNewCommand || m.operation == sessionResumeCommand)
}

// Session actions share the single worker boundary, so no replay races a tool,
// default save, or provider change. The caller still owns Sessions.Close.
func (m *model) startSession(request commandRequest) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel, m.steer = cancel, cancel
	m.busy, m.operation = true, request.kind
	m.activity = "Opening session"
	if request.kind == sessionListCommand {
		m.activity = "Listing sessions"
	}
	m.notice = ""
	ch := make(chan update, 1)
	m.events = ch
	sessions := m.sessions
	go func() {
		defer close(ch)
		result := update{done: true}
		if request.kind == sessionListCommand {
			result.sessions, result.err = sessions.List(ctx)
		} else {
			result.conversation, result.err = sessions.Open(ctx, agent.SessionStart{Path: request.argument})
		}
		ch <- result
	}()
	return tea.Batch(await(ch), m.spinner.Tick)
}

func (m *model) showSessions(sessions []session.Info) {
	var items []list.Item
	selected := 0
	for _, info := range sessions {
		label := fmt.Sprintf("%s · %s", info.Updated.Local().Format("2006-01-02 15:04"), filepath.Base(info.Path))
		if info.Label != "" {
			label = info.Label + " · " + label
		}
		if info.Path == m.sessionFile {
			label += currentChoiceSuffix
			selected = len(items)
		}
		items = append(items, choice{info.Path, oneline(label)})
	}
	if len(items) == 0 {
		m.notice = "No saved sessions in this directory"
		return
	}
	m.openPicker(sessionListCommand, "Choose a session · Enter resumes · Esc closes", items, selected)
}

func (m *model) acceptConversation(next *agent.Conversation) {
	if m.agent == next.Agent {
		return
	}
	m.agent, m.name = next.Agent, next.Name
	m.sessionFile, m.checkpoints = next.Agent.SessionFile(), next.Agent.Checkpoints()
	m.profile, m.profiles = next.Agent.Model(), next.Config.Models
	m.promptMode = next.Config.TUI.PromptMode
	m.pendingModel, m.interrupted = nil, nil
	m.provisional = ""
	m.panel = panelState{}
	m.scroll = transcriptScroll{following: true}
	m.newGenerator = m.sessions.Generator
	m.loginRequired = m.needsLogin != nil && m.needsLogin(m.profile)
}

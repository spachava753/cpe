package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/spachava753/cpe/internal/skills"
)

// commandRequest is parsed at submission. The empty kind is ordinary prompt
// text; skills carry their complete invocation for Agent.Prompt to resolve.
type commandKind string

const skillCommand commandKind = skills.CommandPrefix

type commandRequest struct {
	kind     commandKind
	argument string
}

func parseCommand(text string, literal bool) (commandRequest, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return commandRequest{}, errors.New("empty command")
	}
	name := fields[0]
	argument := strings.TrimSpace(strings.TrimPrefix(text, name))
	switch name {
	case modelCommand, reasoningCommand, themeCommand, loginCommand:
		return commandRequest{commandKind(name), argument}, nil
	case branchCommand:
		if argument == "" {
			return commandRequest{}, errors.New("use /branch ID")
		}
		return commandRequest{commandKind(name), argument}, nil
	case queueCommand:
		if argument != "" && argument != "clear" {
			return commandRequest{}, errors.New("use /queue or /queue clear")
		}
		return commandRequest{commandKind(name), argument}, nil
	case helpCommand, usageCommand, sessionCommand, treeCommand, compactCommand, quitCommand, exitCommand:
		if argument != "" {
			return commandRequest{}, fmt.Errorf("%s takes no arguments", name)
		}
		return commandRequest{commandKind(name), ""}, nil
	}
	if strings.HasPrefix(name, skills.CommandPrefix) {
		return commandRequest{skillCommand, text}, nil
	}
	if strings.HasPrefix(name, "/") && !literal {
		return commandRequest{}, errors.New("unknown command; /help for commands")
	}
	return commandRequest{argument: text}, nil
}

func (r commandRequest) label() string {
	if r.kind == skillCommand {
		return strings.Fields(r.argument)[0]
	}
	return string(r.kind)
}

func (m *model) submitInput() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" || m.savingDefault != nil || m.loggingIn {
		return nil
	}
	request, err := parseCommand(text, m.completion.literal)
	if err != nil {
		m.notice = "Error: " + err.Error()
		return nil
	}
	// Editing a normal draft during work still requires a later explicit submit.
	if m.busy && request.kind == "" {
		return nil
	}
	if m.loginRequired && (request.kind == "" || request.kind == skillCommand) {
		m.notice = "Sign in with /login first"
		return nil
	}
	m.input.Reset()
	m.syncCompletion()
	return m.dispatchCommand(request)
}

func (m *model) enqueueCommand(request commandRequest) tea.Cmd {
	m.queuedCommands = append(m.queuedCommands, request)
	if m.queueView {
		m.refresh(m.scroll.following)
	}
	m.notice = "Queued " + request.label() + " · /queue to inspect or /queue clear"
	return nil
}

func (m *model) dispatchCommand(request commandRequest) tea.Cmd {
	if request.kind != queueCommand {
		m.queueView = false
	}
	switch request.kind {
	case modelCommand, reasoningCommand:
		m.configureModel(string(request.kind), request.argument)
	case themeCommand:
		m.configureTheme(request.argument)
	case loginCommand:
		return m.configureLogin(request.argument)
	case quitCommand, exitCommand:
		return m.quit()
	case helpCommand:
		m.notice = "/model  /reasoning  /theme  /usage  /login  /skill:NAME [args]  /queue [clear]  /compact  /tree  /branch ID  /session  /quit"
	case usageCommand:
		m.staticView, m.usageView = "", true
		m.notice = "Session totals · PgUp/PgDn scroll"
		m.refresh(false)
		m.viewport.GotoTop()
	case sessionCommand:
		m.notice = m.sessionFile
	case treeCommand:
		var b strings.Builder
		b.WriteString("Completed checkpoints — /branch ID to continue from one\n\n")
		for _, e := range m.checkpoints {
			var data struct {
				Label string `json:"label"`
			}
			_ = json.Unmarshal(e.Data, &data)
			if e.Type == "session" {
				data.Label = "start"
			}
			fmt.Fprintf(&b, "%s  %s\n", e.ID, data.Label)
		}
		m.usageView, m.staticView = false, clean(b.String())
		m.notice = "History is preserved when branching"
		m.refresh(false)
		m.viewport.GotoTop()
	case queueCommand:
		if request.argument == "clear" {
			m.queuedCommands = nil
			if m.queueView {
				m.refresh(m.scroll.following)
			}
			m.notice = "Queued commands cleared · active work continues"
		} else {
			m.showQueue()
		}
	case compactCommand, branchCommand, skillCommand:
		if m.busy {
			return m.enqueueCommand(request)
		}
		switch request.kind {
		case compactCommand:
			return m.start(compactCommand)
		case branchCommand:
			return m.start(branchCommand + " " + request.argument)
		default:
			return m.start(request.argument)
		}
	default:
		return m.start(request.argument)
	}
	return nil
}

func (m model) queueText() string {
	var b strings.Builder
	b.WriteString("Queued commands — /queue clear to discard\n\n")
	for i, request := range m.queuedCommands {
		fmt.Fprintf(&b, "%d. %s\n", i+1, request.label())
	}
	if len(m.queuedCommands) == 0 {
		b.WriteString("No queued commands.\n")
	}
	if len(m.operationErrors) > 0 {
		b.WriteString("\nFailed operations (this run)\n\n")
		for i, message := range m.operationErrors {
			fmt.Fprintf(&b, "%d. %s\n\n", i+1, message)
		}
	}
	return clean(b.String())
}

func (m *model) showQueue() {
	m.queueView, m.usageView, m.staticView = true, false, ""
	m.notice = "Commands run in order after active work · Esc/Ctrl+C cancels work and queue"
	m.refresh(false)
	m.viewport.GotoTop()
}

// drainCommands runs only on the UI owner after a worker has fully settled. A
// picker or private login may hold the UI while the user finishes their choice.
func (m *model) drainCommands() tea.Cmd {
	var cmds []tea.Cmd
	for !m.busy && m.savingDefault == nil && !m.loggingIn && m.picker == nil && !m.quitting && m.ctx.Err() == nil && len(m.queuedCommands) > 0 {
		request := m.queuedCommands[0]
		if m.loginRequired && request.kind == skillCommand {
			m.notice = "Queued skill needs login · /login or /queue clear"
			break
		}
		m.queuedCommands = m.queuedCommands[1:]
		cmds = append(cmds, m.dispatchCommand(request))
	}
	return tea.Batch(cmds...)
}

func (m *model) cancelWork() {
	m.queuedCommands = nil
	m.cancel()
	m.activity = "Canceling"
	if m.queueView {
		m.refresh(m.scroll.following)
	}
	m.notice = "Queued commands cleared"
}

func (m *model) quit() tea.Cmd {
	m.quitting = true
	m.queuedCommands = nil
	return tea.Quit
}

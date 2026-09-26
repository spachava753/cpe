package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
)

type queuedCommand struct {
	id      uint64
	request commandRequest
}

type interruptedStream struct {
	after int
	text  string
}

func (r commandRequest) isPrompt() bool { return r.kind == "" || r.kind == skillCommand }

func (m *model) dispatchPrompt(request commandRequest) tea.Cmd {
	if !m.busy && m.panel.kind != queuePanel {
		return m.start(request.argument)
	}
	if m.promptMode == config.PromptSteer {
		return m.requestSteering(request)
	}
	return m.enqueueCommand(request)
}

func (m *model) requestSteering(request commandRequest) tea.Cmd {
	if m.panel.kind == queuePanel {
		m.closePanel()
	}
	m.steering = append(m.steering, request)
	// Branch restoration must settle before the interpreter can accept input.
	if m.busy && !m.loggingIn && m.operation != branchCommand {
		m.steer()
	}
	m.notice = "Steering requested · waiting for current work to settle"
	return nil
}

func (m *model) chooseSteeringPrompt() {
	var items []list.Item
	for _, pending := range m.queuedCommands {
		if pending.request.isPrompt() {
			items = append(items, choice{strconv.FormatUint(pending.id, 10), fmt.Sprintf("%d. %s", pending.id, oneline(pending.request.argument))})
		}
	}
	if len(items) == 0 {
		m.notice = "No queued prompts to steer"
		return
	}
	m.openPicker(queueSteerCommand, "Choose a queued prompt · Enter steers immediately", items, 0)
}

func (m *model) promotePrompt(id string) tea.Cmd {
	for i, pending := range m.queuedCommands {
		if strconv.FormatUint(pending.id, 10) == id && pending.request.isPrompt() {
			m.queuedCommands = append(m.queuedCommands[:i], m.queuedCommands[i+1:]...)
			return m.requestSteering(pending.request)
		}
	}
	m.notice = "Queued prompt is no longer pending"
	return nil
}

func (m *model) configurePromptMode(value string) {
	if value != "" {
		mode, err := config.ParsePromptMode(value)
		if err != nil {
			m.notice = "Error: " + err.Error()
			return
		}
		m.promptMode = mode
		m.notice = "Prompt mode: " + mode.String() + " · current conversation"
		return
	}
	var items []list.Item
	selected := 0
	for _, mode := range []config.PromptMode{config.PromptQueue, config.PromptSteer} {
		label := mode.String()
		if mode == m.promptMode {
			selected = len(items)
			label += currentChoiceSuffix
		}
		items = append(items, choice{mode.String(), label})
	}
	m.openPicker(promptModeCommand, "Choose busy prompt delivery", items, selected)
}

// cancellationOnly distinguishes an expected stop from a joined durability or
// settings failure. A steering cause must not hide an unsuccessful settlement.
func cancellationOnly(err error) bool {
	if err == nil || err == context.Canceled || err == agent.ErrSteered {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !cancellationOnly(child) {
				return false
			}
		}
		return true
	}
	if child := errors.Unwrap(err); child != nil {
		return cancellationOnly(child)
	}
	return false
}

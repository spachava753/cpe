package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
)

const currentChoiceSuffix = " (current)"

const defaultEffort = "default"
const saveDefaultKey = "ctrl+s"

type choice struct{ value, label string }

func (c choice) Title() string       { return c.label }
func (c choice) Description() string { return "" }
func (c choice) FilterValue() string { return strings.TrimSuffix(c.label, currentChoiceSuffix) }

// modelChoice keeps a pending profile name and its settings together. Reasoning
// edits compose with a queued model switch before either has reached a boundary.
type modelChoice struct {
	name    string
	profile config.Model
}

func (m model) modelTarget() modelChoice {
	if m.pendingModel != nil {
		return *m.pendingModel
	}
	return modelChoice{m.name, m.profile}
}

type picker struct {
	command string
	list    list.Model
}

func effortLabel(effort string) string {
	if effort == "" {
		return "provider default"
	}
	return effort
}

func (m *model) configureModel(command, value string) {
	switch command {
	case modelCommand:
		if value != "" {
			m.switchModel(value)
			return
		}
		var items []list.Item
		selected := 0
		for _, name := range slices.Sorted(maps.Keys(m.profiles)) {
			profile := m.profiles[name]
			label := fmt.Sprintf("%s · %s/%s", name, profile.Provider, profile.ID)
			if name == m.name {
				selected = len(items)
				label += currentChoiceSuffix
			}
			items = append(items, choice{name, oneline(label)})
		}
		if len(items) == 0 {
			m.notice = "No model profiles configured"
			return
		}
		m.openPicker(command, "Choose model", items, selected)
	case reasoningCommand:
		target := m.modelTarget()
		if value != "" {
			effort := value
			if value == providerEffort {
				effort = ""
			}
			if value == defaultEffort {
				effort = m.profiles[target.name].ReasoningEffort
			}
			if err := m.setReasoning(target, effort); err != nil {
				m.notice = "Error: " + err.Error() + "; use /reasoning to choose"
				return
			}
			m.notice = "Reasoning: " + effortLabel(effort)
			if m.busy {
				m.notice += " · change queued for " + target.name
			}
			return
		}
		items := []list.Item{choice{defaultEffort, "default · configured setting (" + effortLabel(m.profiles[target.name].ReasoningEffort) + ")"}}
		selected := 0
		for _, effort := range config.ReasoningEfforts() {
			label := effort
			if effort == target.profile.ReasoningEffort {
				selected = len(items)
				label += currentChoiceSuffix
			}
			items = append(items, choice{effort, label})
		}
		label := "provider · omit reasoning effort"
		if target.profile.ReasoningEffort == "" {
			selected = len(items)
			label += currentChoiceSuffix
		}
		items = append(items, choice{providerEffort, label})
		m.openPicker(command, "Choose reasoning for "+oneline(target.name)+" · support varies by model", items, selected)
	}
}

func (m *model) openPicker(command, title string, items []list.Item, selected int) {
	delegate := m.pickerDelegate()
	l := list.New(items, delegate, m.viewport.Width(), m.viewport.Height())
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetShowFilter(false)
	l.SetFilteringEnabled(command == modelCommand)
	if command == modelCommand {
		l.FilterInput.SetVirtualCursor(true)
		l.FilterInput.CharLimit = 0
		l.FilterInput.Placeholder = "Type to filter models"
		l.SetStatusBarItemName("matching model", "matching models")
	}
	l.DisableQuitKeybindings()
	l.Select(selected)
	m.picker = &picker{command: command, list: l}
	m.stylePicker()
	m.layout()
	m.notice = title
}

func (m model) updatePicker(msg tea.Msg) (model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case quitKey:
			if m.busy {
				m.picker = nil
				m.cancelWork()
				return m, nil
			}
			return m, m.quit()
		case escapeKey:
			m.picker = nil
			m.notice = "Selection canceled"
			return m, nil
		case enterKey, saveDefaultKey:
			if key.String() == saveDefaultKey {
				if m.picker.command != modelCommand && m.picker.command != reasoningCommand && m.picker.command != promptModeCommand {
					return m, nil
				}
				if m.configDir == "" {
					m.notice = "Default settings are unavailable"
					return m, nil
				}
			}
			if selected, ok := m.picker.list.SelectedItem().(choice); ok {
				command := m.picker.command
				m.picker = nil
				if key.String() == saveDefaultKey {
					return m, m.saveDefault(command, selected.value)
				}
				if command == loginCommand {
					return m, m.configureLogin(selected.value)
				}
				if command == queueSteerCommand {
					return m, m.promotePrompt(selected.value)
				}
				if command == sessionListCommand {
					return m, m.dispatchCommand(commandRequest{sessionResumeCommand, selected.value})
				}
				if command == promptModeCommand {
					m.configurePromptMode(selected.value)
					return m, nil
				}
				if command == themeCommand {
					m.configureTheme(selected.value)
				} else {
					m.configureModel(command, selected.value)
				}
			}
			return m, nil
		}
		if m.picker.command != modelCommand {
			var cmd tea.Cmd
			m.picker.list, cmd = m.picker.list.Update(key)
			return m, cmd
		}
		// Only navigation keys go to the list. Printable shortcuts such as j,
		// k, / and ? belong to the query; left/right/home/end edit its cursor.
		switch key.String() {
		case "ctrl+u":
			m.picker.list.ResetFilter()
			m.picker.list.GoToStart()
			return m, m.picker.list.FilterInput.Focus()
		case "up", "down", pageUpKey, pageDownKey:
			var cmd tea.Cmd
			m.picker.list, cmd = m.picker.list.Update(key)
			return m, cmd
		}
	}
	if m.picker.command != modelCommand {
		return m, nil
	}
	// Filter synchronously so an immediate Enter/Ctrl+S sees the matches for
	// the latest edit. Keep textinput's cursor rather than SetFilterText's reset.
	input, cmd := m.picker.list.FilterInput.Update(msg)
	if input.Value() != m.picker.list.FilterValue() {
		if input.Value() == "" {
			m.picker.list.ResetFilter()
			m.picker.list.GoToStart()
		} else {
			m.picker.list.SetFilterText(input.Value())
		}
	}
	m.picker.list.FilterInput = input
	return m, cmd
}

func (m *model) switchModel(name string) {
	profile, ok := m.profiles[name]
	if !ok {
		m.notice = fmt.Sprintf("Error: unknown model profile %q; use /model to choose", name)
		return
	}
	if name == m.name && m.pendingModel == nil {
		m.notice = "Model: " + name
		return
	}
	if name == m.name {
		profile = m.profile
	}
	generator, err := m.newGenerator(m.ctx, profile)
	if err == nil {
		if m.busy {
			err = m.agent.QueueModel(name, profile, generator)
		} else {
			err = m.agent.SetModel(profile, generator)
		}
	}
	if err != nil {
		m.notice = "Error: " + err.Error()
		return
	}
	if m.busy {
		m.pendingModel = &modelChoice{name, profile}
		m.notice = "Model switch queued"
		return
	}
	m.name, m.profile = name, profile
	m.contextTokens = m.agent.ContextEstimate()
	m.refresh(m.scroll.following)
	m.loginRequired = m.needsLogin != nil && m.needsLogin(profile)
	m.notice = "Model: " + name
	if m.loginRequired {
		m.notice += " · sign in with /login"
	}
}

func (m *model) acceptSelection(selected *agent.ModelSelection) {
	m.name, m.profile, m.contextTokens = selected.Name, selected.Model, selected.ContextTokens
	if m.pendingModel != nil && m.pendingModel.name == selected.Name && m.pendingModel.profile == selected.Model {
		m.pendingModel = nil
	}
	m.loginRequired = m.needsLogin != nil && m.needsLogin(m.profile)
	m.notice = "Model: " + selected.Name
}

func (m *model) setReasoning(target modelChoice, effort string) error {
	profile, err := target.profile.WithReasoningEffort(effort)
	if err != nil {
		return err
	}
	if m.busy {
		generator, err := m.newGenerator(m.ctx, profile)
		if err != nil {
			return err
		}
		if err := m.agent.QueueModel(target.name, profile, generator); err != nil {
			return err
		}
		m.pendingModel = &modelChoice{target.name, profile}
		return nil
	}
	if err := m.agent.SetReasoningEffort(effort); err != nil {
		return err
	}
	m.profile = m.agent.Model()
	return nil
}

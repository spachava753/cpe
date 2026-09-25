package tui

import (
	"fmt"
	"maps"
	"slices"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/spachava753/cpe/internal/config"
)

const currentChoiceSuffix = " (current)"

const defaultEffort = "default"

type choice struct{ value, label string }

func (c choice) Title() string       { return c.label }
func (c choice) Description() string { return "" }
func (c choice) FilterValue() string { return c.label }

type picker struct {
	command string
	scope   selectionScope
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
		if value != "" {
			effort := value
			if value == providerEffort {
				effort = ""
			}
			if value == defaultEffort {
				effort = m.profiles[m.name].ReasoningEffort
			}
			if err := m.agent.SetReasoningEffort(effort); err != nil {
				m.notice = "Error: " + err.Error() + "; use /reasoning to choose"
				return
			}
			m.profile = m.agent.Model()
			m.notice = "Reasoning: " + effortLabel(effort)
			return
		}
		items := []list.Item{choice{defaultEffort, "default · configured setting (" + effortLabel(m.profiles[m.name].ReasoningEffort) + ")"}}
		selected := 0
		for _, effort := range config.ReasoningEfforts() {
			label := effort
			if effort == m.profile.ReasoningEffort {
				selected = len(items)
				label += currentChoiceSuffix
			}
			items = append(items, choice{effort, label})
		}
		label := "provider · omit reasoning effort"
		if m.profile.ReasoningEffort == "" {
			selected = len(items)
			label += currentChoiceSuffix
		}
		items = append(items, choice{providerEffort, label})
		m.openPicker(command, "Choose reasoning · support varies by model", items, selected)
	}
}

func (m *model) openPicker(command, title string, items []list.Item, selected int) {
	delegate := m.pickerDelegate()
	l := list.New(items, delegate, m.viewport.Width(), m.viewport.Height())
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	l.Select(selected)
	m.picker = &picker{command: command, list: l}
	m.notice = title
}

func (m model) updatePicker(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case quitKey:
		return m, tea.Quit
	case escapeKey:
		m.picker = nil
		m.notice = "Selection canceled"
		return m, nil
	case enterKey:
		if selected, ok := m.picker.list.SelectedItem().(choice); ok {
			command, scope := m.picker.command, m.picker.scope
			m.picker = nil
			if command == defaultsMenu {
				m.configureModel(selected.value, "")
				if m.picker != nil {
					m.picker.scope = savedSelection
					m.notice = "Save default model"
					if selected.value == reasoningCommand {
						m.notice = "Save default reasoning for " + oneline(m.name)
						items := m.picker.list.Items()[1:]
						m.picker.list.SetItems(items)
						for i, item := range items {
							entry, _ := item.(choice)
							value := entry.value
							if value == m.profile.ReasoningEffort || (value == providerEffort && m.profile.ReasoningEffort == "") {
								m.picker.list.Select(i)
							}
						}
					}
				}
				return m, nil
			}
			if scope == savedSelection {
				return m, m.saveDefault(command, selected.value)
			}
			if command == loginCommand {
				return m, m.configureLogin(selected.value)
			}
			if command == themeCommand {
				m.configureTheme(selected.value)
			} else {
				m.configureModel(command, selected.value)
			}
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.picker.list, cmd = m.picker.list.Update(key)
	return m, cmd
}

func (m *model) switchModel(name string) {
	profile, ok := m.profiles[name]
	if !ok {
		m.notice = fmt.Sprintf("Error: unknown model profile %q; use /model to choose", name)
		return
	}
	if name == m.name {
		m.notice = "Model: " + name
		return
	}
	generator, err := m.newGenerator(m.ctx, profile)
	if err == nil {
		err = m.agent.SetModel(profile, generator)
	}
	if err != nil {
		m.notice = "Error: " + err.Error()
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

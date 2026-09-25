package tui

import (
	"context"
	"errors"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

const defaultsMenu = "defaults"
const providerEffort = "provider"

type selectionScope uint8

const (
	conversationSelection selectionScope = iota
	savedSelection
)

type defaultSave struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

// stop cancels and joins an in-flight save before terminal ownership ends.
func (s *defaultSave) stop() { s.cancel(); <-s.done }

type defaultSelection struct{ command, name, effort string }
type defaultsSaved struct {
	selection defaultSelection
	config    config.Config
	generator gai.Generator
	err       error
}

func (m *model) openDefaults() {
	if m.configDir == "" {
		m.notice = "Default settings are unavailable"
		return
	}
	m.openPicker(defaultsMenu, "Save defaults · applies now and to future conversations", []list.Item{
		choice{modelCommand, "Default model"},
		choice{reasoningCommand, "Default reasoning for " + oneline(m.name)},
	}, 0)
	m.syncCompletion()
}

func (m *model) saveDefault(command, value string) tea.Cmd {
	selection := defaultSelection{command: command, name: m.name, effort: value}
	if command == modelCommand {
		selection.name = value
	}
	if value == providerEffort {
		selection.effort = ""
	}
	ctx, cancel := context.WithCancel(m.ctx)
	done := make(chan struct{})
	results := make(chan defaultsSaved, 1)
	m.savingDefault = &defaultSave{cancel: cancel, done: done}
	m.notice = "Saving default…"
	m.syncCompletion()
	dir, factory := m.configDir, m.newGenerator
	go func() {
		defer close(done)
		defer cancel()
		result := defaultsSaved{selection: selection}
		if command == modelCommand {
			result.config, result.err = config.SaveDefaultModel(ctx, dir, selection.name)
		} else {
			result.config, result.err = config.SaveReasoning(ctx, dir, selection.name, selection.effort)
		}
		if result.err == nil && command == modelCommand {
			result.generator, result.err = factory(ctx, result.config.Models[selection.name])
		}
		results <- result
	}()
	return func() tea.Msg { return <-results }
}

func (m *model) applyDefault(result defaultsSaved) {
	m.savingDefault = nil
	if result.config.Models != nil {
		m.profiles = result.config.Models
	}
	if result.err != nil {
		m.notice = "Error: " + result.err.Error()
		if errors.Is(result.err, context.Canceled) {
			m.notice = "Default save canceled"
		}
		if result.config.Models != nil {
			m.notice = "Default saved, but could not apply: " + result.err.Error()
		}
		m.syncCompletion()
		return
	}
	selected := result.selection
	if selected.command == modelCommand {
		result.err = m.agent.SetModel(m.profiles[selected.name], result.generator)
		if result.err == nil {
			m.name, m.profile = selected.name, m.agent.Model()
		}
		m.notice = "Default model saved: " + selected.name
	} else {
		result.err = m.agent.SetReasoningEffort(selected.effort)
		if result.err == nil {
			m.profile = m.agent.Model()
		}
		m.notice = "Default reasoning saved for " + selected.name + ": " + effortLabel(selected.effort)
	}
	if result.err != nil {
		m.notice = "Default saved, but could not apply: " + result.err.Error()
	}
	m.contextTokens = m.agent.ContextEstimate()
	m.loginRequired = m.needsLogin != nil && m.needsLogin(m.profile)
	m.syncCompletion()
	m.refresh(m.scroll.following)
}

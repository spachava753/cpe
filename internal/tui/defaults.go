package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

const providerEffort = "provider"

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

func (m *model) saveDefault(command, value string) tea.Cmd {
	selection := defaultSelection{command: command, name: m.modelTarget().name, effort: value}
	if command == modelCommand {
		selection.name = value
	}
	if command == reasoningCommand {
		switch value {
		case providerEffort:
			selection.effort = ""
		case defaultEffort:
			selection.effort = m.profiles[selection.name].ReasoningEffort
		}
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
		switch command {
		case promptModeCommand:
			var mode config.PromptMode
			mode, result.err = config.ParsePromptMode(value)
			if result.err == nil {
				result.config, result.err = config.SavePromptMode(ctx, dir, mode)
			}
		case modelCommand:
			result.config, result.err = config.SaveDefaultModel(ctx, dir, selection.name)
		default:
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
	// Queue advancement can start another operation in this same UI update.
	// Retain the commit-aware outcome before that operation replaces the notice.
	defer func() {
		if result.err != nil && (!errors.Is(result.err, context.Canceled) || result.config.Models != nil) {
			m.operationErrors = append(m.operationErrors, m.notice)
		}
	}()
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
	if selected.command == promptModeCommand {
		m.promptMode = result.config.TUI.PromptMode
		m.notice = "Default prompt mode saved: " + m.promptMode.String()
		m.syncCompletion()
		return
	}
	if selected.command == modelCommand {
		if m.busy {
			result.err = m.agent.QueueModel(selected.name, m.profiles[selected.name], result.generator)
			if result.err == nil {
				m.pendingModel = &modelChoice{selected.name, m.profiles[selected.name]}
				m.notice = "Default model saved: " + selected.name + " · switch queued"
			} else {
				m.notice = "Default saved, but could not apply: " + result.err.Error()
			}
			m.syncCompletion()
			return
		}
		result.err = m.agent.SetModel(m.profiles[selected.name], result.generator)
		if result.err == nil {
			m.name, m.profile = selected.name, m.agent.Model()
		}
		m.notice = "Default model saved: " + selected.name
	} else {
		target := m.modelTarget()
		if target.name == selected.name {
			result.err = m.setReasoning(target, selected.effort)
		}
		m.notice = "Default reasoning saved for " + selected.name + ": " + effortLabel(selected.effort)
	}
	if result.err != nil {
		m.notice = "Default saved, but could not apply: " + result.err.Error()
	}
	if !m.busy {
		m.contextTokens = m.agent.ContextEstimate()
	}
	m.loginRequired = m.needsLogin != nil && m.needsLogin(m.profile)
	m.syncCompletion()
	m.refresh(m.scroll.following)
}

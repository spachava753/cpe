package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const loginGoCommand = "/login opencode-go"
const loginCodexCommand = "/login codex"
const deviceLogin = "device"
const goLogin = "opencode-go"

func (m *model) configureLogin(method string) tea.Cmd {
	switch method {
	case "":
		var items []list.Item
		if m.login != nil {
			items = append(items, choice{"codex", "ChatGPT Codex · browser"}, choice{deviceLogin, "ChatGPT Codex · device code"})
		}
		if m.loginGo != nil {
			items = append(items, choice{goLogin, "OpenCode Go · API key"})
		}
		if len(items) == 0 {
			m.notice = "Login is unavailable"
			return nil
		}
		selected := 0
		if m.profile.Credential == goLogin && m.loginGo != nil {
			selected = len(items) - 1
		}
		m.openPicker(loginCommand, "Choose login provider", items, selected)
	case "codex", deviceLogin:
		if m.login == nil {
			m.notice = "Codex login is unavailable"
			return nil
		}
		return m.start("/login " + method)
	case goLogin:
		if m.loginGo == nil {
			m.notice = "OpenCode Go login is unavailable"
			return nil
		}
		input := textinput.New()
		input.Prompt = "API key: "
		input.Placeholder = "Paste your OpenCode Go key"
		input.EchoMode = textinput.EchoPassword
		input.EchoCharacter = '•'
		input.CharLimit = 4096
		m.keyInput = &input
		m.input.Blur()
		m.loggingIn = true
		m.usageView, m.staticView = false, ""
		m.loginText = "OpenCode Go\n\nSign in at https://opencode.ai/auth, subscribe to Go, and copy your API key.\n\nPaste it below. CPE saves it privately in ~/.cpe/opencode-go.json and adds available model profiles to config.json. Existing profiles stay unchanged.\n\nThe key is checked by the service on your first model request."
		m.notice = "Enter save · Esc cancel"
		m.layout()
		m.refresh(false)
		m.viewport.GotoTop()
		return m.keyInput.Focus()
	default:
		m.notice = "Use /login, /login codex, /login device, or /login opencode-go"
	}
	return nil
}

func (m model) updateKeyInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyPgUp, tea.KeyPgDown:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case tea.KeyEsc, tea.KeyCtrlC:
			m.keyInput.Reset()
			m.keyInput = nil
			m.loggingIn, m.loginText = false, ""
			m.notice = "Login canceled"
			m.layout()
			m.refresh(true)
			return m, m.input.Focus()
		case tea.KeyEnter:
			value := strings.TrimSpace(m.keyInput.Value())
			if value == "" {
				m.notice = "Paste an API key or press Esc to cancel"
				return m, nil
			}
			m.keyInput.Reset()
			m.keyInput = nil
			m.input.Focus()
			return m, m.startWork(loginGoCommand, value)
		}
	}
	var cmd tea.Cmd
	*m.keyInput, cmd = m.keyInput.Update(msg)
	return m, cmd
}

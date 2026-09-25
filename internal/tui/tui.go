package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/skills"
	"github.com/spachava753/cpe/internal/theme"
)

const loginDeviceCommand = "/login device"
const quitKey = "ctrl+c"
const escapeKey = "esc"
const enterKey = "enter"
const shiftEnterKey = "shift+enter"

type update struct {
	event     agent.Event
	done      bool
	err       error
	loginText string
	profiles  map[string]config.Model
}

// Options supplies model profiles, composer submission preferences, and UI-only authentication. NewGenerator
// defaults to agent.Provider and constructs a provider without generating text.
// Login handles Codex, honoring cancellation and sending only UI instructions.
// LoginGo accepts a UI-only API key and returns all profiles after import. Both
// callbacks must omit secrets from errors and display messages.
// LoginRequired is checked at startup and after changing profiles. ThemeDir is
// the configuration directory for theme loading, live reload, and saved theme
// selections. An empty directory disables theme configuration. Reload failures
// keep the last valid theme and display a warning.
type Options struct {
	SubmitKey     config.SubmitKey
	Models        map[string]config.Model
	NewGenerator  func(context.Context, config.Model) (gai.Generator, error)
	Login         func(context.Context, string, func(string)) error
	LoginGo       func(context.Context, string) (map[string]config.Model, error)
	LoginRequired func(config.Model) bool
	ThemeDir      string
}
type model struct {
	ctx           context.Context
	agent         *agent.Agent
	name          string
	submitKey     config.SubmitKey
	input         textarea.Model
	viewport      viewport.Model
	scroll        transcriptScroll
	spinner       spinner.Model
	width, height int
	busy          bool
	cancel        context.CancelFunc
	events        <-chan update
	messages      gai.Dialog
	provisional   string
	activity      string
	notice        string
	login         func(context.Context, string, func(string)) error
	loginGo       func(context.Context, string) (map[string]config.Model, error)
	keyInput      *textinput.Model
	loggingIn     bool
	loginRequired bool
	loginText     string
	profile       config.Model
	profiles      map[string]config.Model
	newGenerator  func(context.Context, config.Model) (gai.Generator, error)
	needsLogin    func(config.Model) bool
	picker        *picker
	usage         agent.Usage
	contextTokens int
	usageView     bool
	staticView    string
	theme         theme.Theme
	appearance    theme.Appearance
	styles        styles
	themeDir      string
	themeRevision uint64
	themeError    string
	completion    completion
	commands      []slashCommand
}

func newModel(ctx context.Context, a *agent.Agent, name string) model {
	input := textarea.New()
	input.SetVirtualCursor(true)
	input.Placeholder = "Ask anything…"
	input.Prompt = "› "
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetHeight(3)
	input.SetWidth(80)
	input.Focus()
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	m := model{ctx: ctx, agent: a, name: name, input: input, viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(14)), scroll: transcriptScroll{following: true}, spinner: spin, width: 80, height: 24, messages: a.Messages(), notice: "/help for commands"}
	m.profile = a.Model()
	m.commands = append([]slashCommand{}, slashCommands...)
	for _, command := range a.Skills().Commands() {
		m.commands = append(m.commands, slashCommand{name: command.Name, description: oneline(command.Description)})
	}
	m.usage, m.contextTokens = a.Usage(), a.ContextEstimate()
	m.newGenerator = func(ctx context.Context, profile config.Model) (gai.Generator, error) {
		dir, err := config.Directory()
		if err != nil {
			return nil, err
		}
		return agent.Provider(ctx, profile, dir, a.Checkpoints()[0].ID)
	}
	m.applyTheme(theme.Default())
	return m
}

// Run owns the terminal until the user exits or ctx is canceled. Agent and store
// lifetimes remain the caller's responsibility.
func Run(ctx context.Context, a *agent.Agent, name string, options Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newModel(ctx, a, name)
	m.submitKey = options.SubmitKey
	m.themeDir = options.ThemeDir
	if m.themeDir != "" {
		m.appearance = theme.SystemAppearance(ctx)
		t, err := theme.Load(m.themeDir, m.appearance)
		if err != nil {
			m.themeError = err.Error()
		} else {
			m.applyTheme(t)
		}
	}
	m.login, m.loginGo, m.needsLogin, m.profiles = options.Login, options.LoginGo, options.LoginRequired, options.Models
	if options.NewGenerator != nil {
		m.newGenerator = options.NewGenerator
	}
	if m.needsLogin != nil {
		m.loginRequired = m.needsLogin(m.profile)
	}
	if m.loginRequired {
		m.notice = "Sign in with /login to use this model"
	}
	program := tea.NewProgram(m, tea.WithContext(ctx))
	final, err := program.Run()
	// A parent-context exit can interrupt the renderer before the worker finishes.
	// Join it before the caller closes the journal or interpreter.
	if last, ok := final.(model); ok && last.busy && last.cancel != nil {
		last.cancel()
		for u := range last.events {
			if u.done {
				break
			}
		}
	}
	return err
}
func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, pollTheme(m.ctx, m.themeDir, m.themeRevision))
}
func await(events <-chan update) tea.Cmd { return func() tea.Msg { return <-events } }
func (m *model) start(text string) tea.Cmd {
	return m.startWork(text, "")
}
func (m *model) startWork(text, key string) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.busy = true
	m.provisional = ""
	m.notice = ""
	m.usageView = false
	m.staticView = ""
	m.activity = "Thinking"
	m.loggingIn = text == loginCodexCommand || text == loginDeviceCommand || text == loginGoCommand
	m.loginText = ""
	if m.loggingIn {
		m.activity = "Signing in"
	}
	m.layout()
	m.refresh(true)
	ch := make(chan update, 64)
	m.events = ch
	go func() {
		defer close(ch)
		var err error
		var profiles map[string]config.Model
		switch {
		case text == loginGoCommand:
			profiles, err = m.loginGo(ctx, key)
		case m.loggingIn:
			method := "browser"
			if text == loginDeviceCommand {
				method = deviceLogin
			}
			err = m.login(ctx, method, func(text string) { ch <- update{loginText: text} })
		case text == compactCommand:
			err = m.agent.Compact(ctx)
		case strings.HasPrefix(text, branchCommand+" "):
			err = m.agent.Branch(ctx, strings.TrimSpace(strings.TrimPrefix(text, branchCommand+" ")))
		default:
			err = m.agent.Prompt(ctx, text, func(event agent.Event) {
				// Keep durable message notifications even after cancellation. The UI drains
				// until done, so a cancel cannot strand the producer on a full channel.
				ch <- update{event: event}
			})
		}
		ch <- update{done: true, err: err, profiles: profiles}
	}()
	return tea.Batch(await(ch), m.spinner.Tick)
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case themeUpdate:
		if v.revision != m.themeRevision {
			return m, pollTheme(m.ctx, m.themeDir, m.themeRevision)
		}
		m.appearance = v.appearance
		if v.err != nil {
			m.themeError = v.err.Error()
		} else {
			m.themeError = ""
			if v.theme != m.theme {
				m.applyTheme(v.theme)
			}
		}
		if strings.HasPrefix(m.notice, "Theme: ") || strings.HasPrefix(m.notice, "Theme error: ") {
			m.notice = m.themeStatus()
		}
		return m, pollTheme(m.ctx, m.themeDir, m.themeRevision)
	case tea.WindowSizeMsg:
		atBottom := m.scroll.following
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
		m.layout()
		m.refresh(atBottom)
		return m, nil
	case update:
		if v.loginText != "" {
			m.loginText += v.loginText + "\n\n"
			m.refresh(false)
			m.viewport.GotoTop()
			return m, await(m.events)
		}
		if v.done {
			bottom := m.scroll.following
			if m.cancel != nil {
				m.cancel()
			}
			m.busy = false
			m.cancel = nil
			m.activity = ""
			m.provisional = ""
			m.messages = m.agent.Messages()
			m.usage, m.contextTokens = m.agent.Usage(), m.agent.ContextEstimate()
			if m.loggingIn && errors.Is(v.err, context.Canceled) {
				m.notice = "Login canceled"
			} else if errors.Is(v.err, context.Canceled) {
				m.notice = "Canceled · completed work saved"
			} else if v.err != nil {
				m.notice = "Error: " + v.err.Error()
			} else if m.loggingIn {
				m.loginRequired = m.needsLogin != nil && m.needsLogin(m.profile)
				m.notice = "Signed in · credentials saved to ~/.cpe/auth.json"
				if v.profiles != nil {
					m.profiles = v.profiles
					m.notice = "OpenCode Go key saved · models ready in /model"
				}
			} else {
				m.notice = "Saved"
			}
			m.loggingIn = false
			m.loginText = ""
			m.syncCompletion()
			m.layout()
			m.refresh(bottom)
			return m, nil
		}
		switch v.event.Kind {
		case agent.EventUsage:
			if v.event.Usage != nil {
				m.usage = *v.event.Usage
			}
		case agent.EventActivity:
			m.activity = v.event.Text
		case agent.EventDelta:
			m.provisional += v.event.Text
		case agent.EventMessage:
			if v.event.Message != nil {
				m.messages = append(append(gai.Dialog{}, m.messages...), *v.event.Message)
				if v.event.Message.Role == gai.Assistant {
					m.provisional = ""
				}
			}
		}
		m.refresh(m.scroll.following)
		return m, await(m.events)
	case tea.KeyPressMsg:
		if m.keyInput != nil {
			return m.updateKeyInput(msg)
		}
		if m.picker != nil {
			return m.updatePicker(v)
		}
		if m.completionKey(v) {
			return m, nil
		}
		switch v.String() {
		case quitKey, escapeKey:
			if m.busy {
				m.cancel()
				m.activity = "Canceling"
				return m, nil
			}
			if v.String() == quitKey {
				return m, tea.Quit
			}
			m.input.Reset()
			m.syncCompletion()
			return m, nil
		case "ctrl+d":
			if !m.busy && m.input.Value() == "" {
				return m, tea.Quit
			}
		case "pgup", "pgdown", "ctrl+u":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			m.scroll.following = m.viewport.AtBottom()
			return m, cmd
		case m.newlineKey(), "ctrl+j":
			if !m.loggingIn {
				m.input.InsertRune('\n')
				m.syncCompletion()
			}
			return m, nil
		case m.submitKey.String():
			if m.busy {
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			literalSlash := m.completion.literal
			m.input.Reset()
			m.syncCompletion()
			command := strings.Fields(text)[0]
			argument := strings.TrimSpace(strings.TrimPrefix(text, command))
			if command == loginCommand {
				return m, m.configureLogin(argument)
			}
			if command == themeCommand {
				m.configureTheme(argument)
				return m, nil
			}
			if command == modelCommand || command == reasoningCommand {
				m.configureModel(command, argument)
				return m, nil
			}
			switch text {
			case quitCommand, exitCommand:
				return m, tea.Quit
			case helpCommand:
				m.notice = "/model  /reasoning  /theme  /usage  /login  /skill:NAME [args]  /compact  /tree  /branch ID  /session  /quit"
				return m, nil
			case usageCommand:
				m.staticView = ""
				m.usageView = true
				m.notice = "Session totals · PgUp/PgDn scroll"
				m.refresh(false)
				m.viewport.GotoTop()
				return m, nil
			case sessionCommand:
				m.notice = m.agent.SessionFile()
				return m, nil
			case treeCommand:
				var b strings.Builder
				for _, e := range m.agent.Checkpoints() {
					var data struct {
						Label string `json:"label"`
					}
					_ = json.Unmarshal(e.Data, &data)
					if e.Type == "session" {
						data.Label = "start"
					}
					fmt.Fprintf(&b, "%s  %s\n", e.ID, data.Label)
				}
				m.usageView = false
				m.staticView = "Checkpoints — /branch ID to continue from one\n\n" + clean(b.String())
				m.refresh(false)
				m.viewport.GotoTop()
				m.notice = "History is preserved when branching"
				return m, nil
			}
			if !literalSlash && strings.HasPrefix(text, "/") && text != compactCommand && !strings.HasPrefix(text, branchCommand+" ") && !strings.HasPrefix(command, skills.CommandPrefix) {
				m.notice = "Unknown command. /help for commands"
				return m, nil
			}
			if m.loginRequired && !strings.HasPrefix(text, branchCommand+" ") {
				m.input.SetValue(text)
				m.notice = "Sign in with /login first"
				return m, nil
			}
			return m, m.start(text)
		}
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		if wheel, ok := msg.(tea.MouseWheelMsg); ok && (wheel.Button == tea.MouseWheelUp || wheel.Button == tea.MouseWheelDown) {
			m.scroll.following = m.viewport.AtBottom()
		}
		return m, cmd
	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	if m.keyInput != nil {
		return m.updateKeyInput(msg)
	}
	if m.picker != nil {
		return m, nil
	}
	if !m.loggingIn {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.syncCompletion()
		return m, cmd
	}
	return m, nil
}
func (m *model) refresh(bottom bool) {
	if m.staticView != "" {
		m.setViewportContent(m.staticView, bottom)
		return
	}
	if m.usageView {
		m.setViewportContent(m.usageDetails(), bottom)
		return
	}
	if m.loggingIn && m.loginText != "" {
		m.setViewportContent(clean(m.loginText), bottom)
		return
	}
	var b strings.Builder
	if len(m.messages) == 0 {
		b.WriteString("A conversation with a persistent workspace.\n\nAsk a question or describe a change.\n")
	}
	for _, message := range m.messages {
		label := "You"
		labelStyle := m.styles.user
		switch message.Role {
		case gai.Assistant:
			label = "Assistant"
			labelStyle = m.styles.assistant
		case gai.ToolResult:
			label = "Starlark"
			labelStyle = m.styles.tool.Bold(m.theme.Bold)
		}
		b.WriteString(labelStyle.Render(label) + "\n")
		for _, block := range message.Blocks {
			if block.Content == nil {
				continue
			}
			text := block.Content.String()
			if block.BlockType == gai.Content && block.ModalityType == gai.Image {
				text = "[Image: " + block.MimeType + "]"
			}
			if block.BlockType == gai.Thinking {
				continue
			}
			if block.BlockType == gai.ToolCall {
				var call gai.ToolCallInput
				if json.Unmarshal([]byte(text), &call) == nil {
					code, _ := call.Parameters["code"].(string)
					text = "› starlark_repl\n" + code
				}
			}
			text = clean(text)
			if block.BlockType == gai.ToolCall || message.Role == gai.ToolResult {
				text = m.styles.tool.Render(text)
			}
			b.WriteString(text)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	if m.provisional != "" {
		b.WriteString(m.styles.assistant.Render("Assistant") + "\n" + clean(m.provisional))
	}
	m.setViewportContent(b.String(), bottom)
}
func (m model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m model) viewContent() string {
	if m.width < 24 || m.height < 8 {
		return "Resize terminal to at least 24 × 8"
	}
	profile := "  " + oneline(m.name) + " · " + effortLabel(m.profile.ReasoningEffort)
	title := m.styles.accent.Render("cpe") + m.styles.base.Render(profile+"  ") + m.styles.muted.Render(oneline(filepath.Base(m.agent.SessionFile())))
	status := oneline(m.notice)
	if m.busy {
		status = m.spinner.View() + m.styles.base.Render(" "+oneline(m.activity)+" · Esc to cancel")
	} else if strings.HasPrefix(status, "Error:") {
		status = m.styles.failure.Render(status)
	} else {
		status = m.styles.muted.Render(status)
	}
	line := m.styles.border.Render(strings.Repeat("─", max(1, m.width-2)))
	footer := m.styles.muted.Render(keyLabel(m.submitKey.String()) + " send · " + keyLabel(m.newlineKey()) + " newline · PgUp/PgDn scroll · Ctrl+C quit")
	if m.busy {
		footer = m.styles.muted.Render("Draft next message · " + keyLabel(m.newlineKey()) + " newline · Esc/Ctrl+C cancel")
		if m.loggingIn {
			footer = m.styles.muted.Render("Esc/Ctrl+C cancel login")
		}
	}
	content := m.viewport.View()
	if m.picker != nil {
		content = m.picker.list.View()
		footer = m.styles.muted.Render("↑/↓ select · Enter apply · Esc cancel · Ctrl+C quit")
	}
	if m.completionHeight() > 0 {
		footer = m.styles.muted.Render(m.completionHelp())
	}
	if m.themeError != "" {
		footer = m.styles.failure.Render(oneline(m.themeStatus()))
	}
	counts, totals := m.usageLines()
	rows := []string{ansi.Truncate(title, m.width, "…"), content, ansi.Truncate(status, m.width, "…")}
	if !m.loggingIn {
		rows = append(rows, m.styles.muted.Render(ansi.Truncate(counts, m.width, "…")), m.styles.muted.Render(ansi.Truncate(totals, m.width, "…")))
	}
	if popup := m.completionView(); popup != "" {
		rows = append(rows, popup)
	}
	editor := m.input.View()
	if m.keyInput != nil {
		editor = m.keyInput.View() + strings.Repeat("\n", max(0, m.input.Height()-1))
		footer = m.styles.muted.Render("Enter save key · Esc cancel")
	}
	rows = append(rows, line, editor, ansi.Truncate(footer, m.width, "…"))
	return m.canvas(strings.Join(rows, "\n"))
}

func (m *model) layout() {
	overhead := 7
	if m.loggingIn {
		overhead = 5
	}
	m.input.SetWidth(max(1, m.width-2))
	m.input.SetHeight(min(m.theme.InputHeight, max(1, m.height-overhead-1)))
	if m.keyInput != nil {
		m.keyInput.SetWidth(max(1, m.width-12))
		styles := m.keyInput.Styles()
		styles.Focused.Prompt, styles.Focused.Text, styles.Focused.Placeholder = m.styles.accent, m.styles.base, m.styles.muted
		styles.Blurred = styles.Focused
		styles.Cursor.Color = color(m.theme.Colors.Accent)
		m.keyInput.SetStyles(styles)
	}
	m.viewport.SetWidth(max(1, m.width-2))
	m.viewport.SetHeight(max(1, m.height-m.input.Height()-overhead-m.completionHeight()))
	if m.picker != nil {
		m.picker.list.SetSize(m.viewport.Width(), m.viewport.Height())
	}
}

func oneline(text string) string { return strings.Join(strings.Fields(clean(text)), " ") }

// Provider and tool text cannot inject terminal control sequences into the UI.
func clean(text string) string {
	text = ansi.Strip(text)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}

func (m model) newlineKey() string {
	if m.submitKey == config.SubmitShiftEnter {
		return enterKey
	}
	return shiftEnterKey
}

func keyLabel(key string) string {
	if key == shiftEnterKey {
		return "Shift+Enter"
	}
	return "Enter"
}

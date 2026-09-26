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
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/theme"
)

const quitKey = "ctrl+c"
const escapeKey = "esc"
const enterKey = "enter"
const shiftEnterKey = "shift+enter"
const pageDownKey = "pgdown"
const pageUpKey = "pgup"
const cancelWorkAction = "cancel work"

type update struct {
	event       agent.Event
	done        bool
	err         error
	loginText   string
	loginNotice string
	profiles    map[string]config.Model
	steered     bool
}

// Options supplies model profiles, composer/mouse preferences, and UI-only authentication. NewGenerator
// defaults to agent.Provider and constructs a provider without generating text.
// Login handles Codex, honoring cancellation and sending only UI instructions.
// LoginGo accepts a UI-only API key and returns all profiles after import.
// LoginCopilot displays a device code and returns all profiles after import. All
// callbacks must omit secrets from errors and display messages.
// LoginRequired is checked at startup and after changing profiles. ThemeDir is
// the configuration directory for theme loading, live reload, and saved theme
// selections. ConfigDir enables Ctrl+S model/reasoning default persistence.
// An empty directory disables theme configuration. Reload failures
// keep the last valid theme and display a warning.
type Options struct {
	ConfigDir     string
	SubmitKey     config.SubmitKey
	MouseMode     config.MouseMode
	PromptMode    config.PromptMode
	Models        map[string]config.Model
	NewGenerator  func(context.Context, config.Model) (gai.Generator, error)
	Login         func(context.Context, string, func(string)) error
	LoginGo       func(context.Context, string) (map[string]config.Model, error)
	LoginCopilot  func(context.Context, func(string)) (map[string]config.Model, error)
	LoginRequired func(config.Model) bool
	ThemeDir      string
}
type model struct {
	configDir       string
	savingDefault   *defaultSave
	ctx             context.Context
	agent           *agent.Agent
	name            string
	pendingModel    *modelChoice
	queuedCommands  []queuedCommand
	nextQueuedID    uint64
	steering        []commandRequest
	steer           context.CancelFunc
	operation       commandKind
	promptMode      config.PromptMode
	interrupted     *interruptedStream
	operationErrors []string
	quitting        bool
	checkpoints     []session.Entry
	sessionFile     string
	submitKey       config.SubmitKey
	mouseMode       config.MouseMode
	input           textarea.Model
	viewport        viewport.Model
	scroll          transcriptScroll
	spinner         spinner.Model
	width, height   int
	busy            bool
	cancel          context.CancelFunc
	events          <-chan update
	messages        gai.Dialog
	provisional     string
	activity        string
	notice          string
	login           func(context.Context, string, func(string)) error
	loginGo         func(context.Context, string) (map[string]config.Model, error)
	loginCopilot    func(context.Context, func(string)) (map[string]config.Model, error)
	keyInput        *textinput.Model
	loggingIn       bool
	loginRequired   bool
	loginText       string
	profile         config.Model
	profiles        map[string]config.Model
	newGenerator    func(context.Context, config.Model) (gai.Generator, error)
	needsLogin      func(config.Model) bool
	picker          *picker
	usage           agent.Usage
	contextTokens   int
	panel           panelState
	theme           theme.Theme
	appearance      theme.Appearance
	styles          styles
	themeDir        string
	themeRevision   uint64
	themeError      string
	completion      completion
	commands        []slashCommand
}

func newModel(ctx context.Context, a *agent.Agent, name string) model {
	input := textarea.New()
	input.SetVirtualCursor(true)
	// Submission is handled by the parent; all newline events reaching the
	// textarea must retain its selection replacement and cursor behavior.
	input.KeyMap.InsertNewline.SetKeys(enterKey, shiftEnterKey, "ctrl+j", "ctrl+m")
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
	m.sessionFile, m.checkpoints = a.SessionFile(), a.Checkpoints()
	m.commands = append([]slashCommand{}, slashCommands...)
	for _, command := range a.Skills().Commands() {
		m.commands = append(m.commands, slashCommand{name: command.Name, description: oneline(command.Description)})
	}
	m.usage, m.contextTokens = a.Usage(), a.ContextEstimate()
	sessionID := a.Checkpoints()[0].ID
	m.newGenerator = func(ctx context.Context, profile config.Model) (gai.Generator, error) {
		dir, err := config.Directory()
		if err != nil {
			return nil, err
		}
		return agent.Provider(ctx, profile, dir, sessionID)
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
	m.mouseMode = options.MouseMode
	m.promptMode = options.PromptMode
	m.configDir = options.ConfigDir
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
	m.loginCopilot = options.LoginCopilot
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
	if last, ok := final.(model); ok && last.savingDefault != nil {
		last.savingDefault.stop()
	}
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
func (m *model) startWork(text, key string, additional ...string) tea.Cmd {
	ctx, cancel := context.WithCancelCause(m.ctx)
	m.cancel = func() { cancel(context.Canceled) }
	m.steer = func() { cancel(agent.ErrSteered) }
	m.operation = ""
	if text == compactCommand {
		m.operation = compactCommand
	}
	if strings.HasPrefix(text, branchCommand+" ") {
		m.operation = branchCommand
	}
	m.busy = true
	m.provisional = ""
	m.notice = ""
	m.panel = panelState{}
	m.activity = "Thinking"
	m.loggingIn = text == loginOpenAICommand || text == loginDeviceCommand || text == loginGoCommand || text == loginCopilotCommand
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
		loginNotice := "Signed in · credentials saved to ~/.cpe/auth.json"
		switch {
		case text == loginGoCommand:
			profiles, err = m.loginGo(ctx, key)
			loginNotice = "OpenCode Go key saved · models ready in /model"
		case text == loginCopilotCommand:
			profiles, err = m.loginCopilot(ctx, func(text string) { ch <- update{loginText: text} })
			loginNotice = "GitHub Copilot signed in · models ready in /model"
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
			err = m.agent.Prompts(ctx, append([]string{text}, additional...), func(event agent.Event) {
				// Keep durable message notifications even after cancellation. The UI drains
				// until done, so a cancel cannot strand the producer on a full channel.
				ch <- update{event: event}
			})
		}
		ch <- update{done: true, err: err, profiles: profiles, loginNotice: loginNotice, steered: errors.Is(context.Cause(ctx), agent.ErrSteered)}
	}()
	return tea.Batch(await(ch), m.spinner.Tick)
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	cmd = tea.Batch(cmd, next.drainCommands())
	// Synchronize focus after every transition, including picker application,
	// default saves, and asynchronous login completion. Focus's command starts
	// a fresh blink chain after an inactive surface discarded old timer events.
	focused := next.picker == nil && !next.loggingIn
	if focused != next.input.Focused() {
		if focused {
			cmd = tea.Batch(cmd, next.input.Focus())
		} else {
			next.input.Blur()
		}
	}
	if next.picker != nil && next.picker != m.picker && next.picker.command == modelCommand {
		cmd = tea.Batch(cmd, next.picker.list.FilterInput.Focus())
	}
	return next, cmd
}

func (m model) update(msg tea.Msg) (model, tea.Cmd) {
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
	case defaultsSaved:
		m.applyDefault(v)
		return m, nil
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
			_, selectionErr := m.agent.ApplyQueuedModel()
			selected := m.agent.SelectedModel()
			v.err = errors.Join(v.err, selectionErr)
			if selected != nil {
				m.acceptSelection(selected)
			}
			m.pendingModel = nil
			m.profile = m.agent.Model()
			m.cancel = nil
			m.activity = ""
			if m.operation == branchCommand || m.operation == compactCommand || len(m.agent.Messages()) < len(m.messages) {
				m.interrupted = nil
			}
			if v.steered && m.provisional != "" {
				m.interrupted = &interruptedStream{after: len(m.agent.Messages()), text: m.provisional}
			}
			m.provisional = ""
			m.messages = m.agent.Messages()
			m.checkpoints = m.agent.Checkpoints()
			m.usage, m.contextTokens = m.agent.Usage(), m.agent.ContextEstimate()
			if v.err != nil && !cancellationOnly(v.err) {
				m.notice = "Error: " + v.err.Error()
				m.operationErrors = append(m.operationErrors, v.err.Error())
			} else if v.steered {
				m.notice = "Interrupted by steering · completed work and partial tool state saved"
			} else if m.loggingIn && errors.Is(v.err, context.Canceled) {
				m.notice = "Login canceled"
			} else if errors.Is(v.err, context.Canceled) {
				m.notice = "Canceled · completed work saved"
			} else if m.loggingIn {
				m.loginRequired = m.needsLogin != nil && m.needsLogin(m.profile)
				m.notice = v.loginNotice
				if v.profiles != nil {
					m.profiles = v.profiles
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
		case agent.EventModel:
			if v.event.Selection != nil {
				m.acceptSelection(v.event.Selection)
			}
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
		if v.String() == "f4" {
			if m.mouseMode == config.MouseSelect {
				m.mouseMode = config.MouseScroll
			} else {
				m.mouseMode = config.MouseSelect
			}
			return m, nil
		}
		if m.keyInput != nil {
			return m.updateKeyInput(msg)
		}
		if m.picker != nil {
			return m.updatePicker(v)
		}
		if m.savingDefault != nil && (v.String() == quitKey || v.String() == escapeKey) {
			m.savingDefault.cancel()
			m.notice = "Canceling default save…"
			return m, nil
		}
		if v.String() == escapeKey && m.panel.kind != conversationPanel {
			if m.completionKey(v) {
				return m, nil
			}
			m.closePanel()
			return m, nil
		}
		if m.busy && (v.String() == quitKey || v.String() == escapeKey) {
			m.cancelWork()
			return m, nil
		}
		if m.completionKey(v) {
			return m, nil
		}
		switch v.String() {
		case quitKey, escapeKey:
			if v.String() == quitKey {
				return m, m.quit()
			}
			m.input.Reset()
			m.syncCompletion()
			return m, nil
		case saveDefaultKey:
			return m, nil
		case "ctrl+d":
			if !m.busy && m.savingDefault == nil && m.input.Value() == "" {
				return m, m.quit()
			}
		case pageUpKey, pageDownKey, "ctrl+u":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			m.scroll.following = m.viewport.AtBottom()
			return m, cmd
		case m.submitKey.String():
			return m, m.submitInput()
		}
	case tea.MouseMsg:
		if m.mouseMode != config.MouseScroll {
			return m, nil
		}
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
		return m.updatePicker(msg)
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
	switch m.panel.kind {
	case queuePanel:
		m.setViewportContent(m.queueText(), bottom)
		return
	case treePanel:
		m.setViewportContent(m.panel.text, bottom)
		return
	case usagePanel:
		m.setViewportContent(m.usageDetails(), bottom)
		return
	}
	if m.loggingIn && m.loginText != "" {
		m.setViewportContent(clean(m.loginText), bottom)
		return
	}
	var b strings.Builder
	var previews []toolPreview
	line := 0
	if len(m.messages) == 0 {
		b.WriteString("A conversation with a persistent workspace.\n\nAsk a question or describe a change.\n")
	}
	writeInterruption := func(index int) {
		if m.interrupted != nil && m.interrupted.after == index {
			text := m.styles.failure.Render("Assistant · interrupted by steering (not saved)") + "\n" + clean(m.interrupted.text) + "\n\n"
			b.WriteString(text)
			line += strings.Count(text, "\n")
		}
	}
	for index, message := range m.messages {
		writeInterruption(index)
		var body strings.Builder
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
		line++
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
			if message.Role == gai.ToolResult {
				text = strings.TrimRight(text, "\n")
			}
			if block.BlockType == gai.ToolCall || message.Role == gai.ToolResult {
				text = m.styles.tool.Render(text)
			}
			body.WriteString(text)
			body.WriteByte('\n')
		}
		text := body.String()
		if message.Role == gai.ToolResult && text != "" {
			text = strings.TrimRight(text, "\n") + "\n"
			previews = append(previews, toolPreview{start: line, end: line + strings.Count(text, "\n")})
		}
		b.WriteString(text)
		b.WriteByte('\n')
		line += strings.Count(text, "\n") + 1
	}
	writeInterruption(len(m.messages))
	if m.provisional != "" {
		b.WriteString(m.styles.assistant.Render("Assistant") + "\n" + clean(m.provisional))
	}
	m.setViewportContent(b.String(), bottom, previews...)
}
func (m model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	if m.mouseMode == config.MouseScroll {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m model) viewContent() string {
	if m.width < 24 || m.height < 8 {
		return "Resize terminal to at least 24 × 8"
	}
	profile := "  " + oneline(m.name) + " · " + effortLabel(m.profile.ReasoningEffort)
	title := m.styles.accent.Render("cpe") + m.styles.base.Render(profile+"  ") + m.styles.muted.Render(oneline(filepath.Base(m.sessionFile)))
	status := oneline(m.notice)
	if m.busy {
		cancelKey := "Esc"
		cancelAction := "cancel"
		if m.panel.kind != conversationPanel {
			cancelKey = "Ctrl+C"
		}
		if m.savingDefault != nil {
			cancelKey = "Esc"
			cancelAction = "cancel save"
		}
		status = m.spinner.View() + m.styles.base.Render(" "+oneline(m.activity)+" · "+cancelKey+" to "+cancelAction)
		if m.notice != "" {
			status += " · " + oneline(m.notice)
		}
		if m.pendingModel != nil {
			status += " · next: " + oneline(m.pendingModel.name) + " / " + effortLabel(m.pendingModel.profile.ReasoningEffort)
		}
	} else if strings.HasPrefix(status, "Error:") {
		status = m.styles.failure.Render(status)
	} else {
		status = m.styles.muted.Render(status)
	}
	if len(m.queuedCommands) > 0 {
		status = fmt.Sprintf("%d queued: %s · ", len(m.queuedCommands), oneline(m.queuedCommands[0].request.label())) + status
	}
	if len(m.steering) > 0 {
		status = fmt.Sprintf("%d steering · ", len(m.steering)) + status
	}
	if len(m.operationErrors) > 0 {
		status = m.styles.failure.Render(fmt.Sprintf("Error: %d failed · /queue · ", len(m.operationErrors))) + status
	}
	line := m.styles.border.Render(strings.Repeat("─", max(1, m.width-2)))
	footer := m.styles.muted.Render(keyLabel(m.submitKey.String()) + " send · " + keyLabel(m.newlineKey()) + " newline · PgUp/PgDn scroll")
	if m.busy {
		footer = m.styles.muted.Render(keyLabel(m.submitKey.String()) + " " + m.promptMode.String() + " · " + keyLabel(m.newlineKey()) + " newline · / commands · Esc/Ctrl+C cancel")
		if m.loggingIn {
			footer = m.styles.muted.Render("Esc/Ctrl+C cancel login")
		}
	}
	if m.panel.kind != conversationPanel {
		controlC := "quit"
		if m.busy {
			controlC = cancelWorkAction
		}
		footer = m.styles.muted.Render("Esc conversation · PgUp/PgDn scroll · Ctrl+C " + controlC)
	}
	if m.savingDefault != nil {
		footer = m.styles.muted.Render("Saving default · Esc/Ctrl+C cancel save")
	}
	content := m.viewport.View()
	if m.picker != nil {
		content = m.picker.list.View()
		status = m.styles.muted.Render(oneline(m.notice))
		controlC := "quit"
		if m.busy {
			controlC = cancelWorkAction
		}
		apply := "↑/↓ select · Enter apply"
		if m.picker.command == modelCommand || m.picker.command == reasoningCommand || m.picker.command == promptModeCommand {
			apply = "↑↓ · Enter apply · Ctrl+S save default"
		}
		if m.picker.command == queueSteerCommand {
			apply = "↑/↓ select · Enter steer now"
		}
		if m.picker.command == modelCommand {
			status = m.picker.list.FilterInput.View()
		}
		footer = m.styles.muted.Render(apply + " · Esc close · Ctrl+C " + controlC)
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
	mouseAction := "wheel scroll"
	if m.mouseMode == config.MouseScroll {
		mouseAction = "select text"
	}
	footer += m.styles.muted.Render(" · F4 " + mouseAction)
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
		input := &m.picker.list.FilterInput
		previousWidth := input.Width()
		m.picker.list.SetSize(m.viewport.Width(), m.viewport.Height())
		input.SetWidth(max(1, m.viewport.Width()-ansi.StringWidth(input.Prompt)-1))
		if input.Width() != previousWidth {
			// SetWidth leaves textinput's horizontal window unchanged. Rebuild it
			// around the same cursor without changing the list's ranked matches.
			value, position := input.Value(), input.Position()
			input.Reset()
			input.SetValue(value)
			input.SetCursor(position)
		}
		m.picker.list.Styles.NoItems = m.styles.muted.MaxWidth(m.viewport.Width())
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

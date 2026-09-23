package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
)

var accent = lipgloss.NewStyle().Foreground(lipgloss.Color("110"))
var muted = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
var errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("174"))

const loginDeviceCommand = "/login device"
const quitKey = "ctrl+c"

type update struct {
	event     agent.Event
	done      bool
	err       error
	loginText string
}

// Options supplies model profiles and UI-only authentication. NewGenerator
// defaults to agent.Provider and constructs a provider without generating text.
// Login must honor cancellation and send only display instructions to notify.
// LoginRequired is checked at startup and after changing profiles.
type Options struct {
	Models        map[string]config.Model
	NewGenerator  func(context.Context, config.Model) (gai.Generator, error)
	Login         func(context.Context, string, func(string)) error
	LoginRequired func(config.Model) bool
}
type model struct {
	ctx           context.Context
	agent         *agent.Agent
	name          string
	input         textarea.Model
	viewport      viewport.Model
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
}

func newModel(ctx context.Context, a *agent.Agent, name string) model {
	input := textarea.New()
	input.Placeholder = "Ask anything…"
	input.Prompt = "› "
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetHeight(3)
	input.SetWidth(80)
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.Focus()
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = accent
	m := model{ctx: ctx, agent: a, name: name, input: input, viewport: viewport.New(80, 14), spinner: spin, width: 80, height: 24, messages: a.Messages(), notice: "/help for commands"}
	m.profile = a.Model()
	m.usage, m.contextTokens = a.Usage(), a.ContextEstimate()
	m.newGenerator = agent.Provider
	m.refresh(true)
	return m
}

// Run owns the terminal until the user exits or ctx is canceled. Agent and store
// lifetimes remain the caller's responsibility.
func Run(ctx context.Context, a *agent.Agent, name string, options Options) error {
	m := newModel(ctx, a, name)
	m.login, m.needsLogin, m.profiles = options.Login, options.LoginRequired, options.Models
	if options.NewGenerator != nil {
		m.newGenerator = options.NewGenerator
	}
	if m.needsLogin != nil {
		m.loginRequired = m.needsLogin(m.profile)
	}
	if m.loginRequired {
		m.notice = "Sign in with /login to use Codex"
	}
	program := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
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
func (m model) Init() tea.Cmd            { return textarea.Blink }
func await(events <-chan update) tea.Cmd { return func() tea.Msg { return <-events } }
func (m *model) start(text string) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.busy = true
	m.provisional = ""
	m.notice = ""
	m.usageView = false
	m.activity = "Thinking"
	m.loggingIn = text == "/login" || text == loginDeviceCommand
	m.loginText = ""
	if m.loggingIn {
		m.activity = "Signing in"
	}
	m.layout()
	ch := make(chan update, 64)
	m.events = ch
	go func() {
		defer close(ch)
		var err error
		switch {
		case m.loggingIn:
			method := "browser"
			if text == loginDeviceCommand {
				method = "device"
			}
			err = m.login(ctx, method, func(text string) { ch <- update{loginText: text} })
		case text == "/compact":
			err = m.agent.Compact(ctx)
		case strings.HasPrefix(text, "/branch "):
			err = m.agent.Branch(ctx, strings.TrimSpace(strings.TrimPrefix(text, "/branch ")))
		default:
			err = m.agent.Prompt(ctx, text, func(event agent.Event) {
				// Keep durable message notifications even after cancellation. The UI drains
				// until done, so a cancel cannot strand the producer on a full channel.
				ch <- update{event: event}
			})
		}
		ch <- update{done: true, err: err}
	}()
	return tea.Batch(await(ch), m.spinner.Tick)
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		atBottom := m.viewport.AtBottom()
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
				m.loginRequired = false
				m.notice = "Signed in · credentials saved to ~/.cpe/auth.json"
			} else {
				m.notice = "Saved"
			}
			m.loggingIn = false
			m.loginText = ""
			m.layout()
			m.refresh(true)
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
		m.refresh(true)
		return m, await(m.events)
	case tea.KeyMsg:
		if m.picker != nil {
			return m.updatePicker(v)
		}
		switch v.String() {
		case quitKey, "esc":
			if m.busy {
				m.cancel()
				m.activity = "Canceling"
				return m, nil
			}
			if v.String() == quitKey {
				return m, tea.Quit
			}
			m.input.Reset()
			return m, nil
		case "ctrl+d":
			if !m.busy && m.input.Value() == "" {
				return m, tea.Quit
			}
		case "pgup", "pgdown", "ctrl+u":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case "alt+enter":
			if !m.busy {
				m.input.InsertRune('\n')
			}
			return m, nil
		case "enter":
			if m.busy {
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			m.input.Reset()
			command := strings.Fields(text)[0]
			argument := strings.TrimSpace(strings.TrimPrefix(text, command))
			if command == "/model" || command == "/reasoning" {
				m.configureModel(command, argument)
				return m, nil
			}
			switch text {
			case "/quit", "/exit":
				return m, tea.Quit
			case "/help":
				m.notice = "/model  /reasoning  /usage  /login  /compact  /tree  /branch ID  /session  /quit"
				return m, nil
			case "/usage":
				m.usageView = true
				m.notice = "Session totals · PgUp/PgDn scroll"
				m.refresh(false)
				m.viewport.GotoTop()
				return m, nil
			case "/session":
				m.notice = m.agent.SessionFile()
				return m, nil
			case "/tree":
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
				m.viewport.SetContent("Checkpoints — /branch ID to continue from one\n\n" + clean(b.String()))
				m.viewport.GotoTop()
				m.notice = "History is preserved when branching"
				return m, nil
			}
			if text == "/login" || text == loginDeviceCommand {
				if m.login == nil || m.profile.Provider != codexProvider {
					m.notice = "Select a Codex profile with /model to use /login"
					return m, nil
				}
				return m, m.start(text)
			}
			if strings.HasPrefix(text, "/") && text != "/compact" && !strings.HasPrefix(text, "/branch ") {
				m.notice = "Unknown command. /help for commands"
				return m, nil
			}
			if m.loginRequired && !strings.HasPrefix(text, "/branch ") {
				m.input.SetValue(text)
				m.notice = "Sign in with /login first"
				return m, nil
			}
			return m, m.start(text)
		}
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	if !m.busy {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}
func (m *model) refresh(bottom bool) {
	if m.usageView {
		m.viewport.SetContent(ansi.Hardwrap(m.usageDetails(), max(1, m.viewport.Width), true))
		return
	}
	if m.loggingIn && m.loginText != "" {
		m.viewport.SetContent(ansi.Hardwrap(clean(m.loginText), max(1, m.viewport.Width), true))
		return
	}
	var b strings.Builder
	if len(m.messages) == 0 {
		b.WriteString("A conversation with a persistent workspace.\n\nAsk a question or describe a change.\n")
	}
	for _, message := range m.messages {
		label := "You"
		switch message.Role {
		case gai.Assistant:
			label = "Assistant"
		case gai.ToolResult:
			label = "Starlark"
		}
		b.WriteString(accent.Render(label) + "\n")
		for _, block := range message.Blocks {
			if block.Content == nil {
				continue
			}
			text := block.Content.String()
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
			b.WriteString(clean(text))
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	if m.provisional != "" {
		b.WriteString(accent.Render("Assistant") + "\n" + clean(m.provisional))
	}
	wasBottom := m.viewport.AtBottom()
	m.viewport.SetContent(ansi.Hardwrap(b.String(), max(1, m.viewport.Width), true))
	if bottom || wasBottom {
		m.viewport.GotoBottom()
	}
}
func (m model) View() string {
	if m.width < 24 || m.height < 8 {
		return "Resize terminal to at least 24 × 8"
	}
	title := accent.Render("cpe") + "  " + oneline(m.name)
	if supportsReasoning(m.profile) {
		title += " · " + effortLabel(m.profile.ReasoningEffort)
	}
	title += "  " + muted.Render(oneline(filepath.Base(m.agent.SessionFile())))
	status := oneline(m.notice)
	if m.busy {
		status = m.spinner.View() + " " + m.activity + " · Esc to cancel"
	} else if strings.HasPrefix(status, "Error:") {
		status = errorStyle.Render(status)
	} else {
		status = muted.Render(status)
	}
	line := muted.Render(strings.Repeat("─", max(1, m.width-2)))
	footer := muted.Render("Enter send · Alt+Enter newline · PgUp/PgDn scroll · Ctrl+C quit")
	content := m.viewport.View()
	if m.picker != nil {
		content = m.picker.list.View()
		footer = muted.Render("↑/↓ select · Enter apply · Esc cancel · Ctrl+C quit")
	}
	counts, totals := m.usageLines()
	rows := []string{ansi.Truncate(title, m.width, "…"), content, ansi.Truncate(status, m.width, "…")}
	if !m.loggingIn {
		rows = append(rows, muted.Render(ansi.Truncate(counts, m.width, "…")), muted.Render(ansi.Truncate(totals, m.width, "…")))
	}
	rows = append(rows, line, m.input.View(), ansi.Truncate(footer, m.width, "…"))
	return strings.Join(rows, "\n")
}

func (m *model) layout() {
	overhead := 7
	if m.loggingIn {
		overhead = 5
	}
	m.input.SetWidth(max(1, m.width-2))
	m.input.SetHeight(min(3, max(1, m.height-overhead-1)))
	m.viewport.Width = max(1, m.width-2)
	m.viewport.Height = max(1, m.height-m.input.Height()-overhead)
	if m.picker != nil {
		m.picker.list.SetSize(m.viewport.Width, m.viewport.Height)
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

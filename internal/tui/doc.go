// Package tui presents the internal agent through Bubble Tea. A viewport holds
// accepted messages and provisional streamed text; a textarea accepts multiline
// prompts. Agent work runs off the UI loop and sends ordered events through one
// channel. Image content is represented by an [Image: MIME] placeholder; the
// model receives the actual image block rather than a base64 text dump. Each
// Starlark result previews at most 20 wrapped rows across all its blocks, followed
// by a hidden-row count. The full result remains in the agent and session; only
// presentation is shortened. Source code and assistant replies are not capped.
// Transcript tabs display as four spaces before wrapping, preview budgeting,
// and scroll anchoring; model requests and durable text retain their original tabs.
// Esc or Ctrl+C cancels active work; the program waits for reconciliation
// before allowing another operation. Ctrl+C when idle or /quit exits. Shift+Enter
// inserts a newline when the terminal reports modified keys; Ctrl+J is a fallback
// for legacy terminals. By default Enter submits. Options.SubmitKey can reverse
// Enter and Shift+Enter for the composer; Ctrl+J still inserts a newline. Pickers
// and private login input always use Enter to confirm. PgUp/PgDn scroll.
// Mouse selection belongs to the terminal by default: drag normally and use its
// Copy/Paste actions. Options.MouseMode can enable mouse capture for conversation
// wheel scrolling instead. F4 toggles capture on every input surface, including
// during work/login, without changing drafts, config, or conversation state.
// Terminal wheel behavior in selection mode is terminal-specific; PgUp/PgDn
// scroll the conversation in either mode. Mouse capture requires the terminal's
// selection override (commonly Shift+drag) to select text. CPE does not manage the
// terminal's selection or clipboard for this workflow.
// Shift+arrows select composer text; Ctrl+G selects all. Selection uses the
// theme's selection colors, including after reload. Typing, paste, and newline
// replace selected text through the textarea's editing behavior.
// Pickers and login blur the normal composer. Every return to composer input
// restores focus and schedules its cursor command, so blinking cannot depend
// on which phase was visible when a modal surface opened.
// New output follows the bottom only if it was already visible. Scrolling up
// keeps the reading position across deltas, accepted results, and completion or
// cancellation. Scrolling back to the bottom resumes following; starting a new
// operation shows the latest conversation. Width reflow anchors to the same
// logical text, and layout clamping never re-enables following on its own.
// While agent work is active, the composer remains editable and offers the full
// slash-command/skill catalog. Drafts remain unsent until explicitly submitted.
// The configured submit key queues busy prompts by default; /prompt-mode selects
// queue or steer for this conversation, and Ctrl+S in that picker also saves it.
// Shift+Enter follows this mode when configured as the submit key; newline keys
// retain their editing behavior. Skills follow the same prompt delivery mode.
// /queue steer opens a picker to promote one queued prompt by stable ID. It is
// removed once and sent ahead of remaining FIFO work. Steering cancels generation
// or tools with agent.ErrSteered and waits for reconciliation before running again.
// Several steering submissions during settlement become one ordered input batch.
// Branch restoration/login settle normally; steering runs afterward. Pending
// model selections apply before the steered generation. The latest interrupted
// stream stays in the UI with a not-saved label, never in model/session messages.
// Commands are parsed once, then dispatched independently
// of the worker: UI commands use snapshots and run immediately; model/reasoning
// changes use QueueModel at generation boundaries. Queued prompts, compaction, branching,
// and chosen login flows enter an in-memory FIFO until the current operation
// settles. The status shows pending work; /queue inspects it and /queue clear
// discards it, including pending steering, without stopping current work. Canceling active work clears the
// queue; provider failures settle and advance without retrying an invocation.
// Worker failures retain a UI-only error indicator and full messages in /queue
// until exit, including when later commands succeed. /queue clear removes only
// pending requests, not errors. Failures never become model messages.
// Default-save failures use the same reporting, distinguishing uncommitted
// writes from saved defaults that could not apply (including post-commit
// cancellation). Cancellation before a save commits is not a failure.
// /quit drops pending work and joins the active worker/default save on exit.
// A picker, private login, or pending default save holds queue advancement.
// Login secrets never enter the queue. Commands that have not run are not durable;
// skills resolve at execution. New drafts are preserved throughout. Session path
// and completed checkpoints are cached at operation boundaries; usage uses live
// events. No concurrent Agent reads are needed to render/dispatch UI commands.
//
// A leading slash on a single-line draft opens a themed command completion popup
// above the composer. Suggestions filter by prefix while the cursor is at the
// end. Up/Down cycle through all matches; at most five rows are visible. Tab
// fills the command plus a space without running it. The submit key fills and dispatches
// the selection, except /branch which waits for a checkpoint ID. Model, reasoning,
// and theme pickers still handle argument selection after dispatch.
// While idle, Escape dismisses completion without clearing the draft and suppresses it until
// the leading slash is removed or the draft is sent/cleared. An unrecognized
// slash prefix after Escape is ordinary prompt text; recognized commands retain
// their meaning. Unknown commands otherwise still report an error. Inline
// slashes, arguments, and multiline drafts do not open completion. Busy turns
// retain all commands, with Esc/Ctrl+C retaining work/queue cancellation priority.
// An open model picker uses Escape to close; Ctrl+C cancels active work or quits
// when idle. Completion and picker help reflect those active-work exceptions.
// The popup shrinks or hides when terminal space is insufficient, and hidden
// suggestions never capture navigation or the submit key. Reloading themes and resizing
// preserve the completion selection, draft and conversation scroll position.
// /skill:NAME [arguments] uses the agent's startup catalog. Completion includes
// only user-invocable skills, with sanitized descriptions. Tab leaves arguments
// editable; the submit key submits the selection to Agent.Prompt for resolution and
// persistence. The /skill: prefix remains a command after Escape, like /model.
//
// /login opens an account-provider picker; /login openai opens browser PKCE, and
// /login openai device shows an OpenAI device code. /login codex and /login device
// remain compatibility aliases. These OpenAI logins authenticate the existing
// Codex OAuth provider; model configuration and credential storage are unchanged.
// /login opencode-go opens masked API-key input,
// separate from the prompt composer, then saves the key and imports profiles.
// /login copilot displays GitHub's device authorization instructions, then imports
// the account's Copilot models. All logins save their service entry in auth.json.
// Successful import refreshes /model immediately without switching the active
// model or changing its settings. Escape/Ctrl+C cancels key entry and discards
// pending commands without exiting.
// Login work
// shares the cancellation and worker lifecycle with agent work, but sends only
// ephemeral display events. Its URLs and codes are cleared on completion and
// never enter model context or the durable conversation. Missing credentials do
// not prevent startup; successful login enables prompts without restarting.
//
// /model and /reasoning open keyboard pickers (arrows, Enter, Esc), or accept a
// profile name and effort label directly. The model picker has a focused Filter
// input below its choices. Typing/paste fuzzy-match profile names, provider names,
// and model IDs, ranked synchronously before selection. Printable list shortcuts
// belong to the query; arrows/PageUp/PageDown navigate matches, left/right edit
// the query, Backspace deletes, and Ctrl+U clears. Enter/Ctrl+S use the highlighted
// match and do nothing when none match. Escape closes immediately. The query is
// ephemeral, starts empty on open, and survives resizing/theme reload with its
// selection and cursor. Cursor timers follow the active input surface.
// /model also works during agent work:
// the current generation and its tool calls finish, then the next generation uses
// the queued provider/settings. The header shows the applied profile; status shows
// a pending selection. A later choice replaces it, including choosing the current
// profile to stay with it. No new prompt is queued. While a picker is open, Escape
// dismisses it and Ctrl+C cancels active work; paste never reaches the composer.
// /reasoning also queues settings at generation boundaries. A pending model
// selection supplies its target profile, so model/reasoning edits compose.
// Selections leave conversation and interpreter state intact. Selecting a different
// profile applies its configured defaults; selecting the same one is a no-op.
// /reasoning default restores the active profile's configured effort (or omits
// the provider option when unset). Reasoning can be set for every profile and
// appears in the header; supported labels depend on the adapter/model.
// /reasoning provider always omits the provider option, even with a saved effort.
// These commands do not enter model context or session files. Direct arguments
// and Enter in a picker do not edit config.json; restart/resume uses configuration
// and --model.
//
// Ctrl+S in /model, /reasoning, or /prompt-mode saves the highlighted choice to
// config.json off the event loop, then applies it. Composer Ctrl+S has no action.
// Saving reasoning never selects the default model. Saving a model applies that
// profile's saved settings. The provider reasoning choice removes the option.
// A default model or reasoning effort can also be saved during agent work; its selection
// queues at the same generation boundary as an ordinary model change. Only the
// agent's concurrent-safe queue is accessed until that work has completed.
// Drafts and history are preserved. ConfigDir enables this workflow; selection
// and submission are disabled during the save. Write failures preserve the
// current selection. Esc/Ctrl+C cancel an in-flight save without clearing drafts;
// agent work continues and committed writes remain saved. Shutdown cancels and
// joins both pending saves and agent work.
// Provider failures after a save report the saved default
// without replacing the active model.
//
// Two status rows display cumulative uncached input, output, cache reads/writes,
// total tokens, estimated USD cost, and estimated current context versus budget.
// Narrow terminals abbreviate the categories to I/O/R/W; /usage shows exact
// counts and explains missing reports/prices. A plus marks incomplete tokens;
// +? marks partial cost. Login temporarily hides these rows to make room for its
// instructions. Usage snapshots arrive through worker events, never concurrent
// Agent reads. Completed operations refresh totals and the context estimate.
//
// Presentation uses semantic styles from package theme. When ThemeDir is set,
// configuration and system appearance are read at startup and polled once per
// second off the event loop, including
// during generation/login. Reloads preserve drafts, cursor, scroll, pickers and
// special views. Invalid edits retain the previous theme and show a footer
// warning until a successful reload. Startup errors use terminal colors. /theme
// opens a picker of built-in and custom themes; /theme NAME applies one directly.
// Enter validates and saves the selection to themes.json before applying it;
// Escape cancels without changing the theme or file. Failed selections retain
// the current theme. Selections persist across sessions and are reflected by
// other instances through reload. Older in-flight polls cannot undo a selection.
// These commands work before login and never enter model context or the journal.
// Fonts are inherited from the terminal;
// CPE never executes desktop hooks or changes the terminal's global palette.
package tui

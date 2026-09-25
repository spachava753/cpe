// Package tui presents the internal agent through Bubble Tea. A viewport holds
// accepted messages and provisional streamed text; a textarea accepts multiline
// prompts. Agent work runs off the UI loop and sends ordered events through one
// channel. Image content is represented by an [Image: MIME] placeholder; the
// model receives the actual image block rather than a base64 text dump.
// Esc or Ctrl+C cancels active work; the program waits for reconciliation
// before allowing another operation. Ctrl+C when idle or /quit exits. Alt+Enter
// inserts a newline, Enter submits, and PgUp/PgDn scroll the conversation.
// While agent work is active, the composer remains editable for the next draft,
// including paste and Alt+Enter. Enter neither submits nor queues it until work
// finishes. Completion, errors, and cancellation preserve the draft and cursor;
// slash completion resumes when idle. During login, the normal composer stays
// locked and private credential input retains its separate routing.
//
// A leading slash on a single-line draft opens a themed command completion popup
// above the composer. Suggestions filter by prefix while the cursor is at the
// end. Up/Down cycle through all matches; at most five rows are visible. Tab
// fills the command plus a space without running it. Enter fills and dispatches
// the selection, except /branch which waits for a checkpoint ID. Model, reasoning,
// and theme pickers still handle argument selection after dispatch.
// Escape dismisses completion without clearing the draft and suppresses it until
// the leading slash is removed or the draft is sent/cleared. An unrecognized
// slash prefix after Escape is ordinary prompt text; recognized commands retain
// their meaning. Unknown commands otherwise still report an error. Inline
// slashes, arguments, multiline drafts and busy turns do not open completion.
// The popup shrinks or hides when terminal space is insufficient, and hidden
// suggestions never capture navigation or Enter. Reloading themes and resizing
// preserve the completion selection, draft and conversation scroll position.
// /skill:NAME [arguments] uses the agent's startup catalog. Completion includes
// only user-invocable skills, with sanitized descriptions. Tab leaves arguments
// editable; Enter submits the selection to Agent.Prompt for resolution and
// persistence. The /skill: prefix remains a command after Escape, like /model.
//
// /login opens a provider picker; /login codex opens browser PKCE, and /login
// device shows a Codex device code. /login opencode-go opens masked API-key input,
// separate from the prompt composer, then saves the key and imports profiles.
// Successful import refreshes /model immediately without switching the active
// model or changing its settings. Escape/Ctrl+C cancels key entry without exiting.
// Login work
// shares the cancellation and worker lifecycle with agent work, but sends only
// ephemeral display events. Its URLs and codes are cleared on completion and
// never enter model context or the durable conversation. Missing credentials do
// not prevent startup; successful login enables prompts without restarting.
//
// /model and /reasoning open keyboard pickers (arrows, Enter, Esc), or accept a
// profile name and effort label directly. Changes are allowed only between turns
// and leave conversation and interpreter state intact. Selecting a different
// profile applies its configured defaults; selecting the same one is a no-op.
// /reasoning default restores the active profile's configured effort (or omits
// the provider option when unset). Reasoning can be set for every profile and
// appears in the header; supported labels depend on the adapter/model. These
// commands do not enter model context or session files,
// and do not edit config.json; restart/resume uses configuration and --model.
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

// Package tui presents the internal agent through Bubble Tea. A viewport holds
// accepted messages and provisional streamed text; a textarea accepts multiline
// prompts. Agent work runs off the UI loop and sends ordered events through one
// channel. Esc or Ctrl+C cancels active work; the program waits for reconciliation
// before allowing another operation. Ctrl+C when idle or /quit exits. Alt+Enter
// inserts a newline, Enter submits, and PgUp/PgDn scroll the conversation.
//
// /login opens a browser PKCE flow; /login device shows a device code. Login work
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
// the provider option when unset). Only Codex and Responses profiles support
// reasoning effort. These commands do not enter model context or session files,
// and do not edit config.json; restart/resume uses configuration and --model.
//
// Two status rows display cumulative uncached input, output, cache reads/writes,
// total tokens, estimated USD cost, and estimated current context versus budget.
// Narrow terminals abbreviate the categories to I/O/R/W; /usage shows exact
// counts and explains missing reports/prices. A plus marks incomplete tokens;
// +? marks partial cost. Login temporarily hides these rows to make room for its
// instructions. Usage snapshots arrive through worker events, never concurrent
// Agent reads. Completed operations refresh totals and the context estimate.
package tui

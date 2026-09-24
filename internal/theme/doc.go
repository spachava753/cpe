// Package theme resolves ~/.cpe/themes.json independently of model settings.
// The file selects an active built-in or an entry from an optional themes object.
// Built-ins are desktop, terminal, light, dark, nord, dracula, and gruvbox (dark).
// Fixed presets are embedded in the binary. User definitions take precedence
// over built-ins with the same name and require a source: system, terminal, file,
// a fixed preset name, or auto (an alias for system). A missing themes.json uses
// StarterJSON. JSON decoding rejects unknown fields, duplicate keys and trailing
// data. All definitions are structurally validated; only the active definition
// reads an explicit palette file and resolves references.
//
// Desktop uses source system. SystemAppearance reads the local OS preference:
// on Linux, org.freedesktop.portal.Settings.ReadAll on the session D-Bus provides
// color-scheme and optional accent-color; on macOS, a fixed script uses AppKit's
// effectiveAppearance and controlAccentColor through /usr/bin/osascript. This
// requires no CGO or developer SDK. It sends no automation events to other apps.
// There are no distribution names, desktop theme paths, or desktop hooks in the
// detector. Linux needs a running Settings portal backend; no session bus is
// started by CPE. Probes honor cancellation and have a one-second deadline.
// SSH sessions and unsupported/unavailable services yield an empty snapshot.
//
// The OS supplies appearance preferences, not a complete terminal palette. CPE
// derives neutral light/dark surfaces and semantic roles from the mode, applies
// an available accent to headings and selection, and adjusts it for readable
// contrast. Missing/invalid accents use the preset accent; unknown mode falls
// back to terminal inheritance. Linux color-scheme 0 means no preference, not
// light. Out-of-range, non-finite, or incorrectly typed accent values are ignored.
// A failed probe also falls back to terminal inheritance, with later polls able
// to recover. Load and Select consume explicit Appearance snapshots and never
// call OS services themselves, keeping system I/O off the TUI event loop.
//
// Terminal uses the terminal's default foreground/background and ANSI slots,
// regardless of the OS preference. It follows the emulator's palette even over
// SSH or tmux; matching a desktop's entire color scheme depends on the emulator
// following it. CPE never changes the emulator's palette or executes user scripts.
//
// Source file requires palette_file: an explicit JSON object of #rrggbb colors.
// Absolute paths, ~/ paths, and paths relative to ~/.cpe are supported, including
// symlinks. Foreground/background are required (aliases fg/bg or color7/color0);
// semantic names and color0..color15 are accepted. No files are auto-discovered.
// The old Omarchy source and automatic TOML discovery have been removed; use
// system for OS appearance or export a JSON palette for source file.
//
// colors overrides foreground, background, accent, muted, border, error, user,
// assistant, tool, selection_foreground, and selection_background. Values are
// #rrggbb, ANSI indices as strings, "default", or $palette_key references to the
// source palette before overrides. Every source has semantic and color0..color15
// keys. Bold defaults to true for headings/selection, italic to false for muted
// text, and input_height to three rows (1–20, limited by terminal height).
// Font size and family are controlled by the terminal emulator.
//
// The TUI polls configuration and appearance once per second, preserving draft,
// scroll and conversation state. Invalid configuration retains the last valid
// theme and shows a warning; startup errors use Default. Names lists choices
// without resolving palettes. Select validates the chosen palette before saving
// active atomically, preserving custom definitions and file symlinks. Invalid
// selections or write failures keep the old file. Theme presentation never
// changes conversation or interpreter state.
package theme

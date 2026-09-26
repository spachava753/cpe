package config

import (
	"encoding/json"
	"fmt"
)

// tui configures interactive input independently of model generation.
type tui struct {
	SubmitKey SubmitKey `json:"submit_key"`
	MouseMode MouseMode `json:"mouse_mode"`
}

// MouseMode chooses who owns mouse events. Its zero value leaves selection to
// the terminal; MouseScroll captures events for CPE's conversation scrolling.
type MouseMode uint8

const (
	MouseSelect MouseMode = iota
	MouseScroll
)

func (m MouseMode) String() string {
	if m == MouseScroll {
		return "scroll"
	}
	return "select"
}

func (m MouseMode) MarshalJSON() ([]byte, error) { return json.Marshal(m.String()) }

func (m *MouseMode) UnmarshalJSON(data []byte) error {
	var mode string
	if err := json.Unmarshal(data, &mode); err != nil {
		return fmt.Errorf("tui.mouse_mode: %w", err)
	}
	switch mode {
	case "select":
		*m = MouseSelect
	case "scroll":
		*m = MouseScroll
	default:
		return fmt.Errorf("tui.mouse_mode must be select or scroll, got %q", mode)
	}
	return nil
}

// SubmitKey selects the composer submission key. Its zero value is Enter;
// the other Enter combination inserts a newline. Pickers keep Enter to confirm.
type SubmitKey uint8

const (
	SubmitEnter SubmitKey = iota
	SubmitShiftEnter
)

func (k SubmitKey) String() string {
	if k == SubmitShiftEnter {
		return "shift+enter"
	}
	return "enter"
}

func (k SubmitKey) MarshalJSON() ([]byte, error) { return json.Marshal(k.String()) }

func (k *SubmitKey) UnmarshalJSON(data []byte) error {
	var key string
	if err := json.Unmarshal(data, &key); err != nil {
		return fmt.Errorf("tui.submit_key: %w", err)
	}
	switch key {
	case "enter":
		*k = SubmitEnter
	case "shift+enter":
		*k = SubmitShiftEnter
	default:
		return fmt.Errorf("tui.submit_key must be enter or shift+enter, got %q", key)
	}
	return nil
}

package config

import (
	"encoding/json"
	"fmt"
)

// tui configures interactive input independently of model generation.
type tui struct {
	SubmitKey SubmitKey `json:"submit_key"`
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

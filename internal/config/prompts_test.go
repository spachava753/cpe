package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePromptMode(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        PromptMode
		bad         bool
	}{
		{"queue", "queue", PromptQueue, false}, {"steer", "steer", PromptSteer, false}, {"empty", "", 0, true}, {"case", "Steer", 0, true}, {"spaces", " queue", 0, true}, {"unknown", "both", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePromptMode(tc.input)
			if (err != nil) != tc.bad || !tc.bad && got != tc.want {
				t.Fatal(got, err)
			}
		})
	}
}

func TestPromptModeUnmarshalJSON(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        PromptMode
		bad         bool
	}{
		{"queue", `"queue"`, PromptQueue, false}, {"steer", `"steer"`, PromptSteer, false}, {"null", `null`, 0, true}, {"number", `1`, 0, true}, {"bool", `true`, 0, true}, {"empty", `""`, 0, true}, {"unknown", `"other"`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode := PromptSteer
			err := json.Unmarshal([]byte(tc.input), &mode)
			if (err != nil) != tc.bad || !tc.bad && mode != tc.want {
				t.Fatal(mode, err)
			}
			if tc.bad && mode != PromptSteer {
				t.Fatal("invalid input changed setting")
			}
		})
	}
}

func TestSavePromptMode(t *testing.T) {
	for _, tc := range []struct {
		name              string
		mode              PromptMode
		canceled, invalid bool
	}{
		{name: "explicit default", mode: PromptQueue}, {name: "steer", mode: PromptSteer}, {name: "canceled", mode: PromptSteer, canceled: true}, {name: "invalid domain", mode: PromptMode(99), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := defaultFixture(t)
			path := filepath.Join(dir, "config.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			cfg, err := SavePromptMode(ctx, dir, tc.mode)
			if (err != nil) != (tc.canceled || tc.invalid) {
				t.Fatal(err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err != nil {
				if string(after) != string(before) {
					t.Fatal("failed save changed config")
				}
				return
			}
			restored, err := load(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.TUI.PromptMode != tc.mode || restored.TUI.PromptMode != tc.mode || !strings.Contains(string(after), `"prompt_mode"`) {
				t.Fatal("mode not saved explicitly")
			}
			if cfg.DefaultModel != "alpha" || cfg.Models["beta"].ReasoningEffort != "high" || cfg.TUI.SubmitKey != SubmitShiftEnter || cfg.TUI.MouseMode != MouseScroll || cfg.Agent.MaxRounds != 7 {
				t.Fatal("save altered other settings")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0640 {
				t.Fatal("permissions changed", err)
			}
		})
	}
}

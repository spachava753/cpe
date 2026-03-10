package config

import (
	"strings"
	"testing"
)

func uint64ptr(v uint64) *uint64 {
	return &v
}

func baseRawConfigForFlightRecorder() *RawConfig {
	return &RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test Model",
				ID:            "test-id",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 200000,
				MaxOutput:     64000,
			},
		}},
		Defaults: Defaults{Model: "test-model"},
	}
}

func TestResolveFlightRecorder_DefaultBuiltins(t *testing.T) {
	t.Parallel()

	cfg, err := resolveFromRaw(baseRawConfigForFlightRecorder(), RuntimeOptions{}, "")
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}
	if cfg.FlightRecorder == nil {
		t.Fatal("expected flight recorder config, got nil")
	}
	if !cfg.FlightRecorder.Enabled {
		t.Fatal("expected built-in flight recorder to be enabled")
	}
	if cfg.FlightRecorder.MinAge != DefaultFlightRecorderMinAge {
		t.Fatalf("MinAge = %v, want %v", cfg.FlightRecorder.MinAge, DefaultFlightRecorderMinAge)
	}
	if cfg.FlightRecorder.MaxBytes != DefaultFlightRecorderMaxBytes {
		t.Fatalf("MaxBytes = %d, want %d", cfg.FlightRecorder.MaxBytes, DefaultFlightRecorderMaxBytes)
	}
}

func TestResolveFlightRecorder_DefaultsConfig(t *testing.T) {
	t.Parallel()

	rawCfg := baseRawConfigForFlightRecorder()
	rawCfg.Defaults.FlightRecorder = &FlightRecorderConfig{
		Enabled:  true,
		MinAge:   "3s",
		MaxBytes: uint64ptr(2048),
	}

	cfg, err := resolveFromRaw(rawCfg, RuntimeOptions{}, "")
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}
	if cfg.FlightRecorder == nil {
		t.Fatal("expected flight recorder config, got nil")
	}
	if !cfg.FlightRecorder.Enabled {
		t.Fatal("expected configured flight recorder to be enabled")
	}
	if got := cfg.FlightRecorder.MinAge.String(); got != "3s" {
		t.Fatalf("MinAge = %s, want 3s", got)
	}
	if cfg.FlightRecorder.MaxBytes != 2048 {
		t.Fatalf("MaxBytes = %d, want 2048", cfg.FlightRecorder.MaxBytes)
	}
}

func TestResolveFlightRecorder_ModelOverride(t *testing.T) {
	t.Parallel()

	rawCfg := baseRawConfigForFlightRecorder()
	rawCfg.Defaults.FlightRecorder = &FlightRecorderConfig{Enabled: true, MinAge: "3s", MaxBytes: uint64ptr(2048)}
	rawCfg.Models[0].FlightRecorder = &FlightRecorderConfig{Enabled: false}

	cfg, err := resolveFromRaw(rawCfg, RuntimeOptions{}, "")
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}
	if cfg.FlightRecorder == nil {
		t.Fatal("expected flight recorder config, got nil")
	}
	if cfg.FlightRecorder.Enabled {
		t.Fatal("expected model flight recorder override to disable the recorder")
	}
	if cfg.FlightRecorder.MinAge != DefaultFlightRecorderMinAge {
		t.Fatalf("MinAge = %v, want %v", cfg.FlightRecorder.MinAge, DefaultFlightRecorderMinAge)
	}
	if cfg.FlightRecorder.MaxBytes != DefaultFlightRecorderMaxBytes {
		t.Fatalf("MaxBytes = %d, want %d", cfg.FlightRecorder.MaxBytes, DefaultFlightRecorderMaxBytes)
	}
}

func TestValidateWithConfigPath_InvalidFlightRecorderConfig(t *testing.T) {
	t.Parallel()

	rawCfg := baseRawConfigForFlightRecorder()
	rawCfg.Defaults.FlightRecorder = &FlightRecorderConfig{Enabled: true, MinAge: "not-a-duration"}

	err := rawCfg.ValidateWithConfigPath("")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	wantPrefix := "defaults.flightRecorder: minAge: invalid duration \"not-a-duration\""
	if !strings.Contains(err.Error(), wantPrefix) {
		t.Fatalf("unexpected error: got %q want substring %q", err.Error(), wantPrefix)
	}
}

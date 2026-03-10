package config

import (
	"fmt"
	"time"
)

const (
	defaultFlightRecorderEnabled  = true
	DefaultFlightRecorderMinAge   = 10 * time.Second
	DefaultFlightRecorderMaxBytes = 8 << 20
)

// ResolvedFlightRecorderConfig is the runtime-ready flight recorder
// configuration used by generator middleware.
type ResolvedFlightRecorderConfig struct {
	Enabled  bool
	MinAge   time.Duration
	MaxBytes uint64
}

func resolveFlightRecorder(model ModelConfig, defaults Defaults) (*ResolvedFlightRecorderConfig, error) {
	switch {
	case model.FlightRecorder != nil:
		return compileFlightRecorderConfig(model.FlightRecorder)
	case defaults.FlightRecorder != nil:
		return compileFlightRecorderConfig(defaults.FlightRecorder)
	default:
		return &ResolvedFlightRecorderConfig{
			Enabled:  defaultFlightRecorderEnabled,
			MinAge:   DefaultFlightRecorderMinAge,
			MaxBytes: DefaultFlightRecorderMaxBytes,
		}, nil
	}
}

func validateFlightRecorderConfig(cfg *FlightRecorderConfig, fieldPrefix string) error {
	if _, err := compileFlightRecorderConfig(cfg); err != nil {
		return fmt.Errorf("%s: %w", fieldPrefix, err)
	}
	return nil
}

func compileFlightRecorderConfig(cfg *FlightRecorderConfig) (*ResolvedFlightRecorderConfig, error) {
	if cfg == nil {
		return nil, nil
	}

	minAge := DefaultFlightRecorderMinAge
	if cfg.MinAge != "" {
		parsedMinAge, err := time.ParseDuration(cfg.MinAge)
		if err != nil {
			return nil, fmt.Errorf("minAge: invalid duration %q: %w", cfg.MinAge, err)
		}
		minAge = parsedMinAge
	}

	maxBytes := uint64(DefaultFlightRecorderMaxBytes)
	if cfg.MaxBytes != nil {
		maxBytes = *cfg.MaxBytes
	}

	return &ResolvedFlightRecorderConfig{
		Enabled:  cfg.Enabled,
		MinAge:   minAge,
		MaxBytes: maxBytes,
	}, nil
}

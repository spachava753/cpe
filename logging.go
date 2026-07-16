package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	cpelogging "github.com/spachava753/cpe/internal/logging"
)

const logFilename = ".cpe.log"

func newProcessLogger(output io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(cpelogging.NewProcessHandler(handler))
}

func openLogFile() (*os.File, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user config directory: %w", err)
	}

	logDir := filepath.Join(configDir, "cpe")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory %s: %w", logDir, err)
	}

	logPath := filepath.Join(logDir, logFilename)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", logPath, err)
	}
	return logFile, nil
}

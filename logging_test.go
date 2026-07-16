package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	cpelogging "github.com/spachava753/cpe/internal/logging"
)

func TestOpenLogFileUsesCPEUserConfigDirectory(t *testing.T) {
	testDir := t.TempDir()
	switch runtime.GOOS {
	case "darwin":
		t.Setenv("HOME", testDir)
	case "windows":
		t.Setenv("AppData", testDir)
	default:
		t.Setenv("XDG_CONFIG_HOME", testDir)
	}
	logFile, err := openLogFile()
	if err != nil {
		t.Fatalf("openLogFile() error = %v", err)
	}
	logger := slog.New(slog.NewJSONHandler(logFile, nil))
	logger.Info("test log record", "source", "test")
	if err := logFile.Close(); err != nil {
		t.Fatalf("close log file: %v", err)
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error = %v", err)
	}
	logPath := filepath.Join(configDir, "cpe", logFilename)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file %s: %v", logPath, err)
	}
	var record struct {
		Message string `json:"msg"`
		Source  string `json:"source"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode log record %q: %v", data, err)
	}
	if record.Message != "test log record" || record.Source != "test" {
		t.Fatalf("unexpected log record: %#v", record)
	}
}

func TestProcessLoggerIncludesPIDAndContextAttributes(t *testing.T) {
	var output bytes.Buffer
	ctx := cpelogging.WithAttrs(
		t.Context(),
		slog.String("session_id", "session-1"),
		slog.String("cwd", "/repo"),
	)

	newProcessLogger(&output).InfoContext(ctx, "test log record")

	var record struct {
		PID       int    `json:"pid"`
		SessionID string `json:"session_id"`
		Cwd       string `json:"cwd"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode process log: %v", err)
	}
	if record.PID != os.Getpid() || record.SessionID != "session-1" || record.Cwd != "/repo" {
		t.Fatalf("process log attributes = %#v", record)
	}
}

func TestJSONLoggingSupportsIndependentAppendWriters(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), logFilename)
	const writerCount = 4
	const recordsPerWriter = 250

	files := make([]*os.File, writerCount)
	loggers := make([]*slog.Logger, writerCount)
	for writerID := range writerCount {
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatalf("open writer %d: %v", writerID, err)
		}
		files[writerID] = logFile
		loggers[writerID] = slog.New(slog.NewJSONHandler(logFile, nil))
	}

	// Separate handlers and file descriptors share no in-process write lock,
	// matching the relevant behavior of separate CPE processes.
	start := make(chan struct{})
	var writers sync.WaitGroup
	for writerID := range writerCount {
		writers.Go(func() {
			<-start
			for sequence := range recordsPerWriter {
				loggers[writerID].Info("entry", "writer", writerID, "sequence", sequence)
			}
		})
	}
	close(start)
	writers.Wait()
	for writerID, logFile := range files {
		if err := logFile.Close(); err != nil {
			t.Fatalf("close writer %d: %v", writerID, err)
		}
	}

	logFile, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open combined log: %v", err)
	}
	defer logFile.Close()

	seen := make(map[[2]int]bool, writerCount*recordsPerWriter)
	scanner := bufio.NewScanner(logFile)
	for scanner.Scan() {
		var record struct {
			Message  string `json:"msg"`
			Writer   int    `json:"writer"`
			Sequence int    `json:"sequence"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode log line %q: %v", scanner.Bytes(), err)
		}
		if record.Message != "entry" {
			t.Fatalf("unexpected log message %q", record.Message)
		}
		key := [2]int{record.Writer, record.Sequence}
		if seen[key] {
			t.Fatalf("duplicate log record for writer %d sequence %d", record.Writer, record.Sequence)
		}
		seen[key] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan combined log: %v", err)
	}
	if got, want := len(seen), writerCount*recordsPerWriter; got != want {
		t.Fatalf("combined log contains %d records, want %d", got, want)
	}
	for writerID := range writerCount {
		for sequence := range recordsPerWriter {
			if !seen[[2]int{writerID, sequence}] {
				t.Fatalf("missing log record for writer %d sequence %d", writerID, sequence)
			}
		}
	}
}

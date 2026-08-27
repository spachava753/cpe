package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spachava753/cpe/internal/cmd"
	cpelogging "github.com/spachava753/cpe/internal/logging"
)

func main() {
	logOutput := io.Discard
	logFile, err := openLogFile()
	if err != nil {
		// Best-effort: keep running but inform the user once and discard logs.
		fmt.Fprintf(os.Stderr, "warning: failed to initialize CPE logging: %v. logging will be discarded.\n", err)
	} else {
		logOutput = logFile
		defer func() { _ = logFile.Close() }()
	}

	handler := slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(cpelogging.NewProcessHandler(handler)))
	cmd.Execute()
}

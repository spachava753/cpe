package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spachava753/cpe/internal/cmd"
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

	slog.SetDefault(newProcessLogger(logOutput))
	cmd.Execute()
}

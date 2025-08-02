package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spachava753/cpe/cmd"
)

func main() {
	// Initialize slog default logger to write JSON to ./.cpe.log
	if f, err := os.OpenFile(".cpe.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		h := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
		slog.SetDefault(slog.New(h))
	} else {
		// Best-effort: keep running but inform user once via stderr, and discard logs
		slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})))
		fmt.Fprintf(os.Stderr, "warning: failed to initialize .cpe.log: %v. logging will be discarded.\n", err)
	}

	cmd.Execute()
}

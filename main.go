package main

import (
	"context"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spachava753/cpe/internal/cli"
	"github.com/spachava753/cpe/internal/version"
)

func main() {
	if err := fang.Execute(
		context.Background(),
		cli.Root(version.Get()),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}

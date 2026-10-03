package main

import (
	"context"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/cli"
	"github.com/spachava753/cpe/internal/skills"
	"github.com/spachava753/cpe/internal/version"
)

func main() {
	// Source acquisition remains unavailable until a client is implemented.
	manager := skills.NewManager(nil, afero.NewOsFs())
	if err := fang.Execute(
		context.Background(),
		cli.Root(version.Get(), manager),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}

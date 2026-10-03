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
	manager := skills.NewManager(skills.GitSourceClient{}, afero.NewOsFs())
	if err := fang.Execute(
		context.Background(),
		cli.Root(version.Get(), manager),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}

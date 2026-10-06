package main

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/cli"
	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/skills"
	"github.com/spachava753/cpe/internal/terminaltext"
	"github.com/spachava753/cpe/internal/version"
)

func main() {
	skillManager := skills.NewManager(
		skills.GitSourceClient{},
		afero.NewOsFs(),
	)
	pluginManager := plugins.NewManager(
		plugins.GitSourceClient{},
		afero.NewOsFs(),
	)
	if err := fang.Execute(
		context.Background(),
		cli.Root(
			version.Get(),
			skillManager,
			pluginManager,
		),
		fang.WithNotifySignal(os.Interrupt),
		fang.WithErrorHandler(func(output io.Writer, styles fang.Styles, err error) {
			// Sanitize only the displayed text; Fang returns the original error.
			fang.DefaultErrorHandler(
				output,
				styles,
				errors.New(terminaltext.Sanitize(err.Error())),
			)
		}),
	); err != nil {
		os.Exit(1)
	}
}

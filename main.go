package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"

	"github.com/charmbracelet/fang"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/cli"
	"github.com/spachava753/cpe/internal/skills"
	"github.com/spachava753/cpe/internal/version"
)

func main() {
	manager := skills.NewManager(
		skills.GitSourceClient{},
		afero.NewOsFs(),
	)
	if err := fang.Execute(
		context.Background(),
		cli.Root(
			version.Get(),
			manager,
		),
		fang.WithNotifySignal(os.Interrupt),
		fang.WithErrorHandler(func(output io.Writer, styles fang.Styles, err error) {
			// Escape only the displayed text; Fang returns the original error.
			quoted := strconv.Quote(err.Error())
			fang.DefaultErrorHandler(
				output,
				styles,
				errors.New(quoted[1:len(quoted)-1]),
			)
		}),
	); err != nil {
		os.Exit(1)
	}
}

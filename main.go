package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/fang"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/cli"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/skills"
	"github.com/spachava753/cpe/internal/source"
	"github.com/spachava753/cpe/internal/terminaltext"
	"github.com/spachava753/cpe/internal/version"
)

func main() {
	filesystem := afero.NewOsFs()
	authStore := auth.NewStore(filesystem)
	var authPath string
	if directory, err := config.Path(); err == nil {
		authPath = filepath.Join(
			directory,
			"auth.json",
		)
	}
	// Auth operations reject an unconfigured path; help and project commands need no home directory.
	authOptions := auth.LoginOptions{
		Path: authPath,
		Client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	skillManager := skills.NewManager(
		source.GitClient{},
		afero.NewOsFs(),
	)
	pluginManager := plugins.NewManager(
		source.GitClient{},
		afero.NewOsFs(),
	)
	if err := fang.Execute(
		context.Background(),
		cli.Root(
			version.Get(),
			skillManager,
			pluginManager,
			authStore,
			authOptions,
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

package acp

import (
	"io"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

type RunOptions struct {
	RawConfig *config.RawConfig
	Store     *storage.Sqlite
	Stderr    io.Writer
}

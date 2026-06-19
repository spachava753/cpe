package acp

import "io"

type RunOptions struct {
	ConfigPath string
	DbPath     string
	Stderr     io.Writer
}

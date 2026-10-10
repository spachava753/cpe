package source

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
	"time"
)

func readGitTreeHash(ctx context.Context, checkout string, directory string) (string, error) {
	if !fs.ValidPath(directory) || strings.ContainsAny(
		directory,
		"\\\x00",
	) {
		return "", errors.New("invalid Git tree directory")
	}
	object := "HEAD:" + directory
	if directory == "." {
		object = "HEAD^{tree}"
	}
	typeName, err := gitMetadata(
		ctx,
		checkout,
		"cat-file",
		"-t",
		object,
	)
	if err != nil {
		return "", err
	}
	if typeName != "tree" {
		return "", errors.New("git source path is not a tree")
	}
	return gitMetadata(
		ctx,
		checkout,
		"rev-parse",
		"--verify",
		object,
	)
}

func gitMetadata(ctx context.Context, checkout string, args ...string) (string, error) {
	command := exec.CommandContext(
		ctx,
		"git",
		append(
			[]string{
				"-C",
				checkout,
			},
			args...,
		)...,
	)
	configureGitCancellation(command)
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return "", fmt.Errorf(
			"read Git tree metadata: %w",
			err,
		)
	}
	return strings.TrimSpace(string(output)), nil
}

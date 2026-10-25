package agent

import (
	"context"
	_ "embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/spf13/afero"
)

//go:embed project_instructions.go.tmpl
var projectInstructionsText string

var projectInstructionsTemplate = template.Must(template.New("project instructions").Parse(projectInstructionsText))

type projectInstructions struct {
	Root    string
	Content string
	Nested  []string
}

func loadProjectInstructions(ctx context.Context, filesystem afero.Fs, cwd string) (string, error) {
	paths, err := projectInstructionPaths(
		ctx,
		filesystem,
		cwd,
	)
	if err != nil {
		return "", fmt.Errorf(
			"discover project instructions: %w",
			err,
		)
	}
	if len(paths) == 0 {
		return "", nil
	}
	slices.Sort(paths)
	root := filepath.Join(
		cwd,
		"AGENTS.md",
	)
	var instructions projectInstructions
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if path != root {
			instructions.Nested = append(
				instructions.Nested,
				path,
			)
			continue
		}
		content, err := afero.ReadFile(
			filesystem,
			path,
		)
		if err != nil {
			return "", fmt.Errorf(
				"read project instructions %q: %w",
				path,
				err,
			)
		}
		instructions.Root = path
		instructions.Content = string(content)
	}
	var text strings.Builder
	text.WriteString("\n\n")
	if err := projectInstructionsTemplate.Execute(
		&text,
		instructions,
	); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return text.String(), nil
}

func projectInstructionPaths(ctx context.Context, filesystem afero.Fs, directory string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, readErr := afero.ReadDir(
		filesystem,
		directory,
	)
	if readErr != nil {
		return nil, fmt.Errorf(
			"read directory %q: %w",
			directory,
			readErr,
		)
	}
	var paths []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(
			directory,
			entry.Name(),
		)
		if entry.IsDir() {
			if entry.Name() == ".git" {
				continue
			}
			nested, err := projectInstructionPaths(
				ctx,
				filesystem,
				path,
			)
			if err != nil {
				return nil, err
			}
			paths = append(
				paths,
				nested...,
			)
			continue
		}
		if entry.Name() != "AGENTS.md" {
			continue
		}
		// Inspect file links without following directory links and creating scan cycles.
		if entry.Mode()&fs.ModeSymlink != 0 {
			entry, readErr = filesystem.Stat(path)
			if readErr != nil {
				return nil, readErr
			}
		}
		if entry.Mode().IsRegular() {
			paths = append(
				paths,
				path,
			)
		}
	}
	return paths, ctx.Err()
}

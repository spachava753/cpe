package plugins

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/source"
)

func (m *Manager) acquire(ctx context.Context, locator string) (source.Checkout, error) {
	var client source.Client
	if m != nil {
		client = m.sources
	}
	return source.Acquire(
		ctx,
		client,
		locator,
	)
}

// Discovery recognizes native manifests by schema, not by filename alone.
// Nested directories inside a recognized package are resources, not plugins.
func discover(ctx context.Context, filesystem fs.FS, opts DiscoveryOptions, names []string) ([]Plugin, error) {
	var result []Plugin
	err := fs.WalkDir(
		filesystem,
		".",
		func(directory string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() {
				return nil
			}
			if excludedDirectory(entry.Name()) {
				return fs.SkipDir
			}
			if !opts.FullDepth && directory != "." && len(strings.Split(
				directory,
				"/",
			)) > 5 {
				return fs.SkipDir
			}
			entries, err := fs.ReadDir(
				filesystem,
				directory,
			)
			if err != nil {
				return err
			}
			for _, child := range entries {
				if child.Name() != "plugin.json" {
					continue
				}
				if err := bundlefs.CheckSourceDirectory(
					filesystem,
					directory,
				); err != nil {
					return err
				}
				pkg := packageFS{
					root: sourceFS{
						FS: filesystem,
					},
					directory: directory,
				}
				data, manifestErr := fs.ReadFile(
					pkg,
					"plugin.json",
				)
				if manifestErr != nil {
					return manifestErr
				}
				fields, manifestErr := jsonObject(data)
				if manifestErr != nil {
					return fmt.Errorf(
						"%s/plugin.json: %w",
						directory,
						manifestErr,
					)
				}
				schema, manifestErr := jsonString(fields["$schema"])
				if manifestErr != nil || schema != manifestSchema {
					continue
				}
				if len(names) > 0 {
					name, err := jsonString(fields["name"])
					if err == nil && !slices.Contains(
						names,
						name,
					) {
						return fs.SkipDir
					}
				}
				plugin, manifestErr := readPlugin(
					filesystem,
					directory,
				)
				if manifestErr != nil {
					return manifestErr
				}
				result = append(
					result,
					plugin,
				)
				return fs.SkipDir
			}
			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"discover source plugins: %w",
			err,
		)
	}
	return result, nil
}

func excludedDirectory(name string) bool { return name == ".git" || name == "node_modules" }

// Preserve optional ReadLinkFS support when adapting an opaque acquired tree.
type sourceFS struct{ fs.FS }

func (s sourceFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(
		s.FS,
		name,
	)
}

func (s sourceFS) ReadLink(name string) (string, error) {
	if reader, ok := s.FS.(fs.ReadLinkFS); ok {
		return reader.ReadLink(name)
	}
	return "", fs.ErrPermission
}

func (s sourceFS) Lstat(name string) (fs.FileInfo, error) {
	return fs.Lstat(
		s.FS,
		name,
	)
}

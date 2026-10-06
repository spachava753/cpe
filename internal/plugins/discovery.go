package plugins

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

func (m *Manager) acquire(ctx context.Context, source string) (Checkout, error) {
	if err := ctx.Err(); err != nil {
		return Checkout{}, err
	}
	if m == nil || m.sources == nil {
		return Checkout{}, errors.New("source client is unavailable")
	}
	if strings.TrimSpace(source) == "" {
		return Checkout{}, errors.New("source is required")
	}
	checkout, err := m.sources.Fetch(
		ctx,
		source,
	)
	if err != nil {
		return Checkout{}, fmt.Errorf(
			"fetch source: %w",
			err,
		)
	}
	if checkout.FS == nil {
		return Checkout{}, errors.Join(
			errors.New("source filesystem is unavailable"),
			releaseCheckout(checkout),
		)
	}
	return checkout, nil
}

func releaseCheckout(checkout Checkout) error {
	if checkout.Cleanup != nil {
		return checkout.Cleanup()
	}
	return nil
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
				if err := ensureSourceDirectory(
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
				data, err := fs.ReadFile(
					pkg,
					"plugin.json",
				)
				if err != nil {
					return err
				}
				fields, err := jsonObject(data)
				if err != nil {
					return fmt.Errorf(
						"%s/plugin.json: %w",
						directory,
						err,
					)
				}
				schema, err := jsonString(fields["$schema"])
				if err != nil || schema != manifestSchema {
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
				plugin, err := readPlugin(
					filesystem,
					directory,
				)
				if err != nil {
					return err
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

// Inspect parent entries so no directory symlink can redirect source selection.
func ensureSourceDirectory(filesystem fs.FS, directory string) error {
	if !fs.ValidPath(directory) || strings.ContainsAny(
		directory,
		"\\\x00",
	) {
		return fmt.Errorf(
			"invalid source directory %q",
			directory,
		)
	}
	if directory == "." {
		return nil
	}
	parent := "."
	for component := range strings.SplitSeq(
		directory,
		"/",
	) {
		entries, err := fs.ReadDir(
			filesystem,
			parent,
		)
		if err != nil {
			return err
		}
		found := false
		for _, entry := range entries {
			if entry.Name() != component {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf(
					"source path %s must be a regular directory, not a symlink",
					path.Join(
						parent,
						component,
					),
				)
			}
			found = true
			break
		}
		if !found {
			return &fs.PathError{
				Op:   "read source directory",
				Path: directory,
				Err:  fs.ErrNotExist,
			}
		}
		parent = path.Join(
			parent,
			component,
		)
	}
	return nil
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

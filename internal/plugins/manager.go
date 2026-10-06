package plugins

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
)

// Manager owns plugin discovery and destination files.
// It returns domain results without prompting or writing command output.
type Manager struct {
	sources      SourceClient
	destinations afero.Fs
}

// NewManager supplies acquisition and destination filesystem boundaries. A nil
// source client is sufficient for installed listing and removal.
func NewManager(sources SourceClient, destinations afero.Fs) *Manager {
	return &Manager{
		sources:      sources,
		destinations: destinations,
	}
}

type DiscoveryOptions struct{ FullDepth bool }

// InstalledPlugin is a plugin found in a destination directory.
type InstalledPlugin struct {
	Plugin    Plugin
	Directory string
}

// RemovedPlugin identifies a deleted package. Unreadable manifests fall back
// to the directory name and an empty description.
type RemovedPlugin struct {
	Name        string
	Description string
	Path        string
}

// Directory is an exact plugin root. Path selects one source-relative package
// directory and is exclusive with Plugins. Neither selects all discovered plugins.
type InstallRequest struct {
	Source    string
	Directory string
	Plugins   []string
	Path      *string
	Override  bool
	Discovery DiscoveryOptions
}

// RemoveRequest selects explicit installed plugin directory names.
type RemoveRequest struct {
	Directory string
	Plugins   []string
}

// ListSource returns source-relative Plugin.Path values using io/fs conventions.
func (m *Manager) ListSource(ctx context.Context, source string, opts DiscoveryOptions) (result []Plugin, err error) {
	checkout, err := m.acquire(
		ctx,
		source,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(
			err,
			releaseCheckout(checkout),
		)
		if err != nil {
			result = nil
		}
	}()
	return discover(
		ctx,
		checkout.FS,
		opts,
		nil,
	)
}

// Install copies whole packages without activating them. Override replaces
// complete existing packages. Returned plugins identify committed installations,
// even if cleanup returns an error.
func (m *Manager) Install(ctx context.Context, request InstallRequest) (result []InstalledPlugin, err error) {
	if err := m.destination(
		ctx,
		request.Directory,
	); err != nil {
		return nil, err
	}
	if request.Path != nil {
		if len(request.Plugins) > 0 {
			return nil, errors.New("--path and --plugin are mutually exclusive")
		}
		if !fs.ValidPath(*request.Path) || strings.ContainsAny(
			*request.Path,
			"\\\x00",
		) {
			return nil, fmt.Errorf(
				"invalid source directory %q; --path must be a source-relative plugin directory (use . for the source root)",
				*request.Path,
			)
		}
	}
	names, err := selectedNames(request.Plugins)
	if err != nil {
		return nil, err
	}
	checkout, err := m.acquire(
		ctx,
		request.Source,
	)
	if err != nil {
		return nil, err
	}
	released := false
	defer func() {
		if !released {
			err = errors.Join(
				err,
				releaseCheckout(checkout),
			)
			if err != nil {
				result = nil
			}
		}
	}()
	var available []Plugin
	if request.Path != nil {
		if err := ensureSourceDirectory(
			checkout.FS,
			*request.Path,
		); err != nil {
			return nil, err
		}
		plugin, err := readPlugin(
			checkout.FS,
			*request.Path,
		)
		if err != nil {
			return nil, err
		}
		available = []Plugin{
			plugin,
		}
		names = []string{
			plugin.Name,
		}
	} else {
		available, err = discover(
			ctx,
			checkout.FS,
			request.Discovery,
			names,
		)
		if err != nil {
			return nil, err
		}
	}
	candidates := map[string][]Plugin{}
	for _, plugin := range available {
		candidates[plugin.Name] = append(
			candidates[plugin.Name],
			plugin,
		)
	}
	if len(names) == 0 {
		for name := range candidates {
			names = append(
				names,
				name,
			)
		}
	}
	sort.Strings(names)
	var changes []bundleChange
	for _, name := range names {
		options := candidates[name]
		if len(options) == 0 {
			return nil, fmt.Errorf(
				"plugin %q not found: %w",
				name,
				fs.ErrNotExist,
			)
		}
		if len(options) > 1 {
			var directories []string
			for _, plugin := range options {
				directories = append(
					directories,
					plugin.Path,
				)
			}
			return nil, fmt.Errorf(
				"ambiguous plugin name %q at source directories %q; use --path to select one",
				name,
				directories,
			)
		}
		filename := filepath.Join(
			request.Directory,
			name,
		)
		if info, statErr := m.pathInfo(filename); statErr == nil {
			if !request.Override {
				return nil, fmt.Errorf(
					"destination %s already exists; use --override to replace it: %w",
					filename,
					fs.ErrExist,
				)
			}
			if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
				return nil, fmt.Errorf(
					"destination %s is not a regular plugin directory",
					filename,
				)
			}
			definition, err := m.pathInfo(filepath.Join(
				filename,
				"plugin.json",
			))
			if err != nil {
				return nil, err
			}
			if !definition.Mode().IsRegular() {
				return nil, fmt.Errorf(
					"destination %s must contain a regular plugin.json",
					filename,
				)
			}
		} else if !errors.Is(
			statErr,
			fs.ErrNotExist,
		) {
			return nil, statErr
		}
		plugin := options[0]
		payload, err := readPayload(
			ctx,
			checkout.FS,
			plugin.Path,
		)
		if err != nil {
			return nil, err
		}
		action := installBundle
		if request.Override {
			action = replaceBundle
		}
		changes = append(
			changes,
			bundleChange{
				name:    name,
				payload: payload,
				action:  action,
			},
		)
		result = append(
			result,
			InstalledPlugin{
				Plugin: relocatePlugin(
					plugin,
					filename,
				),
				Directory: filename,
			},
		)
	}
	released = true
	if err := releaseCheckout(checkout); err != nil {
		return nil, fmt.Errorf(
			"release source: %w",
			err,
		)
	}
	if committed, err := m.applyChanges(
		ctx,
		request.Directory,
		changes,
	); err != nil {
		if !committed {
			result = nil
		}
		return result, fmt.Errorf(
			"install plugins: %w",
			err,
		)
	}
	return result, nil
}

// ListInstalled inspects one exact root, including manually copied packages.
// Plugin-root symlinks are rejected, matching Load. Listing never activates code.
func (m *Manager) ListInstalled(ctx context.Context, directory string) ([]InstalledPlugin, error) {
	if err := m.destination(
		ctx,
		directory,
	); err != nil {
		return nil, err
	}
	entries, err := afero.ReadDir(
		m.destinations,
		directory,
	)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	filesystem := m.destinationFilesystem(directory)
	var result []InstalledPlugin
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf(
				"plugin root %q must not be a symbolic link",
				entry.Name(),
			)
		}
		if !entry.IsDir() || strings.HasPrefix(
			entry.Name(),
			".cpe-stage-",
		) {
			continue
		}
		if _, err := m.pathInfo(filepath.Join(
			directory,
			entry.Name(),
			"plugin.json",
		)); errors.Is(
			err,
			fs.ErrNotExist,
		) {
			continue
		} else if err != nil {
			return nil, err
		}
		plugin, err := readPlugin(
			filesystem,
			entry.Name(),
		)
		if err != nil {
			return nil, err
		}
		filename := filepath.Join(
			directory,
			entry.Name(),
		)
		result = append(
			result,
			InstalledPlugin{
				Plugin: relocatePlugin(
					plugin,
					filename,
				),
				Directory: filename,
			},
		)
	}
	return result, nil
}

// Remove deletes named plugin directories. Reading the manifest for result
// details is best-effort and never determines whether a package can be removed.
// Returned plugins identify committed removals, even if cleanup returns an error.
func (m *Manager) Remove(ctx context.Context, request RemoveRequest) ([]RemovedPlugin, error) {
	plan, err := m.prepareRemoval(
		ctx,
		request,
	)
	if err != nil {
		return nil, err
	}
	filesystem := m.destinationFilesystem(request.Directory)
	var result []RemovedPlugin
	for _, directory := range plan.paths {
		removed := RemovedPlugin{
			Name: filepath.Base(directory),
			Path: directory,
		}
		if plugin, err := readPlugin(
			filesystem,
			filepath.Base(directory),
		); err == nil {
			removed.Name = plugin.Name
			removed.Description = plugin.Description
		}
		result = append(
			result,
			removed,
		)
	}
	if committed, err := m.applyChanges(
		ctx,
		request.Directory,
		plan.changes,
	); err != nil {
		if committed {
			return result, fmt.Errorf(
				"remove plugins: %w",
				err,
			)
		}
		return nil, fmt.Errorf(
			"remove plugins: %w",
			err,
		)
	}
	return result, nil
}

func relocatePlugin(plugin Plugin, directory string) Plugin {
	prefix := plugin.Path + "/"
	if plugin.Path == "." {
		prefix = ""
	}
	for i := range plugin.Skills {
		plugin.Skills[i].Path = filepath.Join(
			directory,
			filepath.FromSlash(strings.TrimPrefix(
				plugin.Skills[i].Path,
				prefix,
			)),
		)
	}
	plugin.Path = directory
	return plugin
}

func (m *Manager) pathInfo(filename string) (fs.FileInfo, error) {
	if filesystem, ok := m.destinations.(afero.Lstater); ok {
		info, _, err := filesystem.LstatIfPossible(filename)
		return info, err
	}
	return m.destinations.Stat(filename)
}

func (m *Manager) destinationFilesystem(directory string) fs.FS {
	return destinationFS{
		FS: afero.NewIOFS(afero.NewBasePathFs(
			m.destinations,
			directory,
		)),
		manager:   m,
		directory: directory,
	}
}

type destinationFS struct {
	fs.FS
	manager   *Manager
	directory string
}

func (d destinationFS) ReadLink(name string) (string, error) {
	if reader, ok := d.manager.destinations.(afero.LinkReader); ok {
		return reader.ReadlinkIfPossible(filepath.Join(
			d.directory,
			filepath.FromSlash(name),
		))
	}
	return "", fs.ErrPermission
}

func (d destinationFS) Lstat(name string) (fs.FileInfo, error) {
	return d.manager.pathInfo(filepath.Join(
		d.directory,
		filepath.FromSlash(name),
	))
}

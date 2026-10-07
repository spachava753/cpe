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

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/source"
)

// Manager discovers and manages native Agent Plugin packages in source and
// destination filesystems. Construct a Manager with NewManager.
type Manager struct {
	sources      source.Client
	destinations afero.Fs
}

// NewManager uses sources to fetch trees and destinations to manage installed files.
// sources may be nil for installed listing and removal. Installed operations require
// a non-nil destinations filesystem, such as afero.NewOsFs().
func NewManager(sources source.Client, destinations afero.Fs) *Manager {
	return &Manager{
		sources:      sources,
		destinations: destinations,
	}
}

// DiscoveryOptions controls recursive source search, which skips .git and node_modules.
type DiscoveryOptions struct {
	// FullDepth removes the default search limit of five directory components.
	FullDepth bool
}

// InstalledPlugin pairs a plugin with its installed package directory.
type InstalledPlugin struct {
	// Plugin.Path equals Directory; skill paths name installed SKILL.md files.
	Plugin Plugin
	// Directory is the package's location beneath the supplied destination root.
	Directory string
}

// RemovedPlugin identifies a deleted package. Unreadable manifests yield the
// directory name and an empty description.
type RemovedPlugin struct {
	Name        string
	Description string
	// Path names the former package directory in native filesystem syntax.
	Path string
}

// InstallRequest selects packages and an exact root for Manager.Install.
// Empty Plugins and nil Path select all discovered native packages.
type InstallRequest struct {
	// Source is a locator accepted by the configured source.Client.
	Source string
	// Directory receives packages at Directory/<declared plugin name>.
	Directory string
	// Plugins selects declared manifest names. Use Path to resolve ambiguity.
	Plugins []string
	// Path selects a source-relative package directory; "." selects the root.
	// It uses io/fs slash paths and is mutually exclusive with Plugins.
	Path *string
	// Override replaces existing packages, including obsolete files.
	Override bool
	// Discovery controls search when Path is nil.
	Discovery DiscoveryOptions
}

// RemoveRequest selects installed package directories for Manager.Remove.
type RemoveRequest struct {
	// Directory is the exact installation root in native filesystem syntax.
	Directory string
	// Plugins contains child directory names. At least one name is required.
	Plugins []string
}

// ListSource fetches source and discovers native packages according to opts.
// Package and skill paths are source-relative slash paths; "." names a root package.
// The checkout is closed before returning. Fatal errors return nil plugins;
// component problems are recorded in Plugin.Diagnostics.
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
			checkout.Close(),
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

// Install copies selected packages to request.Directory/<declared plugin name>.
// Whole packages are preserved except Git metadata; contained relative links are
// materialized as independent files. Existing destinations require a regular plugin.json.
//
// Publication failures attempt rollback. Non-nil results with an error identify
// committed installations whose cleanup failed; rollback failures retain recovery
// files described in the error. Component problems remain in Plugin.Diagnostics.
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
				checkout.Close(),
			)
			if err != nil {
				result = nil
			}
		}
	}()
	var available []Plugin
	if request.Path != nil {
		if err := bundlefs.CheckSourceDirectory(
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
	var changes []bundlefs.Change
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
		if info, statErr := bundlefs.Lstat(
			m.destinations,
			filename,
		); statErr == nil {
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
			if err := m.checkDefinition(filename); err != nil {
				return nil, err
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
		action := bundlefs.Install
		if request.Override {
			action = bundlefs.Replace
		}
		changes = append(
			changes,
			bundlefs.Change{
				Name:    name,
				Payload: payload,
				Action:  action,
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
	if err := checkout.Close(); err != nil {
		return nil, fmt.Errorf(
			"release source: %w",
			err,
		)
	}
	if outcome, err := bundlefs.Apply(
		ctx,
		m.destinations,
		request.Directory,
		changes,
		m.checkDefinition,
	); err != nil {
		if outcome != bundlefs.Committed {
			result = nil
		}
		return result, fmt.Errorf(
			"install plugins: %w",
			err,
		)
	}
	return result, nil
}

// ListInstalled reads packages immediately beneath directory, including manually
// copied packages. It skips directories without plugin.json and recovery stages;
// symlink entries are rejected. A missing root returns no results. Invalid manifests
// return nil results and an error; component problems become Plugin.Diagnostics.
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
		if _, err := bundlefs.Lstat(
			m.destinations,
			filepath.Join(
				directory,
				entry.Name(),
				"plugin.json",
			),
		); errors.Is(
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

// Remove deletes selected package directories containing regular plugin.json files.
// Directory symlinks are rejected; result metadata is read best-effort.
// Non-nil results with an error identify committed removals whose cleanup failed.
// Failed rollback retains recovery files described in the error.
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
	if outcome, err := bundlefs.Apply(
		ctx,
		m.destinations,
		request.Directory,
		plan.changes,
		m.checkDefinition,
	); err != nil {
		if outcome == bundlefs.Committed {
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
	return bundlefs.Lstat(
		d.manager.destinations,
		filepath.Join(
			d.directory,
			filepath.FromSlash(name),
		),
	)
}

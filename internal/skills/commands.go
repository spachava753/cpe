package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

func resolveDirectories(scope ScopeOptions, requireScope bool) ([]string, error) {
	if scope.Project && scope.Global {
		return nil, errors.New("conflicting scope selections")
	}
	if requireScope && !scope.Project && !scope.Global {
		return nil, errors.New("scope is required for install")
	}
	if scope.Directory != nil {
		if *scope.Directory == "" {
			return nil, errors.New("directory must not be empty")
		}
		if scope.Global {
			return nil, errors.New("custom directory cannot be combined with global scope")
		}
		directory := *scope.Directory
		if !filepath.IsAbs(directory) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			directory = cwd + string(filepath.Separator) + directory
		}
		return []string{directory}, nil
	}
	var directories []string
	if scope.Project || !scope.Global {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		directories = append(directories, ProjectPath(cwd))
	}
	if scope.Global || !scope.Project {
		directory, err := GlobalPath()
		if err != nil {
			return nil, err
		}
		if len(directories) == 0 || directory != directories[0] {
			directories = append(directories, directory)
		}
	}
	return directories, nil
}

func terminalText(value string) string {
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}

type commandError struct {
	action string
	cause  error
}

func (err *commandError) Error() string {
	return err.action + " skills: " + terminalText(err.cause.Error())
}
func (err *commandError) Unwrap() error { return err.cause }

func outputStream(streams IO) io.Writer {
	if streams.Out != nil {
		return streams.Out
	}
	return os.Stdout
}

func writeInstalled(streams IO, asJSON bool, result []InstalledSkill) error {
	output := outputStream(streams)
	if asJSON {
		if result == nil {
			result = []InstalledSkill{}
		}
		return json.NewEncoder(output).Encode(result)
	}
	for _, entry := range result {
		if _, err := fmt.Fprintf(output, "%s\t%s\t%s\n", terminalText(entry.Skill.Name), terminalText(entry.Skill.Description), terminalText(entry.Directory)); err != nil {
			return err
		}
	}
	return nil
}

func installCommand(ctx context.Context, manager *Manager, opts InstallOptions, streams IO) error {
	directories, err := resolveDirectories(opts.Scope, true)
	if err != nil {
		return err
	}
	result, err := manager.Install(ctx, InstallRequest{Source: opts.Source, Directory: directories[0], Skills: opts.Skills, Discovery: DiscoveryOptions{FullDepth: opts.FullDepth}})
	if err != nil {
		if _, committed := errors.AsType[*CommittedCleanupError](err); committed {
			return errors.Join(err, writeInstalled(streams, opts.JSON, result))
		}
		return err
	}
	return writeInstalled(streams, opts.JSON, result)
}

func listCommand(ctx context.Context, manager *Manager, opts ListOptions, streams IO) error {
	if opts.Source != nil {
		result, err := manager.ListSource(ctx, *opts.Source, DiscoveryOptions{})
		if err != nil {
			return err
		}
		output := outputStream(streams)
		if opts.JSON {
			if result == nil {
				result = []Skill{}
			}
			return json.NewEncoder(output).Encode(result)
		}
		for _, skill := range result {
			if _, err := fmt.Fprintf(output, "%s\t%s\t%s\n", terminalText(skill.Name), terminalText(skill.Description), terminalText(skill.Path)); err != nil {
				return err
			}
		}
		return nil
	}
	directories, err := resolveDirectories(opts.Scope, false)
	if err != nil {
		return err
	}
	var result []InstalledSkill
	for _, directory := range directories {
		installed, err := manager.ListInstalled(ctx, directory)
		if err != nil {
			return err
		}
		result = append(result, installed...)
	}
	return writeInstalled(streams, opts.JSON, result)
}

func removeCommand(ctx context.Context, manager *Manager, opts RemoveOptions, streams IO) error {
	directories, err := resolveDirectories(opts.Scope, false)
	if err != nil {
		return err
	}
	names, err := selectedNames(opts.Skills)
	if err != nil {
		return err
	}
	if opts.All && len(names) > 0 {
		return errors.New("all cannot be combined with selected skill names")
	}
	if !opts.All && len(names) == 0 {
		return errors.New("select at least one skill to remove")
	}
	selected := make(map[string][]string)
	if opts.All {
		for _, directory := range directories {
			installed, err := manager.ListInstalled(ctx, directory)
			if err != nil {
				return err
			}
			for _, entry := range installed {
				selected[directory] = append(selected[directory], entry.Skill.Name)
			}
		}
	} else {
		for _, name := range names {
			var matches []string
			for _, directory := range directories {
				if err := manager.destination(ctx, directory); err != nil {
					return err
				}
				info, err := manager.pathInfo(filepath.Join(directory, name))
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				if err != nil {
					return err
				}
				if !info.IsDir() {
					return fmt.Errorf("%s is not a skill directory", filepath.Join(directory, name))
				}
				matches = append(matches, directory)
			}
			if len(matches) == 0 {
				return fmt.Errorf("skill %q not found: %w", name, fs.ErrNotExist)
			}
			if len(matches) > 1 {
				return fmt.Errorf("skill %q exists in both directories; --scope is required", name)
			}
			selected[matches[0]] = append(selected[matches[0]], name)
		}
	}
	// Check every selection's eligibility before removing the first scope.
	for _, directory := range directories {
		if len(selected[directory]) == 0 {
			continue
		}
		if _, err := manager.prepareRemoval(ctx, RemoveRequest{Directory: directory, Skills: selected[directory]}); err != nil {
			return err
		}
	}
	var result []string
	for _, directory := range directories {
		if len(selected[directory]) == 0 {
			continue
		}
		removed, err := manager.Remove(ctx, RemoveRequest{Directory: directory, Skills: selected[directory]})
		result = append(result, removed...)
		if err != nil {
			if len(result) > 0 {
				return errors.Join(fmt.Errorf("some skills changes committed: %w", err), writeRemoved(streams, opts.JSON, result))
			}
			return err
		}
	}
	return writeRemoved(streams, opts.JSON, result)
}

func writeRemoved(streams IO, asJSON bool, result []string) error {
	output := outputStream(streams)
	if asJSON {
		if result == nil {
			result = []string{}
		}
		return json.NewEncoder(output).Encode(result)
	}
	for _, filename := range result {
		if _, err := fmt.Fprintln(output, terminalText(filename)); err != nil {
			return err
		}
	}
	return nil
}

func updateCommand(ctx context.Context, manager *Manager, opts UpdateOptions, streams IO) error {
	directories, err := resolveDirectories(opts.Scope, false)
	if err != nil {
		return err
	}
	names, err := selectedNames(opts.Skills)
	if err != nil {
		return err
	}
	selected := make(map[string][]string)
	found := make(map[string]bool)
	for _, directory := range directories {
		if err := manager.destination(ctx, directory); err != nil {
			return err
		}
		records, err := manager.records(directory)
		if err != nil {
			return err
		}
		for _, name := range names {
			if _, ok := records.Skills[name]; ok {
				selected[directory] = append(selected[directory], name)
				found[name] = true
			}
		}
	}
	for _, name := range names {
		if !found[name] {
			return fmt.Errorf("skill %q is not managed", name)
		}
	}
	var result []InstalledSkill
	for _, directory := range directories {
		if len(names) > 0 && len(selected[directory]) == 0 {
			continue
		}
		updated, err := manager.Update(ctx, UpdateRequest{Directory: directory, Skills: selected[directory]})
		result = append(result, updated...)
		if err != nil {
			if len(result) > 0 {
				return errors.Join(fmt.Errorf("some skills changes committed: %w", err), writeInstalled(streams, opts.JSON, result))
			}
			return err
		}
	}
	return writeInstalled(streams, opts.JSON, result)
}

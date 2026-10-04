package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		return []string{
			directory,
		}, nil
	}
	var directories []string
	if scope.Project || !scope.Global {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		directories = append(
			directories,
			ProjectPath(cwd),
		)
	}
	if scope.Global || !scope.Project {
		directory, err := GlobalPath()
		if err != nil {
			return nil, err
		}
		if len(directories) == 0 || directory != directories[0] {
			directories = append(
				directories,
				directory,
			)
		}
	}
	return directories, nil
}

func terminalText(value string) string {
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}

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
		if _, err := fmt.Fprintf(
			output,
			"%s\t%s\t%s\n",
			terminalText(entry.Skill.Name),
			terminalText(entry.Skill.Description),
			terminalText(entry.Directory),
		); err != nil {
			return err
		}
	}
	return nil
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
		if _, err := fmt.Fprintln(
			output,
			terminalText(filename),
		); err != nil {
			return err
		}
	}
	return nil
}

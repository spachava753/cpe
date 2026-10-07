package commandio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ScopeOptions selects project, global, or custom installation roots.
// Project and Global are mutually exclusive.
type ScopeOptions struct {
	// Directory selects one exact root when non-nil and requires Global to be false.
	Directory *string
	// Project selects the current working directory's installation root.
	Project bool
	// Global selects the user's home directory's installation root.
	Global bool
}

// Directories resolves roots beneath .agents/defaultDirectory in the working or
// home directory. With neither scope selected, it returns both roots, project first.
// requireScope requires an explicit Project or Global choice, even with Directory.
//
// Directory selects only that exact root. Relative custom paths use the working
// directory; path components such as link/.. are preserved for later safety checks.
// Invalid scope combinations or path-resolution failures return nil paths and an error.
func (scope ScopeOptions) Directories(defaultDirectory string, requireScope bool) ([]string, error) {
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
		if filepath.VolumeName(directory) != "" && !filepath.IsAbs(directory) {
			return nil, errors.New("directory must not be drive-relative")
		}
		if !filepath.IsAbs(directory) && !strings.HasPrefix(
			filepath.FromSlash(directory),
			string(filepath.Separator),
		) {
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
			ProjectPath(
				cwd,
				defaultDirectory,
			),
		)
	}
	if scope.Global || !scope.Project {
		directory, err := GlobalPath(defaultDirectory)
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

// ProjectPath returns filepath.Join(cwd, ".agents", directory).
func ProjectPath(cwd string, directory string) string {
	return filepath.Join(
		cwd,
		".agents",
		directory,
	)
}

// GlobalPath returns <home>/.agents/<directory>, or an error if the user's home
// directory cannot be determined.
func GlobalPath(directory string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return ProjectPath(
		home,
		directory,
	), nil
}

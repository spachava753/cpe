package skills

import (
	"os"
	"path/filepath"

	"github.com/spachava753/cpe/internal/commandio"
)

type skillScope struct {
	directory string
	lock      LockFile
}

func resolveScopes(options commandio.ScopeOptions, requireScope bool) ([]skillScope, error) {
	if options.Directory == nil && !options.Project && !options.Global && !requireScope {
		project, err := resolveScopes(
			commandio.ScopeOptions{
				Project: true,
			},
			false,
		)
		if err != nil {
			return nil, err
		}
		global, err := resolveScopes(
			commandio.ScopeOptions{
				Global: true,
			},
			false,
		)
		if err != nil {
			return nil, err
		}
		return append(
			project,
			global...,
		), nil
	}
	directories, err := options.Directories(
		"skills",
		requireScope,
	)
	if err != nil {
		return nil, err
	}
	var result []skillScope
	for _, directory := range directories {
		file := LockFile{
			Format: ProjectLock,
		}
		switch {
		case options.Directory != nil:
			file.Path = directory + string(filepath.Separator) + "skills-lock.json"
		case options.Global:
			file.Format = GlobalLock
			if state := os.Getenv("XDG_STATE_HOME"); state != "" {
				file.Path = state + string(filepath.Separator) + filepath.Join(
					"skills",
					".skill-lock.json",
				)
			} else {
				file.Path = filepath.Join(
					filepath.Dir(directory),
					".skill-lock.json",
				)
			}
		default:
			file.Path = filepath.Join(
				filepath.Dir(filepath.Dir(directory)),
				"skills-lock.json",
			)
		}
		result = append(
			result,
			skillScope{
				directory: directory,
				lock:      file,
			},
		)
	}
	return result, nil
}

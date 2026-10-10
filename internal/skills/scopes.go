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
	directories, err := options.Directories(
		"skills",
		requireScope,
	)
	if err != nil {
		return nil, err
	}
	var result []skillScope
	for index, directory := range directories {
		file := LockFile{
			Format: ProjectLock,
		}
		switch {
		case options.Directory != nil:
			file.Path = filepath.Join(
				directory,
				"skills-lock.json",
			)
		case options.Global || index > 0:
			file.Format = GlobalLock
			if state := os.Getenv("XDG_STATE_HOME"); state != "" {
				file.Path = filepath.Join(
					state,
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

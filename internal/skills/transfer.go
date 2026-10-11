package skills

import (
	"context"
	"fmt"
	"io/fs"
	"path"

	"github.com/spachava753/cpe/internal/bundlefs"
)

type payloadPolicy uint8

const (
	sourcePayload payloadPolicy = iota + 1
	installedPayload
)

// Source bundles omit dependency directories. Installed fingerprints include all
// files, so locally added files in otherwise excluded directories remain protected.
func readPayload(ctx context.Context, filesystem fs.FS, directory string) ([]bundlefs.Entry, error) {
	return collectPayload(
		ctx,
		filesystem,
		directory,
		sourcePayload,
	)
}

func collectPayload(ctx context.Context, filesystem fs.FS, directory string, policy payloadPolicy) ([]bundlefs.Entry, error) {
	if err := bundlefs.CheckSourceDirectory(
		filesystem,
		directory,
	); err != nil {
		return nil, err
	}
	var result []bundlefs.Entry
	err := fs.WalkDir(
		filesystem,
		directory,
		func(filename string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			if policy == sourcePayload && filename != directory && entry.IsDir() && excludedDirectory(entry.Name()) {
				return fs.SkipDir
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				return fmt.Errorf(
					"payload symlink %s is not supported",
					filename,
				)
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf(
					"payload %s is not a regular file",
					filename,
				)
			}
			name := filename
			if directory != "." {
				if filename == directory {
					name = "."
				} else {
					name = filename[len(directory)+1:]
				}
			}
			if !fs.ValidPath(name) {
				return fmt.Errorf(
					"invalid payload path %q",
					name,
				)
			}
			item := bundlefs.Entry{
				Path: name,
				Mode: info.Mode(),
			}
			if !item.Mode.IsDir() {
				item.Data, err = fs.ReadFile(
					filesystem,
					filename,
				)
				if err != nil {
					return fmt.Errorf(
						"read payload %s: %w",
						filename,
						err,
					)
				}
			}
			result = append(
				result,
				item,
			)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func sourceDirectory(skill Skill) string { return path.Dir(skill.Path) }

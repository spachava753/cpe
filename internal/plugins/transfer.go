package plugins

import (
	"context"
	"fmt"
	"io/fs"
	"path"

	"github.com/spachava753/cpe/internal/bundlefs"
)

// Materialize contained relative links through the same package boundary used
// by Load. Installed packages remain independent of their temporary checkout.
// Bundled dependencies are preserved, unlike the skill collection policy.
func readPayload(ctx context.Context, filesystem fs.FS, directory string) ([]bundlefs.Entry, error) {
	if err := bundlefs.CheckSourceDirectory(
		filesystem,
		directory,
	); err != nil {
		return nil, err
	}
	pkg := packageFS{
		root: sourceFS{
			FS: filesystem,
		},
		directory: directory,
	}
	var result []bundlefs.Entry
	ancestors := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !fs.ValidPath(name) {
			return fmt.Errorf(
				"invalid payload path %q",
				name,
			)
		}
		resolved, err := pkg.resolve(name)
		if err != nil {
			return err
		}
		info, err := pkg.Stat(name)
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf(
				"payload %s is not a regular file",
				name,
			)
		}
		item := bundlefs.Entry{
			Path: name,
			Mode: info.Mode(),
		}
		if !info.IsDir() {
			item.Data, err = fs.ReadFile(
				pkg,
				name,
			)
			if err != nil {
				return err
			}
		}
		result = append(
			result,
			item,
		)
		if !info.IsDir() {
			return nil
		}
		if ancestors[resolved] {
			return fmt.Errorf(
				"payload directory link cycle at %s",
				name,
			)
		}
		ancestors[resolved] = true
		defer delete(
			ancestors,
			resolved,
		)
		entries, err := pkg.ReadDir(name)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Name() == ".git" {
				continue
			}
			if err := visit(path.Join(
				name,
				entry.Name(),
			)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit("."); err != nil {
		return nil, err
	}
	return result, nil
}

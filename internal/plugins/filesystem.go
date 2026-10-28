package plugins

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// packageFS keeps package reads inside one plugin. Relative symlinks are
// resolved component by component; absolute links and links whose containment
// cannot be established through ReadLinkFS are rejected. No host paths are
// inferred from the opaque fs.FS. Launch-time access needs the same checks.
type packageFS struct {
	root      fs.ReadDirFS
	directory string
}

func (p packageFS) resolve(name string) (string, error) {
	fail := func(err error) (string, error) {
		return "", &fs.PathError{
			Op: "resolve",
			Path: path.Join(
				p.directory,
				name,
			),
			Err: err,
		}
	}
	if strings.HasPrefix(
		name,
		"/",
	) || strings.ContainsAny(
		name,
		"\\\x00",
	) {
		return fail(fs.ErrPermission)
	}
	pending := strings.Split(
		name,
		"/",
	)
	var resolved []string
	links := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(resolved) == 0 {
				return fail(fs.ErrPermission)
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		parent := path.Join(
			p.directory,
			strings.Join(
				resolved,
				"/",
			),
		)
		entries, err := p.root.ReadDir(parent)
		if err != nil {
			return fail(err)
		}
		var entry fs.DirEntry
		for _, candidate := range entries {
			if candidate.Name() == part {
				entry = candidate
				break
			}
		}
		if entry == nil {
			return fail(fs.ErrNotExist)
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			links++
			if links > 40 {
				return fail(fmt.Errorf("too many symbolic links"))
			}
			reader, ok := p.root.(fs.ReadLinkFS)
			if !ok {
				return fail(fs.ErrPermission)
			}
			target, err := reader.ReadLink(path.Join(
				parent,
				part,
			))
			if err != nil {
				return fail(err)
			}
			if strings.HasPrefix(
				target,
				"/",
			) || strings.ContainsAny(
				target,
				"\\\x00",
			) {
				return fail(fs.ErrPermission)
			}
			// Do not clean away '..' before resolving preceding symlinks.
			pending = append(
				strings.Split(
					target,
					"/",
				),
				pending...,
			)
			continue
		}
		if len(pending) > 0 && !entry.IsDir() {
			return fail(fmt.Errorf(
				"%q is not a directory",
				part,
			))
		}
		resolved = append(
			resolved,
			part,
		)
	}
	return path.Join(
		p.directory,
		strings.Join(
			resolved,
			"/",
		),
	), nil
}

func (p packageFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fs.ErrInvalid,
		}
	}
	resolved, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	return p.root.Open(resolved)
}

func (p packageFS) Stat(name string) (fs.FileInfo, error) {
	resolved, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	return fs.Stat(
		p.root,
		resolved,
	)
}

func (p packageFS) ReadDir(name string) ([]fs.DirEntry, error) {
	resolved, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	return p.root.ReadDir(resolved)
}

// skills.Load is deliberately fail-fast. Give it exactly one discovered skill
// so the plugin layer can isolate a bad skill without duplicating its parser.
type singleSkillFS struct {
	entry fs.DirEntry
	pkg   packageFS
}

func (s singleSkillFS) Open(name string) (fs.File, error) {
	return s.pkg.Open(path.Join(
		"skills",
		name,
	))
}

func (s singleSkillFS) Stat(name string) (fs.FileInfo, error) {
	return s.pkg.Stat(path.Join(
		"skills",
		name,
	))
}

func (s singleSkillFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "." {
		return []fs.DirEntry{
			s.entry,
		}, nil
	}
	return s.pkg.ReadDir(path.Join(
		"skills",
		name,
	))
}

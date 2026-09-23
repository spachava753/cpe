package repl

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/spf13/afero"
)

type fileInfo struct {
	Filename string
	Bytes    int64
	FileMode fs.FileMode
	Modified time.Time
}

func snapshot(info os.FileInfo) *fileInfo {
	if info == nil {
		return nil
	}
	return &fileInfo{info.Name(), info.Size(), info.Mode(), info.ModTime()}
}
func (i *fileInfo) Name() string       { return i.Filename }
func (i *fileInfo) Size() int64        { return i.Bytes }
func (i *fileInfo) Mode() fs.FileMode  { return i.FileMode }
func (i *fileInfo) ModTime() time.Time { return i.Modified }
func (i *fileInfo) IsDir() bool        { return i.FileMode.IsDir() }
func (i *fileInfo) Sys() any           { return nil }

type filesystem struct {
	host afero.Fs
	j    *journal
	next int
}

func (f *filesystem) Name() string                         { return "cpe-journal" }
func (f *filesystem) Open(name string) (afero.File, error) { return f.OpenFile(name, os.O_RDONLY, 0) }
func (f *filesystem) Create(name string) (afero.File, error) {
	return f.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
}
func (f *filesystem) OpenFile(name string, flags int, mode os.FileMode) (afero.File, error) {
	f.next++
	var host afero.File
	id, err := boundary(f.j, "fs.open", []any{name, flags, mode}, func() (int, error) {
		if f.host == nil {
			return 0, errors.New("filesystem capability is unavailable")
		}
		var err error
		host, err = f.host.OpenFile(name, flags, mode)
		return f.next, err
	})
	if err != nil {
		return nil, err
	}
	return &file{host: host, fs: f.host, j: f.j, id: id, name: name, flags: flags, mode: mode}, nil
}
func (f *filesystem) Stat(name string) (os.FileInfo, error) {
	v, err := boundary(f.j, "fs.stat", name, func() (*fileInfo, error) {
		if f.host == nil {
			return nil, errors.New("filesystem capability is unavailable")
		}
		i, err := f.host.Stat(name)
		return snapshot(i), err
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}
func (f *filesystem) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	type result struct {
		Info *fileInfo
		Used bool
	}
	v, err := boundary(f.j, "fs.lstat", name, func() (result, error) {
		l, ok := f.host.(afero.Lstater)
		if !ok {
			return result{}, errors.ErrUnsupported
		}
		i, used, err := l.LstatIfPossible(name)
		return result{snapshot(i), used}, err
	})
	if err != nil {
		return nil, v.Used, err
	}
	return v.Info, v.Used, nil
}
func (f *filesystem) SymlinkIfPossible(oldname, newname string) error {
	return effect(f.j, "fs.symlink", []any{oldname, newname}, func() error {
		h, ok := f.host.(afero.Linker)
		if !ok {
			return errors.ErrUnsupported
		}
		return h.SymlinkIfPossible(oldname, newname)
	})
}
func (f *filesystem) ReadlinkIfPossible(name string) (string, error) {
	return boundary(f.j, "fs.readlink", name, func() (string, error) {
		h, ok := f.host.(afero.LinkReader)
		if !ok {
			return "", errors.ErrUnsupported
		}
		return h.ReadlinkIfPossible(name)
	})
}
func (f *filesystem) Abs(name string) (string, error) {
	return boundary(f.j, "fs.abs", name, func() (string, error) {
		h, ok := f.host.(interface{ Abs(string) (string, error) })
		if !ok {
			return "", errors.ErrUnsupported
		}
		return h.Abs(name)
	})
}
func (f *filesystem) Realpath(name string) (string, error) {
	return boundary(f.j, "fs.realpath", name, func() (string, error) {
		h, ok := f.host.(interface{ Realpath(string) (string, error) })
		if !ok {
			return "", errors.ErrUnsupported
		}
		return h.Realpath(name)
	})
}
func (f *filesystem) SameFile(a, b string) (bool, error) {
	return boundary(f.j, "fs.samefile", []any{a, b}, func() (bool, error) {
		h, ok := f.host.(interface {
			SameFile(string, string) (bool, error)
		})
		if !ok {
			return false, errors.ErrUnsupported
		}
		return h.SameFile(a, b)
	})
}
func (f *filesystem) Link(a, b string) error {
	return effect(f.j, "fs.link", []any{a, b}, func() error {
		h, ok := f.host.(interface{ Link(string, string) error })
		if !ok {
			return errors.ErrUnsupported
		}
		return h.Link(a, b)
	})
}

type file struct {
	host             afero.File
	fs               afero.Fs
	j                *journal
	id               int
	name             string
	flags            int
	mode             os.FileMode
	offset           int64
	directoryEntries int
	closed           bool
}

func (f *file) Name() string { return f.name }
func (f *file) available() error {
	if f.closed {
		return fs.ErrClosed
	}
	if f.host != nil {
		return nil
	}
	if f.fs == nil {
		return errors.New("filesystem capability is unavailable")
	}
	// This is reached only inside a NEW journaled host call, never during replay.
	// Reattach by path and position without repeating creation or truncation.
	host, err := f.fs.OpenFile(f.name, f.flags&^(os.O_CREATE|os.O_EXCL|os.O_TRUNC), f.mode)
	if err != nil {
		return err
	}
	if f.offset != 0 {
		_, err = host.Seek(f.offset, io.SeekStart)
	}
	if err == nil && f.directoryEntries > 0 {
		_, err = host.Readdirnames(f.directoryEntries)
	}
	if err != nil {
		_ = host.Close()
		return err
	}
	f.host = host
	return nil
}
func (f *file) Close() error {
	// Session cleanup must not append calls outside a chunk or touch replay handles.
	if !f.j.active {
		if f.host != nil {
			return f.host.Close()
		}
		return nil
	}
	err := effect(f.j, "file.close", f.id, func() error {
		if f.host == nil {
			return nil
		}
		return f.host.Close()
	})
	if err == nil {
		f.closed = true
	}
	return err
}

type readResult struct {
	Data []byte
	N    int
}

func (f *file) Read(p []byte) (int, error) {
	r, err := boundary(f.j, "file.read", []any{f.id, len(p)}, func() (readResult, error) {
		if err := f.available(); err != nil {
			return readResult{}, err
		}
		n, err := f.host.Read(p)
		return readResult{p[:n], n}, err
	})
	copy(p, r.Data)
	f.offset += int64(r.N)
	return r.N, err
}
func (f *file) ReadAt(p []byte, off int64) (int, error) {
	r, err := boundary(f.j, "file.read_at", []any{f.id, len(p), off}, func() (readResult, error) {
		if err := f.available(); err != nil {
			return readResult{}, err
		}
		n, err := f.host.ReadAt(p, off)
		return readResult{p[:n], n}, err
	})
	copy(p, r.Data)
	return r.N, err
}
func (f *file) Stat() (os.FileInfo, error) {
	v, err := boundary(f.j, "file.stat", f.id, func() (*fileInfo, error) {
		if err := f.available(); err != nil {
			return nil, err
		}
		i, err := f.host.Stat()
		return snapshot(i), err
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}
func (f *file) Readdir(n int) ([]os.FileInfo, error) {
	v, err := boundary(f.j, "file.readdir", []any{f.id, n}, func() ([]*fileInfo, error) {
		if err := f.available(); err != nil {
			return nil, err
		}
		infos, err := f.host.Readdir(n)
		var out []*fileInfo
		for _, i := range infos {
			out = append(out, snapshot(i))
		}
		return out, err
	})
	f.directoryEntries += len(v)
	out := make([]os.FileInfo, len(v))
	for i, info := range v {
		out[i] = info
	}
	return out, err
}
func (f *filesystem) Mkdir(name string, mode os.FileMode) error {
	return effect(f.j, "fs.mkdir", []any{name, mode}, func() error {
		if f.host == nil {
			return errors.New("filesystem capability is unavailable")
		}
		return f.host.Mkdir(name, mode)
	})
}
func (f *filesystem) MkdirAll(name string, mode os.FileMode) error {
	return effect(f.j, "fs.mkdirall", []any{name, mode}, func() error {
		if f.host == nil {
			return errors.New("filesystem capability is unavailable")
		}
		return f.host.MkdirAll(name, mode)
	})
}
func (f *filesystem) Remove(name string) error {
	return effect(f.j, "fs.remove", []any{name}, func() error {
		if f.host == nil {
			return errors.New("filesystem capability is unavailable")
		}
		return f.host.Remove(name)
	})
}
func (f *filesystem) RemoveAll(name string) error {
	return effect(f.j, "fs.removeall", []any{name}, func() error {
		if f.host == nil {
			return errors.New("filesystem capability is unavailable")
		}
		return f.host.RemoveAll(name)
	})
}
func (f *filesystem) Rename(a, b string) error {
	return effect(f.j, "fs.rename", []any{a, b}, func() error {
		if f.host == nil {
			return errors.New("filesystem capability is unavailable")
		}
		return f.host.Rename(a, b)
	})
}
func (f *filesystem) Chmod(name string, mode os.FileMode) error {
	return effect(f.j, "fs.chmod", []any{name, mode}, func() error {
		if f.host == nil {
			return errors.New("filesystem capability is unavailable")
		}
		return f.host.Chmod(name, mode)
	})
}
func (f *filesystem) Chown(name string, uid, gid int) error {
	return effect(f.j, "fs.chown", []any{name, uid, gid}, func() error {
		if f.host == nil {
			return errors.New("filesystem capability is unavailable")
		}
		return f.host.Chown(name, uid, gid)
	})
}
func (f *filesystem) Chtimes(name string, a, b time.Time) error {
	return effect(f.j, "fs.chtimes", []any{name, a, b}, func() error {
		if f.host == nil {
			return errors.New("filesystem capability is unavailable")
		}
		return f.host.Chtimes(name, a, b)
	})
}

type writeResult struct {
	N      int
	Offset int64
}

func (f *file) Write(p []byte) (int, error) {
	result, err := boundary(f.j, "file.write", []any{f.id, p}, func() (writeResult, error) {
		if err := f.available(); err != nil {
			return writeResult{Offset: f.offset}, err
		}
		n, err := f.host.Write(p)
		offset, seekErr := f.host.Seek(0, io.SeekCurrent)
		if seekErr != nil {
			offset = f.offset + int64(n)
		}
		return writeResult{n, offset}, err
	})
	f.offset = result.Offset
	return result.N, err
}
func (f *file) WriteAt(p []byte, off int64) (int, error) {
	return boundary(f.j, "file.writeat", []any{f.id, p, off}, func() (int, error) {
		if err := f.available(); err != nil {
			return 0, err
		}
		return f.host.WriteAt(p, off)
	})
}
func (f *file) WriteString(s string) (int, error) { return f.Write([]byte(s)) }
func (f *file) Seek(off int64, whence int) (int64, error) {
	position, err := boundary(f.j, "file.seek", []any{f.id, off, whence}, func() (int64, error) {
		if err := f.available(); err != nil {
			return 0, err
		}
		return f.host.Seek(off, whence)
	})
	if err == nil {
		f.offset = position
		f.directoryEntries = 0
	}
	return position, err
}
func (f *file) Readdirnames(n int) ([]string, error) {
	names, err := boundary(f.j, "file.readdirnames", []any{f.id, n}, func() ([]string, error) {
		if err := f.available(); err != nil {
			return nil, err
		}
		return f.host.Readdirnames(n)
	})
	f.directoryEntries += len(names)
	return names, err
}
func (f *file) Sync() error {
	return effect(f.j, "file.sync", []any{f.id}, func() error {
		if err := f.available(); err != nil {
			return err
		}
		return f.host.Sync()
	})
}
func (f *file) Truncate(n int64) error {
	return effect(f.j, "file.truncate", []any{f.id, n}, func() error {
		if err := f.available(); err != nil {
			return err
		}
		return f.host.Truncate(n)
	})
}

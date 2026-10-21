package toolruntime

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spachava753/dyson"
	"github.com/spf13/afero"
)

func TestEdit(t *testing.T) {
	for _, test := range []struct {
		name       string
		before     string
		code       string
		after      string
		output     string
		failed     bool
		applied    int
		otherAfter string
	}{
		{
			name:    "relative filename and automatic diff",
			before:  "start\nold\nend\n",
			code:    "edit('file', 'old', 'new')",
			after:   "start\nnew\nend\n",
			applied: 1,
			output:  "--- /work/file\n+++ /work/file\n@@ -2,1 +2,1 @@\n-old\n+new\n",
		},
		{
			name:    "keyword arguments and absolute filename",
			before:  "old",
			code:    "edit(filename='/work/file', old='old', new='new')",
			after:   "new",
			applied: 1,
			output:  "-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file",
		},
		{
			name:    "multiline diff line counts",
			before:  "prefix\nabc\nend\n",
			code:    "edit('file', 'abc', 'a\\nb')",
			after:   "prefix\na\nb\nend\n",
			applied: 1,
			output:  "@@ -2,1 +2,2 @@\n-abc\n+a\n+b\n",
		},
		{
			name:    "partial line replacement without final newline",
			before:  "prefix\nabc",
			code:    "edit('file', 'b', '\\n')",
			after:   "prefix\na\nc",
			applied: 1,
			output:  "@@ -2,1 +2,2 @@\n-abc\n\\ No newline at end of file\n+a\n+c\n\\ No newline at end of file",
		},
		{
			name:    "delete trailing line",
			before:  "one\ntwo\n",
			code:    "edit('file', 'two\\n', '')",
			after:   "one\n",
			applied: 1,
			output:  "@@ -2,1 +1,0 @@\n-two\n",
		},
		{
			name:    "empty replacement deletes text",
			before:  "old\n",
			code:    "edit('file', 'old\\n', '')",
			after:   "",
			applied: 1,
			output:  "@@ -1,1 +0,0 @@\n-old\n",
		},
		{
			name:    "multiline replacement preserves CRLF and unicode",
			before:  "keep\r\nold\r\ntext\r\n尾",
			code:    "edit('file', 'old\\r\\ntext', '新')",
			after:   "keep\r\n新\r\n尾",
			applied: 1,
		},
		{
			name:   "no match leaves file unchanged",
			before: "original",
			code:   "edit('file', 'missing', 'new')",
			after:  "original",
			output: "old text was not found",
			failed: true,
		},
		{
			name:   "multiple matches leave file unchanged",
			before: "old and old",
			code:   "edit('file', 'old', 'new')",
			after:  "old and old",
			output: "old text is ambiguous",
			failed: true,
		},
		{
			name:   "overlapping matches are ambiguous",
			before: "aaa",
			code:   "edit('file', 'aa', 'new')",
			after:  "aaa",
			output: "old text is ambiguous",
			failed: true,
		},
		{
			name:   "empty old text is rejected",
			before: "original",
			code:   "edit('file', '', 'new')",
			after:  "original",
			output: "nonempty old text",
			failed: true,
		},
		{
			name:   "missing file is an error",
			before: "original",
			code:   "edit('missing', 'old', 'new')",
			after:  "original",
			output: "file does not exist",
			failed: true,
		},
		{
			name:   "directory is not replaced",
			before: "original",
			code:   "edit('/work', 'old', 'new')",
			after:  "original",
			output: "regular file",
			failed: true,
		},
		{
			name:   "no change is an error",
			before: "old",
			code:   "edit('file', 'old', 'old')",
			after:  "old",
			output: "replacement would not change the file",
			failed: true,
		},
		{
			name:   "arguments must be strings",
			before: "original",
			code:   "edit('file', 1, 'new')",
			after:  "original",
			output: "for parameter old",
			failed: true,
		},
		{
			name:    "later script failure does not undo the edit",
			before:  "old",
			code:    "edit('file', 'old', 'new')\nfail('later failure')",
			after:   "new",
			applied: 1,
			output:  "later failure",
			failed:  true,
		},
		{
			name:    "same-file edits see preceding changes",
			before:  "old",
			code:    "edit('file', 'old', 'middle')\nedit('file', 'middle', 'new')",
			after:   "new",
			output:  "+new",
			applied: 2,
		},
		{
			name:    "failed edit retains earlier result and stops later edits",
			before:  "old",
			code:    "edit('file', 'old', 'middle')\nedit('file', 'missing', 'new')\nedit('file', 'middle', 'never')",
			after:   "middle",
			output:  "old text was not found",
			failed:  true,
			applied: 1,
		},
		{
			name:   "no-op stops later edits",
			before: "old",
			code:   "edit('file', 'old', 'old')\nedit('file', 'old', 'never')",
			after:  "old",
			output: "replacement would not change the file",
			failed: true,
		},
		{
			name:       "sequential edits across files",
			before:     "old",
			code:       "edit('file', 'old', 'new')\nedit('other', 'other', 'updated')",
			after:      "new",
			otherAfter: "updated",
			output:     "Edited \"/work/other\": replaced one occurrence.",
			applied:    2,
		},
		{
			name:       "failure across files retains earlier edit and stops the script",
			before:     "old",
			code:       "edit('file', 'old', 'new')\nedit('other', 'missing', 'updated')\nedit('file', 'new', 'never')",
			after:      "new",
			otherAfter: "other",
			output:     "old text was not found",
			failed:     true,
			applied:    1,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				host := hostFixture(
					t,
					emptyMCP{},
					runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
						return dyson.CommandResult{}, nil
					}),
				)
				filesystem := host.stdlib.FS
				if err := filesystem.MkdirAll(
					"/work",
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := afero.WriteFile(
					filesystem,
					"/work/file",
					[]byte(test.before),
					0o751,
				); err != nil {
					t.Fatal(err)
				}
				if test.otherAfter != "" {
					if err := afero.WriteFile(
						filesystem,
						"/work/other",
						[]byte("other"),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				result, err := host.execute(
					t.Context(),
					toolRequest(
						"starlark",
						"code",
						test.code,
					),
				)
				if err != nil {
					t.Fatal(err)
				}
				var output strings.Builder
				applied := 0
				for _, block := range result.Blocks {
					text := block.Content.String()
					output.WriteString(text)
					if strings.HasPrefix(
						text,
						"Edited ",
					) {
						applied++
					}
				}
				if applied != test.applied {
					t.Fatalf(
						"reported %d applied edits, want %d: %s",
						applied,
						test.applied,
						output.String(),
					)
				}
				if result.ToolResultError != test.failed || !strings.Contains(
					output.String(),
					test.output,
				) {
					t.Fatalf(
						"failed=%v output=%q; want failed=%v containing %q",
						result.ToolResultError,
						output.String(),
						test.failed,
						test.output,
					)
				}
				after, err := afero.ReadFile(
					filesystem,
					"/work/file",
				)
				if err != nil || string(after) != test.after {
					t.Fatalf(
						"file after edit: %q, %v; want %q",
						after,
						err,
						test.after,
					)
				}
				info, err := filesystem.Stat("/work/file")
				if err != nil || info.Mode().Perm() != 0o751 {
					t.Fatalf(
						"file permissions changed: %v, %v",
						info,
						err,
					)
				}
				wantFiles := 1
				if test.otherAfter != "" {
					wantFiles++
					other, err := afero.ReadFile(
						filesystem,
						"/work/other",
					)
					if err != nil || string(other) != test.otherAfter {
						t.Fatalf(
							"second file after edit: %q, %v; want %q",
							other,
							err,
							test.otherAfter,
						)
					}
				}
				entries, err := afero.ReadDir(
					filesystem,
					"/work",
				)
				if err != nil || len(entries) != wantFiles {
					t.Fatalf(
						"edit left unexpected files: %v, %v",
						entries,
						err,
					)
				}
			},
		)
	}
}

func TestEditHostPaths(t *testing.T) {
	for _, test := range []struct {
		name     string
		filename string
	}{
		{
			name:     "parent traversal",
			filename: "../target",
		},
		{
			name:     "file symlink",
			filename: "link",
		},
		{
			name:     "symlinked parent and relative file symlink",
			filename: "directory/link",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				root := t.TempDir()
				cwd := filepath.Join(
					root,
					"work",
				)
				if err := os.Mkdir(
					cwd,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(
					root,
					"target",
				)
				if err := os.WriteFile(
					target,
					[]byte("old"),
					0o751,
				); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(
					cwd,
					"link",
				)
				if err := os.Symlink(
					"../target",
					link,
				); err != nil {
					t.Fatal(err)
				}
				directory := filepath.Join(
					cwd,
					"directory",
				)
				if err := os.Symlink(
					cwd,
					directory,
				); err != nil {
					t.Fatal(err)
				}
				host, err := Local(
					t.Context(),
					Config{
						Cwd: cwd,
						MCP: emptyMCP{},
						KV:  memoryKV{},
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := host.Close(context.Background()); err != nil {
						t.Error(err)
					}
				})
				result, err := host.(*toolHost).execute(
					t.Context(),
					toolRequest(
						"starlark",
						"code",
						"edit("+strconv.Quote(test.filename)+", 'old', 'new')",
					),
				)
				if err != nil || result.ToolResultError {
					t.Fatalf(
						"edit failed: %v, %v",
						result,
						err,
					)
				}
				after, err := os.ReadFile(target)
				if err != nil || string(after) != "new" {
					t.Fatalf(
						"target not edited: %q, %v",
						after,
						err,
					)
				}
				info, err := os.Lstat(link)
				if err != nil || info.Mode()&fs.ModeSymlink == 0 {
					t.Fatalf(
						"edit replaced the symbolic link: %v, %v",
						info,
						err,
					)
				}
				info, err = os.Stat(target)
				if err != nil || info.Mode().Perm() != 0o751 {
					t.Fatalf(
						"edit changed target permissions: %v, %v",
						info,
						err,
					)
				}
			},
		)
	}
}

type editFaultFS struct {
	*afero.MemMapFs
	fault  string
	cancel context.CancelFunc
}

func (f *editFaultFS) OpenFile(name string, flag int, mode fs.FileMode) (afero.File, error) {
	file, err := f.MemMapFs.OpenFile(
		name,
		flag,
		mode,
	)
	if err != nil || !strings.HasPrefix(
		filepath.Base(name),
		".cpe-edit-",
	) {
		return file, err
	}
	return &editFaultFile{
		File:       file,
		filesystem: f,
	}, nil
}

func (f *editFaultFS) Chmod(name string, mode fs.FileMode) error {
	if f.fault == "chmod" {
		return errors.New("chmod failed")
	}
	return f.MemMapFs.Chmod(
		name,
		mode,
	)
}

func (f *editFaultFS) Rename(from, to string) error {
	if f.fault == "rename" {
		return errors.New("rename failed")
	}
	return f.MemMapFs.Rename(
		from,
		to,
	)
}

type editFaultFile struct {
	afero.File
	filesystem *editFaultFS
}

func (f *editFaultFile) WriteString(text string) (int, error) {
	if f.filesystem.fault == "short write" {
		return 0, nil
	}
	if f.filesystem.fault == "write" {
		return 0, errors.New("write failed")
	}
	return f.File.WriteString(text)
}

func (f *editFaultFile) Sync() error {
	switch f.filesystem.fault {
	case "sync":
		return errors.New("sync failed")
	case "concurrent change":
		return afero.WriteFile(
			f.filesystem.MemMapFs,
			"/file",
			[]byte("another writer"),
			0o600,
		)
	case "cancellation":
		f.filesystem.cancel()
	}
	return f.File.Sync()
}

func (f *editFaultFile) Close() error {
	err := f.File.Close()
	if f.filesystem.fault == "close" {
		return errors.Join(
			err,
			errors.New("close failed"),
		)
	}
	return err
}

func TestEditAtomicFailures(t *testing.T) {
	for _, fault := range []string{
		"write",
		"short write",
		"sync",
		"close",
		"chmod",
		"rename",
		"concurrent change",
		"cancellation",
	} {
		t.Run(
			fault,
			func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				filesystem := &editFaultFS{
					MemMapFs: afero.NewMemMapFs().(*afero.MemMapFs),
					fault:    fault,
					cancel:   cancel,
				}
				if err := afero.WriteFile(
					filesystem.MemMapFs,
					"/file",
					[]byte("old"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				if _, err := editFile(
					ctx,
					filesystem,
					"/file",
					"old",
					"new",
				); err == nil {
					t.Fatal("edit succeeded despite failure")
				}
				want := "old"
				if fault == "concurrent change" {
					want = "another writer"
				}
				after, err := afero.ReadFile(
					filesystem,
					"/file",
				)
				if err != nil || string(after) != want {
					t.Fatalf(
						"failed edit clobbered file: %q, %v; want %q",
						after,
						err,
						want,
					)
				}
				entries, err := afero.ReadDir(
					filesystem,
					"/",
				)
				if err != nil || len(entries) != 1 {
					t.Fatalf(
						"failed edit leaked a temporary file: %v, %v",
						entries,
						err,
					)
				}
			},
		)
	}
}

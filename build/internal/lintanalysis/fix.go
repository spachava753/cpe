package lintanalysis

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"go/format"
	"os"
	"slices"

	"golang.org/x/tools/go/analysis/checker"
)

type textEdit struct {
	text  string
	start int
	end   int
}

type fileFix struct {
	path   string
	before []byte
	after  []byte
}

func applyFixes(ctx context.Context, graph *checker.Graph) (bool, error) {
	byFile := make(map[string][]textEdit)
	for _, act := range graph.Roots {
		for _, diag := range act.Diagnostics {
			if len(diag.SuggestedFixes) == 0 {
				continue
			}
			for _, edit := range diag.SuggestedFixes[0].TextEdits {
				file := act.Package.Fset.File(edit.Pos)
				if file == nil {
					return false, fmt.Errorf(
						"%s: suggested fix has no source file",
						act.Analyzer.Name,
					)
				}
				end := edit.End
				if !end.IsValid() {
					end = edit.Pos
				}
				if act.Package.Fset.File(end) != file || end < edit.Pos {
					return false, fmt.Errorf(
						"%s: invalid suggested fix in %s",
						act.Analyzer.Name,
						file.Name(),
					)
				}
				byFile[file.Name()] = append(
					byFile[file.Name()],
					textEdit{
						text:  string(edit.NewText),
						start: file.Offset(edit.Pos),
						end:   file.Offset(end),
					},
				)
			}
		}
	}
	// Validate and format every file before writing any. Test package variants may
	// suggest identical edits; deduplicate those, but never guess at conflicts.
	var fixes []fileFix
	for path, edits := range byFile {
		before, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		after, err := editedSource(
			before,
			edits,
		)
		if err != nil {
			return false, fmt.Errorf(
				"%s: %w",
				path,
				err,
			)
		}
		after, err = format.Source(after)
		if err != nil {
			return false, fmt.Errorf(
				"format suggested fixes for %s: %w",
				path,
				err,
			)
		}
		if !bytes.Equal(
			before,
			after,
		) {
			fixes = append(
				fixes,
				fileFix{
					path:   path,
					before: before,
					after:  after,
				},
			)
		}
	}
	slices.SortFunc(
		fixes,
		func(a, b fileFix) int {
			return cmp.Compare(
				a.path,
				b.path,
			)
		},
	)
	for _, fix := range fixes {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		current, err := os.ReadFile(fix.path)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(
			current,
			fix.before,
		) {
			return false, fmt.Errorf(
				"%s changed while preparing fixes",
				fix.path,
			)
		}
		if err := os.WriteFile(
			fix.path,
			fix.after,
			0o644,
		); err != nil {
			return false, err
		}
	}
	return len(fixes) != 0, nil
}

func editedSource(before []byte, edits []textEdit) ([]byte, error) {
	slices.SortFunc(
		edits,
		func(a, b textEdit) int {
			return cmp.Or(
				cmp.Compare(
					a.start,
					b.start,
				),
				cmp.Compare(
					a.end,
					b.end,
				),
				cmp.Compare(
					a.text,
					b.text,
				),
			)
		},
	)
	edits = slices.Compact(edits)
	var after []byte
	cursor := 0
	for i, edit := range edits {
		if edit.start < 0 || edit.end < edit.start || edit.end > len(before) {
			return nil, fmt.Errorf(
				"suggested edit has invalid byte range [%d:%d]",
				edit.start,
				edit.end,
			)
		}
		if i > 0 && (edit.start < edits[i-1].end || (edit.start == edits[i-1].start && edit.end == edits[i-1].end)) {
			return nil, fmt.Errorf(
				"conflicting suggested edits at byte %d",
				edit.start,
			)
		}
		after = append(
			after,
			before[cursor:edit.start]...,
		)
		after = append(
			after,
			edit.text...,
		)
		cursor = edit.end
	}
	return append(
		after,
		before[cursor:]...,
	), nil
}

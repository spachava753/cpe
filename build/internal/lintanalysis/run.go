// Package lintanalysis runs the build task's additional Go analyzers in-process.
package lintanalysis

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
)

// Options controls package loading, diagnostic output, and suggested fixes.
type Options struct {
	Output  io.Writer
	Dir     string
	Fix     bool
	Verbose bool
}

// Run analyzes packages, including tests, without changing global flags or exiting.
// Fix applies the first suggested fix for each diagnostic, then checks again so
// unfixable findings still fail the task. Loading or analysis errors prevent fixes.
func Run(ctx context.Context, analyzers []*analysis.Analyzer, patterns []string, opts Options) error {
	if len(patterns) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.Verbose {
		fmt.Fprintf(
			opts.Output,
			"additional analyzers: loading %d package patterns\n",
			len(patterns),
		)
	}
	initial, err := packages.Load(
		&packages.Config{
			Context: ctx,
			Mode:    packages.LoadAllSyntax | packages.NeedModule,
			Dir:     opts.Dir,
			Tests:   true,
		},
		patterns...,
	)
	if err != nil {
		return err
	}
	var loadErrors []error
	packages.Visit(
		initial,
		func(pkg *packages.Package) bool {
			for _, problem := range pkg.Errors {
				loadErrors = append(
					loadErrors,
					errors.New(problem.Error()),
				)
			}
			return true
		},
		nil,
	)
	if loadErr := errors.Join(loadErrors...); loadErr != nil {
		return loadErr
	}
	if len(initial) == 0 {
		return errors.New("no packages matched for additional analyzers")
	}
	graph, err := checker.Analyze(
		analyzers,
		initial,
		nil,
	)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var analysisErrors []error
	for act := range graph.All() {
		if opts.Verbose && act.IsRoot {
			fmt.Fprintf(
				opts.Output,
				"%s: %s\n",
				act,
				act.Duration,
			)
		}
		if act.Err != nil {
			analysisErrors = append(
				analysisErrors,
				fmt.Errorf(
					"%s: %w",
					act,
					act.Err,
				),
			)
		}
	}
	if err := errors.Join(analysisErrors...); err != nil {
		return err
	}
	if opts.Fix {
		changed, err := applyFixes(
			ctx,
			graph,
		)
		if err != nil {
			return err
		}
		if changed {
			opts.Fix = false
			return Run(
				ctx,
				analyzers,
				patterns,
				opts,
			)
		}
	}
	if err := graph.PrintText(
		opts.Output,
		-1,
	); err != nil {
		return err
	}
	for _, act := range graph.Roots {
		if len(act.Diagnostics) != 0 {
			return errors.New("additional analyzer findings")
		}
	}
	return nil
}

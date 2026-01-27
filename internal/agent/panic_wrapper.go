package agent

import (
	"context"
	"fmt"

	"github.com/spachava753/gai"
)

// PanicCatchingGenerator wraps a gai.Generator and catches any panics,
// converting them to errors so that dialogs can be saved even if a panic occurs
type PanicCatchingGenerator struct {
	gai.GeneratorWrapper
}

// Generate implements gai.Generator by catching panics and converting them to errors
func (p *PanicCatchingGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (resp gai.Response, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("generator panicked: %v", r)
		}
	}()

	return p.GeneratorWrapper.Generate(ctx, dialog, options)
}

// NewPanicCatchingGenerator wraps a generator with panic recovery
func NewPanicCatchingGenerator(g gai.Generator) gai.Generator {
	return &PanicCatchingGenerator{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
	}
}

// WithPanicCatching returns a WrapperFunc for use with gai.Wrap
func WithPanicCatching() gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewPanicCatchingGenerator(g)
	}
}

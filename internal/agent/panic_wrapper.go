package agent

import (
	"context"
	"fmt"

	"github.com/spachava753/gai"
)

// PanicCatchingGenerator wraps a gai.Generator and catches any panics,
// converting them to errors so that dialogs can be saved even if a panic occurs
type PanicCatchingGenerator struct {
	G gai.ToolCapableGenerator
}

// Register implements the gai.ToolRegister interface by delegating to the wrapped generator.
func (p *PanicCatchingGenerator) Register(tool gai.Tool) error {
	return p.G.Register(tool)
}

// Generate implements gai.Generator by catching panics and converting them to errors
func (p *PanicCatchingGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (resp gai.Response, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("generator panicked: %v", r)
		}
	}()

	return p.G.Generate(ctx, dialog, options)
}

// NewPanicCatchingGenerator wraps a generator with panic recovery
func NewPanicCatchingGenerator(g gai.ToolCapableGenerator) gai.ToolCapableGenerator {
	return &PanicCatchingGenerator{G: g}
}

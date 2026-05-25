package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/spachava753/gai"
)

const responsesPhaseConflictErrorText = "responses output contained multiple assistant message phases"

const defaultResponsesPhaseRetryMaxRetries = 5

type responsesPhaseRetryGenerator struct {
	gai.GeneratorWrapper
	maxRetries int
}

func newResponsesPhaseRetryGenerator(inner gai.ToolCallingGenerator) *responsesPhaseRetryGenerator {
	return &responsesPhaseRetryGenerator{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: inner},
		maxRetries:       defaultResponsesPhaseRetryMaxRetries,
	}
}

func (g *responsesPhaseRetryGenerator) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	maxAttempts := max(g.maxRetries, 0) + 1

	var resp gai.Response
	var err error
	for range maxAttempts {
		if ctx.Err() != nil {
			return resp, ctx.Err()
		}

		resp, err = g.GeneratorWrapper.Generate(ctx, dialog, opts)
		if err == nil {
			return resp, nil
		}
		if !isResponsesPhaseConflictError(err) {
			return resp, err
		}
	}

	return resp, fmt.Errorf("%d generation attempts: %w", maxAttempts, err)
}

func isResponsesPhaseConflictError(err error) bool {
	return err != nil && strings.Contains(err.Error(), responsesPhaseConflictErrorText)
}

var _ gai.ToolCallingGenerator = (*responsesPhaseRetryGenerator)(nil)

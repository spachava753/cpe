package agent

import (
	"context"
	"strings"

	"github.com/spachava753/gai"
)

const responsesPhaseConflictErrorText = "responses output contained multiple assistant message phases"

const defaultResponsesPhaseRetryMaxRetries = 5

type responsesPhaseRetryGenerator struct {
	gai.GeneratorWrapper
	maxRetries int
}

func newResponsesPhaseRetryGenerator(inner gai.ToolCapableGenerator) *responsesPhaseRetryGenerator {
	return &responsesPhaseRetryGenerator{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: inner},
		maxRetries:       defaultResponsesPhaseRetryMaxRetries,
	}
}

func (g *responsesPhaseRetryGenerator) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	maxRetries := max(g.maxRetries, 0)

	var resp gai.Response
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return resp, err
		}

		var err error
		resp, err = g.GeneratorWrapper.Generate(ctx, dialog, opts)
		if err == nil || !isResponsesPhaseConflictError(err) || attempt == maxRetries {
			return resp, err
		}
	}

	return resp, nil
}

func isResponsesPhaseConflictError(err error) bool {
	return err != nil && strings.Contains(err.Error(), responsesPhaseConflictErrorText)
}

var _ gai.ToolCapableGenerator = (*responsesPhaseRetryGenerator)(nil)

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/trace"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

const flightRecorderTracePattern = "cpe-generator-*.trace"

type traceHintErr struct {
	cause error
	hint  string
}

func (e *traceHintErr) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *traceHintErr) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *traceHintErr) GenerationHint() string {
	if e == nil {
		return ""
	}
	return e.hint
}

type flightRecorderGenerator struct {
	gai.GeneratorWrapper
	modelType string
	modelID   string
	cfg       *config.ResolvedFlightRecorderConfig
}

func WithFlightRecorder(model config.Model, cfg *config.ResolvedFlightRecorderConfig) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return &flightRecorderGenerator{
			GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
			modelType:        model.Type,
			modelID:          model.ID,
			cfg:              cfg,
		}
	}
}

func (g *flightRecorderGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	if g.cfg == nil || !g.cfg.Enabled {
		return g.GeneratorWrapper.Generate(ctx, dialog, options)
	}

	fr := trace.NewFlightRecorder(trace.FlightRecorderConfig{
		MinAge:   g.cfg.MinAge,
		MaxBytes: g.cfg.MaxBytes,
	})
	if err := fr.Start(); err != nil {
		return g.GeneratorWrapper.Generate(ctx, dialog, options)
	}
	defer fr.Stop()

	taskCtx, task := trace.NewTask(ctx, "cpe.agent.generate")
	trace.Log(taskCtx, "cpe.model.type", g.modelType)
	trace.Log(taskCtx, "cpe.model.id", g.modelID)
	trace.Logf(taskCtx, "cpe.dialog.messages", "%d", len(dialog))

	var (
		resp gai.Response
		err  error
	)
	trace.WithRegion(taskCtx, "cpe.agent.generate", func() {
		resp, err = g.GeneratorWrapper.Generate(taskCtx, dialog, options)
	})

	if err == nil || errors.Is(err, context.Canceled) {
		task.End()
		return resp, err
	}

	trace.Logf(taskCtx, "cpe.generate.error", "%T: %v", err, err)
	task.End()
	path, snapshotErr := writeFlightRecorderTrace(fr)
	if snapshotErr != nil {
		return resp, &traceHintErr{
			cause: err,
			hint:  fmt.Sprintf("Flight trace capture failed: %v", snapshotErr),
		}
	}
	return resp, &traceHintErr{
		cause: err,
		hint:  fmt.Sprintf("Flight trace saved to %s", path),
	}
}

func writeFlightRecorderTrace(fr *trace.FlightRecorder) (string, error) {
	if fr == nil || !fr.Enabled() {
		return "", errors.New("flight recorder is inactive")
	}

	f, err := os.CreateTemp(os.TempDir(), flightRecorderTracePattern)
	if err != nil {
		return "", fmt.Errorf("creating trace file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	if _, err := fr.WriteTo(f); err != nil {
		return "", fmt.Errorf("writing trace snapshot: %w", err)
	}
	return f.Name(), nil
}

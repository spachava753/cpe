package agent

import (
	"context"
	"errors"
	"iter"
	"strings"
	"sync"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

// ModelSelection is an applied selection snapshot, safe to display during work.
type ModelSelection struct {
	Name          string
	Model         config.Model
	ContextTokens int
}

type queuedModel struct {
	name      string
	model     config.Model
	generator gai.Generator
}

type modelQueue struct {
	mu   sync.Mutex
	next *queuedModel
}

// QueueModel schedules a selection at the next generation boundary. It is the
// only Agent mutation safe during work. The latest queued selection wins; the
// current request and its tools finish with their original settings.
func (a *Agent) QueueModel(name string, model config.Model, generator gai.Generator) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("model profile name is required")
	}
	if err := checkModel(model, generator); err != nil {
		return err
	}
	a.selection.mu.Lock()
	defer a.selection.mu.Unlock()
	a.selection.next = &queuedModel{name, model, generator}
	return nil
}

// ApplyQueuedModel applies a pending selection between operations. Prompt and
// Compact also do this at generation boundaries and before returning. Callers
// may drain once more after joining work to handle selections queued during its
// completion notification. Like SetModel, this must not run concurrently with work.
func (a *Agent) ApplyQueuedModel() (*ModelSelection, error) {
	a.selection.mu.Lock()
	next := a.selection.next
	a.selection.next = nil
	a.selection.mu.Unlock()
	if next == nil {
		return nil, nil
	}
	if err := a.SetModel(next.model, next.generator); err != nil {
		return nil, err
	}
	a.selectedName = next.name
	selected := a.SelectedModel()
	a.notify(Event{Kind: EventModel, Selection: selected})
	return selected, nil
}

func checkModel(model config.Model, generator gai.Generator) error {
	if generator == nil || model.ID == "" {
		return errors.New("model ID and generator are required")
	}
	if err := model.ValidateBudget(); err != nil {
		return err
	}
	_, err := model.WithReasoningEffort(model.ReasoningEffort)
	return err
}

// conversationGenerator chooses the active provider once per generation. It
// forwards streams without pretending nonstreaming providers yield stream chunks.
type conversationGenerator struct{ agent *Agent }

func (g conversationGenerator) Generate(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
	generator := g.agent.metered("conversation")
	if stream, ok := generator.(gai.StreamingGenerator); ok {
		return (&gai.StreamingAdapter{S: conversationStream{stream, g.agent}}).Generate(ctx, req)
	}
	return generator.Generate(ctx, req)
}

type conversationStream struct {
	source gai.StreamingGenerator
	agent  *Agent
}

func (s conversationStream) Stream(ctx context.Context, req gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return func(yield func(gai.StreamChunk) bool) {
		for chunk := range s.source.Stream(ctx, req) {
			b := chunk.Block
			if chunk.Err == nil && b.BlockType == gai.Content && b.ModalityType == gai.Text && b.Content != nil {
				s.agent.notify(Event{Kind: EventDelta, Text: b.Content.String()})
			}
			if !yield(chunk) {
				return
			}
		}
	}
}

// SelectedModel returns the last named model selection and current settings.
// Call only between operations. Unnamed initial/direct SetModel selections return
// nil; the caller already knows their display name.
func (a *Agent) SelectedModel() *ModelSelection {
	if a.selectedName == "" {
		return nil
	}
	return &ModelSelection{Name: a.selectedName, Model: a.opts.Model, ContextTokens: a.ContextEstimate()}
}

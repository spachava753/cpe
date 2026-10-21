package agent

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"
)

func (s *session) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, ok := s.record.KV[key]
	if !ok {
		return nil, fmt.Errorf(
			"KV key %q not found",
			key,
		)
	}
	return bytes.Clone(value), nil
}

// TODO: Revisit whole-record KV persistence: changing one key currently rewrites
// all saved values and conversation history, which becomes costly with large logs.
func (s *session) Store(ctx context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	record := s.record
	record.KV = make(
		map[string][]byte,
		len(s.record.KV)+1,
	)
	maps.Copy(
		record.KV,
		s.record.KV,
	)
	record.KV[key] = bytes.Clone(value)
	record.Info.UpdatedAt = time.Now().UTC()
	if err := s.deps.History.Save(
		ctx,
		record,
	); err != nil {
		return err
	}
	s.record = record
	return nil
}

func (s *session) Keys(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(s.record.KV)), nil
}

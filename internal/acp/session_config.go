package acp

import (
	"context"
	"fmt"
	"slices"

	"github.com/coder/acp-go-sdk"

	"github.com/spachava753/cpe/internal/config"
)

const (
	modelRefConfigId      acp.SessionConfigId = "modelRef"
	thinkingLevelConfigId acp.SessionConfigId = "thinkingLevel"
)

// SetSessionConfigOption implements [acp.Agent].
//
// TODO: we should probably expose more options like thinking mode, tool choice, etc. and wire up defaults from the config
// TODO: we should save after every session config option change
// TODO: we should exclude thinking options if not configured
func (a *Agent) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	if params.ValueId == nil {
		return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("unsupported session config option type")
	}
	s, ok := a.activeSessions.Load(params.ValueId.SessionId)
	if !ok {
		panic(fmt.Sprintf("unknown session: %s", params.ValueId.SessionId)) // TODO: should we panic or return error?
	}
	var modelRefVal, thinkingVal string
	if err := s.Do(func(t *session) error {
		modelRefVal = t.modelRef
		thinkingVal = t.thinkingLevel
		return nil
	}); err != nil {
		panic("unreachable")
	}
	switch params.ValueId.ConfigId {
	case modelRefConfigId:
		modelRefVal = string(params.ValueId.Value)
		if err := a.db.SetACPSessionModelRef(ctx, params.ValueId.SessionId, modelRefVal); err != nil {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("could not persist model config: %v", err)
		}
		if err := s.Do(func(t *session) error {
			if t.runtime != nil {
				if err := t.runtime.Close(); err != nil {
					return err
				}
			}
			t.modelRef = modelRefVal
			t.runtime = nil
			return nil
		}); err != nil {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("could not update model config: %v", err)
		}
	case thinkingLevelConfigId:
		thinkingVal = string(params.ValueId.Value)
		if err := a.db.SetACPSessionThinkingLevel(ctx, params.ValueId.SessionId, thinkingVal); err != nil {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("could not persist model config: %v", err)
		}
		if err := s.Do(func(t *session) error {
			if t.runtime != nil {
				if err := t.runtime.Close(); err != nil {
					return err
				}
			}
			t.thinkingLevel = thinkingVal
			t.runtime = nil
			return nil
		}); err != nil {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("could not update model config: %v", err)
		}
	default:
		panic(fmt.Sprintf("unknown config id: %s", params.ValueId.ConfigId))
	}
	return acp.SetSessionConfigOptionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: a.configOption(params.ValueId.SessionId, modelRefConfigId, modelRefVal),
			},
			{
				Select: a.configOption(params.ValueId.SessionId, thinkingLevelConfigId, thinkingVal),
			},
		},
	}, nil
}

func (a *Agent) configOption(sessionId acp.SessionId, configId acp.SessionConfigId, currentVal string) *acp.SessionConfigOptionSelect {
	switch configId {
	case modelRefConfigId:
		opts := make(acp.SessionConfigSelectOptionsUngrouped, len(a.rawCfg.Models))
		for i, m := range a.rawCfg.Models {
			// TODO: *m.InputCostPerMillion and *m.OutputCostPerMillion can cause panic, fix with checking nil and using string builder
			opts[i] = acp.SessionConfigSelectOption{
				Description: new(fmt.Sprintf(`Type: %s
Base Url: %s
Context Window: %d
Input Cost: %f
Output Cost: %f`, m.Type, m.BaseUrl, m.ContextWindow, *m.InputCostPerMillion, *m.OutputCostPerMillion)),
				Name:  m.DisplayName,
				Value: acp.SessionConfigValueId(m.Ref),
			}
		}
		return &acp.SessionConfigOptionSelect{
			Category:     new(acp.SessionConfigOptionCategoryModel),
			CurrentValue: acp.SessionConfigValueId(currentVal),
			Description:  new("Choose model"),
			Id:           modelRefConfigId,
			Name:         "Model",
			Options: acp.SessionConfigSelectOptions{
				Ungrouped: &opts,
			},
			Type: "select",
		}
	case thinkingLevelConfigId:
		// get modelRef first, if not set, there is an issue
		var modelRefVal string
		s, ok := a.activeSessions.Load(sessionId)
		if !ok {
			panic(fmt.Sprintf("unknown session: %s", sessionId)) // TODO: should we panic or return error?
		}
		if err := s.Do(func(t *session) error {
			modelRefVal = t.modelRef
			return nil
		}); err != nil {
			panic("unreachable")
		}

		if modelRefVal == "" {
			panic("modelRefVal is empty")
		}

		idx := slices.IndexFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
			return m.Ref == modelRefVal
		})

		if idx == -1 {
			panic("model not found")
		}

		opts := make(acp.SessionConfigSelectOptionsUngrouped, len(a.rawCfg.Models[idx].ThinkingValues))
		for i, tv := range a.rawCfg.Models[idx].ThinkingValues {
			opts[i] = acp.SessionConfigSelectOption{
				Description: new(tv.Description),
				Name:        tv.Name,
				Value:       acp.SessionConfigValueId(tv.Value),
			}
		}
		return &acp.SessionConfigOptionSelect{
			Category:     new(acp.SessionConfigOptionCategoryThoughtLevel),
			CurrentValue: acp.SessionConfigValueId(currentVal),
			Description:  new("Choose thinking level"),
			Id:           thinkingLevelConfigId,
			Name:         "Thinking level",
			Options: acp.SessionConfigSelectOptions{
				Ungrouped: &opts,
			},
			Type: "select",
		}
	default:
		panic(fmt.Sprintf("unknown config id: %s", configId))
	}
}

// SetSessionMode implements [acp.Agent].
//
// TODO: maybe we should have a read-only mode?
func (a *Agent) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

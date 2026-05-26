package acp

import (
	"context"
	"fmt"

	"github.com/coder/acp-go-sdk"
)

const (
	modelRefConfigId acp.SessionConfigId = "modelRef"
)

// SetSessionConfigOption implements [acp.Agent].
//
// TODO: we should probably expose more options like thinking mode, tool choice, etc. and wire up defaults from the config
func (a *Agent) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	if params.ValueId == nil {
		return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("unsupported session config option type")
	}
	s, ok := a.activeSessions.Load(params.ValueId.SessionId)
	if !ok {
		panic(fmt.Sprintf("unknown session: %s", params.ValueId.SessionId)) // TODO: should we panic or return error?
	}
	switch params.ValueId.ConfigId {
	case modelRefConfigId:
		modelRefVal := string(params.ValueId.Value)
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
	default:
		panic(fmt.Sprintf("unknown config id: %s", params.ValueId.ConfigId))
	}
	return acp.SetSessionConfigOptionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: a.configOption(modelRefConfigId, string(params.ValueId.Value)),
			},
		},
	}, nil
}

func (a *Agent) configOption(configId acp.SessionConfigId, currentVal string) *acp.SessionConfigOptionSelect {
	switch configId {
	case modelRefConfigId:
		opts := make(acp.SessionConfigSelectOptionsUngrouped, len(a.rawCfg.Models))
		for i, m := range a.rawCfg.Models {
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

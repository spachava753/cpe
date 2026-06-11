package acp

import (
	"context"
	"fmt"
	"log/slog"
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
// TODO: we should probably expose more options like tool choice, etc. and wire up defaults from the config
func (a *Agent) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	if params.ValueId == nil {
		return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("unsupported session config option type")
	}
	s, err := a.activeSession(params.ValueId.SessionId)
	if err != nil {
		return acp.SetSessionConfigOptionResponse{}, err
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
		idx := slices.IndexFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
			return m.Ref == modelRefVal
		})
		if idx == -1 {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("invalid model config value: %s", modelRefVal)
		}
		thinkingVal = ""
		if len(a.rawCfg.Models[idx].ThinkingValues) > 0 {
			thinkingVal = a.rawCfg.Models[idx].ThinkingValues[0].Value
		}
		if err := a.db.SetACPSessionModelRef(ctx, params.ValueId.SessionId, modelRefVal); err != nil {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("could not persist model config: %v", err)
		}
		if err := a.db.SetACPSessionThinkingLevel(ctx, params.ValueId.SessionId, thinkingVal); err != nil {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("could not persist thinking config: %v", err)
		}
		if err := s.Do(func(t *session) error {
			if t.runtime != nil {
				if err := t.runtime.Close(); err != nil {
					return err
				}
			}
			t.modelRef = modelRefVal
			t.thinkingLevel = thinkingVal
			t.runtime = nil
			return nil
		}); err != nil {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("could not update model config: %v", err)
		}
	case thinkingLevelConfigId:
		thinkingVal = string(params.ValueId.Value)
		idx := slices.IndexFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
			return m.Ref == modelRefVal
		})
		if idx == -1 {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("could not validate thinking config for model: %s", modelRefVal)
		}
		if !slices.ContainsFunc(a.rawCfg.Models[idx].ThinkingValues, func(tv config.ThinkingValueConfig) bool {
			return tv.Value == thinkingVal
		}) {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("invalid thinking level config value: %s", thinkingVal)
		}
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
		return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("unknown config id: %s", params.ValueId.ConfigId)
	}
	return acp.SetSessionConfigOptionResponse{
		ConfigOptions: a.configOptions(ctx, params.ValueId.SessionId),
	}, nil
}

func (a *Agent) configOptions(ctx context.Context, sessionId acp.SessionId) []acp.SessionConfigOption {
	s, err := a.db.GetACPSession(ctx, sessionId)
	if err != nil {
		panic(fmt.Sprintf("error fetching session %s: %v", sessionId, err))
	}
	var sessionConfigs []acp.SessionConfigOption

	// model not picked yet
	if s.ModelRef == "" {
		slog.Info("model not picked yet", slog.String("session_id", string(sessionId)))

		opts := a.modelSelectOptions()

		sessionConfigs = append(sessionConfigs, acp.SessionConfigOption{
			Select: &acp.SessionConfigOptionSelect{
				Category:     new(acp.SessionConfigOptionCategoryModel),
				CurrentValue: acp.SessionConfigValueId(""),
				Description:  new("Choose model"),
				Id:           modelRefConfigId,
				Name:         "Model",
				Options: acp.SessionConfigSelectOptions{
					Ungrouped: &opts,
				},
				Type: "select",
			},
		})
		return sessionConfigs
	}

	// check if stale session config, if true, then just return model picker
	if !slices.ContainsFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
		return m.Ref == s.ModelRef
	}) {
		slog.Info(
			"stale config value",
			slog.String("session_id", string(sessionId)),
			slog.String("config_id", string(modelRefConfigId)),
			slog.String("value", string(s.ModelRef)),
		)
		opts := a.modelSelectOptions()

		sessionConfigs = append(sessionConfigs, acp.SessionConfigOption{
			Select: &acp.SessionConfigOptionSelect{
				Category:     new(acp.SessionConfigOptionCategoryModel),
				CurrentValue: acp.SessionConfigValueId(""),
				Description:  new("Choose model"),
				Id:           modelRefConfigId,
				Name:         "Model",
				Options: acp.SessionConfigSelectOptions{
					Ungrouped: &opts,
				},
				Type: "select",
			},
		})
		return sessionConfigs
	}

	// model was set, and is valid value
	modelOpts := a.modelSelectOptions()

	sessionConfigs = append(sessionConfigs, acp.SessionConfigOption{
		Select: &acp.SessionConfigOptionSelect{
			Category:     new(acp.SessionConfigOptionCategoryModel),
			CurrentValue: acp.SessionConfigValueId(s.ModelRef),
			Description:  new("Choose model"),
			Id:           modelRefConfigId,
			Name:         "Model",
			Options: acp.SessionConfigSelectOptions{
				Ungrouped: &modelOpts,
			},
			Type: "select",
		},
	})

	idx := slices.IndexFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
		return m.Ref == s.ModelRef
	})
	m := a.rawCfg.Models[idx]

	// if thinking level not set and no thinking values available, not need to set thinking config option
	if len(m.ThinkingValues) == 0 {
		// TODO: should we update stale thinking level here?
		return sessionConfigs
	}

	if !slices.ContainsFunc(m.ThinkingValues, func(tv config.ThinkingValueConfig) bool {
		return tv.Value == s.ThinkingLevel
	}) {
		slog.Info(
			"stale config value",
			slog.String("session_id", string(sessionId)),
			slog.String("config_id", string(modelRefConfigId)),
			slog.String("value", string(s.ModelRef)),
		)
		// TODO: should we update stale thinking level here?
		s.ThinkingLevel = m.ThinkingValues[0].Value
	}

	thinkingOpts := make(acp.SessionConfigSelectOptionsUngrouped, len(m.ThinkingValues))
	for i, tv := range m.ThinkingValues {
		thinkingOpts[i] = acp.SessionConfigSelectOption{
			Description: new(tv.Description),
			Name:        tv.Name,
			Value:       acp.SessionConfigValueId(tv.Value),
		}
	}

	sessionConfigs = append(sessionConfigs, acp.SessionConfigOption{
		Select: &acp.SessionConfigOptionSelect{
			Category:     new(acp.SessionConfigOptionCategoryThoughtLevel),
			CurrentValue: acp.SessionConfigValueId(s.ThinkingLevel),
			Description:  new("Choose thinking level"),
			Id:           thinkingLevelConfigId,
			Name:         "Thinking level",
			Options: acp.SessionConfigSelectOptions{
				Ungrouped: &thinkingOpts,
			},
			Type: "select",
		},
	})

	return sessionConfigs
}

// modelSelectOptions builds the model picker options for every configured
// model profile. Cost fields are optional in the config, so nil costs are
// rendered as "n/a" instead of being dereferenced.
func (a *Agent) modelSelectOptions() acp.SessionConfigSelectOptionsUngrouped {
	opts := make(acp.SessionConfigSelectOptionsUngrouped, len(a.rawCfg.Models))
	for i, m := range a.rawCfg.Models {
		opts[i] = acp.SessionConfigSelectOption{
			Description: new(fmt.Sprintf(`Type: %s
Base Url: %s
Context Window: %d
Input Cost: %s
Output Cost: %s`, m.Type, m.BaseUrl, m.ContextWindow, formatOptionalCost(m.InputCostPerMillion), formatOptionalCost(m.OutputCostPerMillion))),
			Name:  m.DisplayName,
			Value: acp.SessionConfigValueId(m.Ref),
		}
	}
	return opts
}

func formatOptionalCost(cost *float64) string {
	if cost == nil {
		return "n/a"
	}
	return fmt.Sprintf("%0.2f", *cost)
}

// SetSessionMode implements [acp.Agent].
//
// Modes are being superseded by session config options.
// We just provide a default response for older clients.
func (a *Agent) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

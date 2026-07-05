package acp

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/spachava753/acp-sdk/acp"

	"github.com/spachava753/cpe/internal/config"
)

const (
	modelRefConfigId      acp.SessionConfigId = "modelRef"
	thinkingLevelConfigId acp.SessionConfigId = "thinkingLevel"
)

// SetSessionConfigOption implements [acp.SessionHandler].
//
// TODO: we should probably expose more options like tool choice, etc. and wire up defaults from the config
func (a *Agent) SetSessionConfigOption(ctx context.Context, params *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	s, err := a.activeSession(params.SessionID)
	if err != nil {
		return nil, err
	}
	if err := a.refreshAvailableSkillCommands(ctx, params.SessionID, s); err != nil {
		return nil, fmt.Errorf("could not refresh available skill commands: %v", err)
	}
	value, ok := sessionConfigValueString(params.Value)
	if !ok {
		return nil, fmt.Errorf("unsupported session config option type")
	}
	var modelRefVal, thinkingVal string
	if err := s.Do(func(t *session) error {
		modelRefVal = t.model
		thinkingVal = t.thinking
		return nil
	}); err != nil {
		panic("unreachable")
	}
	switch params.ConfigID {
	case modelRefConfigId:
		modelRefVal = value
		idx := slices.IndexFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
			return m.Ref == modelRefVal
		})
		if idx == -1 {
			return nil, fmt.Errorf("invalid model config value: %s", modelRefVal)
		}
		thinkingVal = ""
		if len(a.rawCfg.Models[idx].ThinkingValues) > 0 {
			thinkingVal = a.rawCfg.Models[idx].ThinkingValues[0].Value
		}
		if err := a.db.SetACPSessionModelRef(ctx, params.SessionID, modelRefVal); err != nil {
			return nil, fmt.Errorf("could not persist model config: %v", err)
		}
		if err := a.db.SetACPSessionThinkingLevel(ctx, params.SessionID, thinkingVal); err != nil {
			return nil, fmt.Errorf("could not persist thinking config: %v", err)
		}
		if err := s.Do(func(t *session) error {
			if t.runtime != nil {
				if err := t.runtime.Close(); err != nil {
					return err
				}
			}
			t.model = modelRefVal
			t.thinking = thinkingVal
			t.runtime = nil
			return nil
		}); err != nil {
			return nil, fmt.Errorf("could not update model config: %v", err)
		}
	case thinkingLevelConfigId:
		thinkingVal = value
		idx := slices.IndexFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
			return m.Ref == modelRefVal
		})
		if idx == -1 {
			return nil, fmt.Errorf("could not validate thinking config for model: %s", modelRefVal)
		}
		if !slices.ContainsFunc(a.rawCfg.Models[idx].ThinkingValues, func(tv config.ThinkingValueConfig) bool {
			return tv.Value == thinkingVal
		}) {
			return nil, fmt.Errorf("invalid thinking level config value: %s", thinkingVal)
		}
		if err := a.db.SetACPSessionThinkingLevel(ctx, params.SessionID, thinkingVal); err != nil {
			return nil, fmt.Errorf("could not persist model config: %v", err)
		}
		if err := s.Do(func(t *session) error {
			if t.runtime != nil {
				if err := t.runtime.Close(); err != nil {
					return err
				}
			}
			t.thinking = thinkingVal
			t.runtime = nil
			return nil
		}); err != nil {
			return nil, fmt.Errorf("could not update model config: %v", err)
		}
	default:
		return nil, fmt.Errorf("unknown config id: %s", params.ConfigID)
	}
	return &acp.SetSessionConfigOptionResponse{
		ConfigOptions: a.configOptions(ctx, params.SessionID),
	}, nil
}

func sessionConfigValueString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case acp.SessionConfigValueId:
		return string(v), true
	default:
		return "", false
	}
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
		option := acp.SelectSessionConfigOption(modelRefConfigId, "Model", acp.SessionConfigValueId(""), acp.SessionConfigSelectOptions{Ungrouped: &opts})
		option.Category = new(acp.SessionConfigOptionCategoryModel)
		option.Description = new("Choose model")
		sessionConfigs = append(sessionConfigs, option)
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
		option := acp.SelectSessionConfigOption(modelRefConfigId, "Model", acp.SessionConfigValueId(""), acp.SessionConfigSelectOptions{Ungrouped: &opts})
		option.Category = new(acp.SessionConfigOptionCategoryModel)
		option.Description = new("Choose model")
		sessionConfigs = append(sessionConfigs, option)
		return sessionConfigs
	}

	// model was set, and is valid value
	modelOpts := a.modelSelectOptions()
	modelOption := acp.SelectSessionConfigOption(modelRefConfigId, "Model", acp.SessionConfigValueId(s.ModelRef), acp.SessionConfigSelectOptions{Ungrouped: &modelOpts})
	modelOption.Category = new(acp.SessionConfigOptionCategoryModel)
	modelOption.Description = new("Choose model")
	sessionConfigs = append(sessionConfigs, modelOption)

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

	thinkingOpts := make(acp.UngroupedSessionConfigSelectOptions, len(m.ThinkingValues))
	for i, tv := range m.ThinkingValues {
		thinkingOpts[i] = acp.SessionConfigSelectOption{
			Description: new(tv.Description),
			Name:        tv.Name,
			Value:       acp.SessionConfigValueId(tv.Value),
		}
	}

	thinkingOption := acp.SelectSessionConfigOption(thinkingLevelConfigId, "Thinking level", acp.SessionConfigValueId(s.ThinkingLevel), acp.SessionConfigSelectOptions{Ungrouped: &thinkingOpts})
	thinkingOption.Category = new(acp.SessionConfigOptionCategoryThoughtLevel)
	thinkingOption.Description = new("Choose thinking level")
	sessionConfigs = append(sessionConfigs, thinkingOption)

	return sessionConfigs
}

// modelSelectOptions builds the model picker options for every configured
// model profile. Cost fields are optional in the config, so nil costs are
// rendered as "n/a" instead of being dereferenced.
func (a *Agent) modelSelectOptions() acp.UngroupedSessionConfigSelectOptions {
	opts := make(acp.UngroupedSessionConfigSelectOptions, len(a.rawCfg.Models))
	for i, m := range a.rawCfg.Models {
		opts[i] = acp.SessionConfigSelectOption{
			Description: new(modelSelectDescription(m)),
			Name:        m.DisplayName,
			Value:       acp.SessionConfigValueId(m.Ref),
		}
	}
	return opts
}

func modelSelectDescription(m config.ModelConfig) string {
	location := fmt.Sprintf("Base Url: %s", m.BaseUrl)
	if m.Vertex != nil {
		location = fmt.Sprintf("Vertex Project: %s\nVertex Region: %s", m.Vertex.ProjectID, m.Vertex.Region)
	}
	return fmt.Sprintf(`Type: %s
%s
Context Window: %d
Input Cost: %s
Output Cost: %s`, m.Type, location, m.ContextWindow, formatOptionalCost(m.InputCostPerMillion), formatOptionalCost(m.OutputCostPerMillion))
}

func formatOptionalCost(cost *float64) string {
	if cost == nil {
		return "n/a"
	}
	return fmt.Sprintf("%0.2f", *cost)
}

// SetSessionMode implements [acp.SessionHandler].
//
// Modes are being superseded by session config options.
// We just provide a default response for older clients.
func (a *Agent) SetSessionMode(ctx context.Context, params *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
	return &acp.SetSessionModeResponse{}, nil
}

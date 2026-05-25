package acp

import (
	"context"
	"fmt"

	"github.com/coder/acp-go-sdk"
)

// SetSessionConfigOption implements [acp.Agent].
func (a *Agent) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	s, ok := a.activeSessions.Load(params.Boolean.SessionId)
	if !ok {
		panic(fmt.Sprintf("unknown session: %s", params.Boolean.SessionId)) // TODO: should we panic or return error?
	}
	switch params.ValueId.ConfigId {
	case modelRef:
		s.Do(func(t session) error {
			t.modelRef = string(params.ValueId.Value)
			return nil
		})
	default:
		panic(fmt.Sprintf("unknown config id: %s", params.ValueId.ConfigId))
	}
	return acp.SetSessionConfigOptionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: a.configOption(modelRef, string(params.ValueId.Value)),
			},
		},
	}, nil
}

func (a *Agent) configOption(configId acp.SessionConfigId, currentVal string) *acp.SessionConfigOptionSelect {
	switch configId {
	case modelRef:
		return &acp.SessionConfigOptionSelect{
			Category:     new(acp.SessionConfigOptionCategoryModel),
			CurrentValue: acp.SessionConfigValueId(currentVal),
			Description:  new("Choose model"),
			Id:           modelRef,
			Name:         "Model",
			Options: acp.SessionConfigSelectOptions{
				Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
					{
						Description: new(string), // TODO: need to fill out these values
						Name:        "",
						Value:       "",
					},
				},
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

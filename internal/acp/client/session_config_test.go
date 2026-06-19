package client

import (
	"strings"
	"testing"

	"github.com/nalgeon/be"
	"github.com/spachava753/acp-sdk/acp"
)

func TestValidateConfigValue(t *testing.T) {
	t.Run("accepts configured value", func(t *testing.T) {
		modelOptions := acp.UngroupedSessionConfigSelectOptions{
			{Name: "First", Value: "first-model"},
			{Name: "Second", Value: "second-model"},
		}
		options := []acp.SessionConfigOption{
			acp.SelectSessionConfigOption(modelRefConfigID, "Model", "first-model", acp.SessionConfigSelectOptions{Ungrouped: &modelOptions}),
		}

		err := validateConfigValue(options, modelRefConfigID, "second-model")
		be.Err(t, err, nil)
	})

	t.Run("rejects missing option", func(t *testing.T) {
		err := validateConfigValue(nil, modelRefConfigID, "first-model")
		be.True(t, err != nil)
		be.True(t, strings.Contains(err.Error(), `session config option "modelRef" is not available`))
	})

	t.Run("rejects option without values", func(t *testing.T) {
		options := []acp.SessionConfigOption{
			{ID: modelRefConfigID},
		}

		err := validateConfigValue(options, modelRefConfigID, "first-model")
		be.True(t, err != nil)
		be.True(t, strings.Contains(err.Error(), `session config option "modelRef" has no selectable values`))
	})

	t.Run("rejects value outside option list", func(t *testing.T) {
		modelOptions := acp.UngroupedSessionConfigSelectOptions{
			{Name: "First", Value: "first-model"},
			{Name: "Second", Value: "second-model"},
		}
		options := []acp.SessionConfigOption{
			acp.SelectSessionConfigOption(modelRefConfigID, "Model", "first-model", acp.SessionConfigSelectOptions{Ungrouped: &modelOptions}),
		}

		err := validateConfigValue(options, modelRefConfigID, "missing-model")
		be.True(t, err != nil)
		be.True(t, strings.Contains(err.Error(), `invalid modelRef "missing-model"; valid values: first-model, second-model`))
	})
}

func TestFindConfigOption(t *testing.T) {
	modelOptions := acp.UngroupedSessionConfigSelectOptions{{Name: "Model", Value: "model"}}
	thinkingOptions := acp.UngroupedSessionConfigSelectOptions{{Name: "High", Value: "high"}}
	options := []acp.SessionConfigOption{
		acp.SelectSessionConfigOption(modelRefConfigID, "Model", "model", acp.SessionConfigSelectOptions{Ungrouped: &modelOptions}),
		acp.SelectSessionConfigOption(thinkingLevelConfigID, "Thinking level", "high", acp.SessionConfigSelectOptions{Ungrouped: &thinkingOptions}),
	}

	option, ok := findConfigOption(options, thinkingLevelConfigID)
	be.True(t, ok)
	be.Equal(t, option.ID, thinkingLevelConfigID)

	_, ok = findConfigOption(options, "missing")
	be.Equal(t, ok, false)
}

func TestConfigValues(t *testing.T) {
	modelOptions := acp.UngroupedSessionConfigSelectOptions{
		{Name: "First", Value: "first-model"},
		{Name: "Second", Value: "second-model"},
	}
	option := acp.SelectSessionConfigOption(modelRefConfigID, "Model", "first-model", acp.SessionConfigSelectOptions{Ungrouped: &modelOptions})

	be.Equal(t, configValues(option), []string{"first-model", "second-model"})
	be.Equal(t, configValues(acp.SessionConfigOption{}), []string(nil))
}

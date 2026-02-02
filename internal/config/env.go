package config

import "os"

// expandEnvironmentVariables expands environment variables in configuration values.
// Note: Map keys are also expanded, which means if two keys expand to the same value,
// one will be silently overwritten (last-write-wins based on map iteration order).
func (c *RawConfig) expandEnvironmentVariables() {
	// Expand in model configurations
	for i := range c.Models {
		model := &c.Models[i]
		model.BaseUrl = os.ExpandEnv(model.BaseUrl)
		model.ApiKeyEnv = os.ExpandEnv(model.ApiKeyEnv)
		model.SystemPromptPath = os.ExpandEnv(model.SystemPromptPath)
		expandPatchRequestConfig(model.PatchRequest)
		expandCodeModeConfig(model.CodeMode)
	}

	// Expand in MCP server configurations
	for name, server := range c.MCPServers {
		server.Command = os.ExpandEnv(server.Command)
		server.Args = expandStringSlice(server.Args)
		server.URL = os.ExpandEnv(server.URL)
		server.Env = expandStringMap(server.Env)
		server.Headers = expandStringMap(server.Headers)
		c.MCPServers[name] = server
	}

	// Expand in subagent configuration
	if c.Subagent != nil {
		c.Subagent.OutputSchemaPath = os.ExpandEnv(c.Subagent.OutputSchemaPath)
	}

	// Expand in defaults
	c.Defaults.SystemPromptPath = os.ExpandEnv(c.Defaults.SystemPromptPath)
	expandCodeModeConfig(c.Defaults.CodeMode)
}

// expandStringSlice expands environment variables in a string slice
func expandStringSlice(slice []string) []string {
	if slice == nil {
		return nil
	}
	expanded := make([]string, len(slice))
	for i, s := range slice {
		expanded[i] = os.ExpandEnv(s)
	}
	return expanded
}

// expandStringMap expands environment variables in both keys and values of a map.
// Note: If multiple keys expand to the same value, last-write-wins based on map iteration order.
func expandStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	expanded := make(map[string]string)
	for k, v := range m {
		expanded[os.ExpandEnv(k)] = os.ExpandEnv(v)
	}
	return expanded
}

// expandPatchRequestConfig expands environment variables in PatchRequestConfig
func expandPatchRequestConfig(prc *PatchRequestConfig) {
	if prc == nil {
		return
	}
	prc.IncludeHeaders = expandStringMap(prc.IncludeHeaders)
}

// expandCodeModeConfig expands environment variables in CodeModeConfig
func expandCodeModeConfig(cmc *CodeModeConfig) {
	if cmc == nil {
		return
	}
	cmc.ExcludedTools = expandStringSlice(cmc.ExcludedTools)
}

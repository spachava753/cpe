/*
Package config defines CPE's YAML configuration schema and runtime resolution pipeline.

It separates two layers:
  - RawConfig: file-level representation loaded from YAML.
  - Config: effective runtime settings for one selected model profile.

The config file intentionally has no global defaults layer. Each models entry is
a complete runtime profile containing model provider settings plus optional MCP
servers, generation parameters, system prompt path, timeout, codeMode, and
compaction settings. Users can reduce YAML duplication with anchors and aliases;
CPE resolves only the selected profile and does not merge profile fields with any
global config block.

Resolution precedence is limited to runtime selection and explicit CLI overrides:
  - model selection: --model or CPE_MODEL is required;
  - generation options: CLI/runtime opts override the selected profile fields;
  - timeout: CLI/runtime timeout overrides the selected profile timeout, then
    falls back to the built-in default.

The package also validates custom invariants (model references, auth method
constraints, MCP server transport constraints, codeMode path normalization, and
compaction schema/template/restart-limit validity) and resolves filesystem-relative
codeMode.localModulePaths.

MCP server connection settings are represented via the dependency-neutral
`internal/mcpconfig` schema package so config loading does not depend on MCP
runtime implementation packages. `type: builtin` selects a CPE-provided server
while preserving the same per-profile MCP filtering and duplicate-name rules.
*/
package config

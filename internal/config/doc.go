/*
Package config defines CPE's unified configuration schema and runtime
resolution pipeline.

It separates two layers:
  - RawConfig: file-level representation loaded from YAML/JSON.
  - Config: effective runtime settings for one selected model.

Resolution precedence:
  - model selection: CLI --model -> defaults.model;
  - generation options: CLI overrides -> model generationDefaults ->
    defaults.generationParams;
  - system prompt path: model override -> defaults.systemPromptPath;
  - timeout: CLI --timeout -> defaults.timeout -> built-in default;
  - tool-oriented features such as codeMode and compaction use whole-object
    model overrides instead of field-level merging.

The package also validates custom invariants (model references, auth method
constraints, subagent/output schema checks, codeMode path normalization, and
compaction schema/template/restart-limit validity) and resolves
filesystem-relative paths such as conversation storage and
codeMode.localModulePaths.

MCP server connection settings are represented via the dependency-neutral
`internal/mcpconfig` schema package so config loading does not depend on MCP
runtime implementation packages.
*/
package config

/*
Package config defines CPE's YAML configuration schema and runtime resolution pipeline.

It separates two layers:
  - RawConfig: file-level representation loaded from YAML.
  - Config: effective runtime settings for one selected model profile.

The config file is a list of self-contained model profiles. Each models entry
contains model provider settings plus optional MCP servers, generation
parameters, valid thinking values, system prompt path, timeout, codeMode, and
compaction settings. Users can reduce YAML duplication with anchors and aliases;
CPE resolves only the selected profile and does not infer shared fields from
other profiles.

Resolution precedence is limited to runtime selection and explicit overrides:
  - model selection: ACP session state, --model, or CPE_MODEL supplies a model
    profile ref depending on caller;
  - generation options: runtime opts override the selected profile fields;
  - timeout: runtime timeout override, then selected profile timeout, then the
    built-in default.

The package also validates custom invariants (model references, auth method
constraints, MCP server transport constraints, codeMode settings, and compaction
schema/template/restart-limit validity), resolves filesystem-relative
systemPromptPath values, renders system prompt templates for resolved profiles,
and carries per-profile runtime flags such as bundled edit-tool opt-out.

MCP server connection settings are represented via the dependency-neutral
`internal/mcpconfig` schema package so config loading does not depend on MCP
runtime implementation packages.
*/
package config

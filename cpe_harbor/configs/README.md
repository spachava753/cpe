# CPE Harbor Configs

This directory contains CPE configuration and system prompt artifacts used by the Harbor CPE agent.

The Harbor agent is intentionally generic: it installs CPE, installs a config artifact to `$HOME/.config/cpe/cpe.yaml`, installs a system prompt artifact to `$HOME/.config/cpe/agent_instructions.md`, and then runs CPE with explicit `--config`, `--model`, and `--thinking-level` values. Artifacts may be HTTP(S) URLs or local `file://` URLs; local files are embedded into the setup command so mandatory smoke tests validate the current worktree. Add new experiment variants here by creating a new subdirectory with a `cpe.yaml` and `agent_instructions.md` pair. Always pass the CPE profile ref and thinking value explicitly with `--ak model_ref=<ref>` and `--ak thinking_level=<value>`; Harbor's `-m` model name is not used for inference. The bundled GLM configs target Z.ai's OpenAI-compatible endpoint using CPE's implemented `openai` provider type and expose `high` as their Harbor smoke thinking value.

Use local `file://` URLs while iterating on active experiments. Use commit-SHA raw GitHub URLs for final reproducible benchmark runs.

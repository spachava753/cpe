# DeepSWE CPE Config

This directory contains the CPE config and prompt variants for running CPE through Pier on DeepSWE tasks.

All model profiles in `cpe.yaml` use the same runtime prompt path:

```yaml
systemPromptPath: $HOME/.config/cpe/agent_instructions.md
```

Choose the effective prompt by changing the Pier `system_prompt_url` agent kwarg:

- Use `gpt_instructions.md` for `gpt` and `gptmini`.
- Use `agent_instructions.md` for `kimi`, `glm`, `opus`, and `fable`.

## GPT 5.5 OAuth Run

Run from the Pier checkout:

```shell
cd /Users/shashankpachava/dev/pier

CPE=/Users/shashankpachava/dev/cpe
AUTH='file:///Users/shashankpachava/Library/Application%20Support/cpe/auth.json'

PYTHONPATH="$CPE" uv run pier run \
  -p ../deep-swe/tasks/abs-module-cache-flags \
  --agent-import-path cpe_harbor.pier:CPE \
  --ak "config_url=file://$CPE/cpe_harbor/configs/deep-swe/cpe.yaml" \
  --ak "system_prompt_url=file://$CPE/cpe_harbor/configs/deep-swe/gpt_instructions.md" \
  --ak "auth_url=$AUTH" \
  --ak model_ref=gpt \
  --ak thinking_level=high \
  --ak version=v0.45.3 \
  -m openai/gpt-5.5 \
  -e modal \
  -n 1 \
  --yes \
  --agent-timeout-multiplier 4 \
  --agent-setup-timeout-multiplier 3
```

To run GPT mini, change only these values:

```shell
--ak model_ref=gptmini
-m openai/gpt-5.4-mini
```

## Non-GPT Run

Use the general agent instructions and pass the provider API key through Pier with `--ae` or `--env-file`.

Example for GLM:

```shell
cd /Users/shashankpachava/dev/pier

CPE=/Users/shashankpachava/dev/cpe

PYTHONPATH="$CPE" uv run pier run \
  -p ../deep-swe/tasks/abs-module-cache-flags \
  --agent-import-path cpe_harbor.pier:CPE \
  --ak "config_url=file://$CPE/cpe_harbor/configs/deep-swe/cpe.yaml" \
  --ak "system_prompt_url=file://$CPE/cpe_harbor/configs/deep-swe/agent_instructions.md" \
  --ak model_ref=glm \
  --ak version=v0.45.3 \
  -m zai/glm-5.2 \
  -e modal \
  -n 1 \
  --yes \
  --ae Z_API_KEY="$Z_API_KEY" \
  --agent-timeout-multiplier 4 \
  --agent-setup-timeout-multiplier 3
```

Other profile substitutions:

| CPE profile | Pier `-m` metadata | Secret/env | Thinking flag |
| --- | --- | --- | --- |
| `kimi` | `moonshot/kimi-k2.7-code` | `--ae MOONSHOT_API_KEY="$MOONSHOT_API_KEY"` | Omit `thinking_level` |
| `glm` | `zai/glm-5.2` | `--ae Z_API_KEY="$Z_API_KEY"` | Omit `thinking_level` |
| `opus` | `anthropic/claude-opus-4-8` | `--ae ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY"` | Optional, for example `--ak thinking_level=high` |
| `fable` | `anthropic/claude-fable-5` | `--ae ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY"` | Optional, for example `--ak thinking_level=high` |

## Running More Tasks

The examples above run one task:

```shell
-p ../deep-swe/tasks/abs-module-cache-flags
```

To run a different single task, replace that path with another directory under `../deep-swe/tasks/`.

Use `-n 1` for smoke tests. Increase `-n` only after the single-task smoke passes.

Prefer `-e modal` for DeepSWE because the tasks request more storage than the local Daytona quota supports. Use Daytona only for local adapter smoke tests with an explicit resource override.

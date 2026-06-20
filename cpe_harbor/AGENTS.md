# Overview

This folder holds the files for executing CPE within Harbor and Pier. The Harbor entrypoint is `cpe_harbor:CPE`; the Pier-native entrypoint is `cpe_harbor.pier:CPE`. See https://www.harborframework.com/docs and https://github.com/datacurve-ai/pier.

## Execution

I already have the `harbor` cli installed using `uv`. Pier is checked out at `../pier` and can be run with `uv run` from that repository.

## Verification

When making changes to `./cpe.py`, update or add tests in `./test_cpe.py`. When making changes to `./pier.py` or shared CPE adapter behavior in `./_shared.py`, update or add tests in `./test_pier.py`. The CPE agents require `config_url`, `system_prompt_url`, and `model_ref` kwargs; pass `thinking_level` only for profiles that declare `thinkingValues`. DeepSWE profiles all point at `$HOME/.config/cpe/agent_instructions.md`; choose the effective prompt by changing `system_prompt_url`, for example `gpt_instructions.md` for GPT profiles and `agent_instructions.md` for other profiles. Use local `file://` URLs for mandatory smoke tests while iterating so Harbor/Pier validate the current worktree. Use commit-SHA raw GitHub URLs for final reproducible benchmark runs.

After changing the Harbor agent or bundled configs, always run at least one single-task Harbor smoke test from this file and record the job result. After changing the Pier agent or bundled configs, always run at least one single-task Pier smoke test against DeepSWE and record the job result. Unit tests alone are not sufficient because they do not exercise benchmark setup, CPE installation, config download/embedding, Go module cache prewarming, network allowlists, or direct prompt execution inside the benchmark environment.

Always pass the CPE model profile ref explicitly with `--ak model_ref=<ref>`. Pass `--ak thinking_level=<value>` only when the selected CPE profile declares `thinkingValues`; omit it for profiles such as DeepSWE `kimi` and `glm` that do not expose a CPE thinking selector. Harbor/Pier `-m` model names are benchmark metadata and must not be used to infer the CPE profile. The Pier network allowlist is derived from install/cache requirements plus the selected CPE config profile, including its `base_url` and remote MCP server URLs. Pin the CPE release with `--ak version=<tag>` for recorded smoke tests and benchmark runs; use `latest` only when intentionally testing the moving latest release and agent-install caching is not a concern.

For the bundled GLM configs, make `Z_API_KEY` available in the Harbor/Pier process environment, for example by exporting it in the shell or by using the framework's `--env-file` option. For ChatGPT/Codex OAuth configs, pass the host CPE auth store as `--ak auth_url=<file-url>`; on this macOS machine that is `file:///Users/shashankpachava/Library/Application%20Support/cpe/auth.json`. The adapter injects it only into the runtime environment and does not bake OAuth credentials into install steps or images.

## Command Flag Reference

These smoke commands combine framework CLI flags with CPE-specific agent kwargs. Harbor and Pier own the benchmark lifecycle; the CPE adapter owns CPE installation, config installation, prompt installation, auth injection, and direct prompt execution.

| Argument | Used by | Purpose |
| --- | --- | --- |
| `harbor run` | Harbor | Starts a Harbor benchmark job. |
| `uv run pier run` | Pier | Starts a Pier benchmark job inside the Pier uv environment. Run it from `../pier` so Pier's project environment and relative DeepSWE paths resolve correctly. |
| `PYTHONPATH=$PWD/../cpe` | Python import path | Makes this checkout's `cpe_harbor` package importable when running Pier from `../pier`. Without it, Pier may import only installed packages. |
| `-d bigcode/humanevalfix` | Harbor | Selects the Harbor dataset. The HumanEvalFix smoke commands use this with `-i` to select one dataset item. |
| `-i bigcode/python-48` | Harbor | Includes only this dataset task for a one-task smoke. For full Harbor runs, omit this filter. |
| `-p ../deep-swe/tasks/abs-module-cache-flags` | Pier | Selects a DeepSWE task directory by local path. Replace this path to smoke a different DeepSWE task. |
| `--agent-import-path cpe_harbor:CPE` | Harbor | Loads the legacy Harbor adapter class from this repository. |
| `--agent-import-path cpe_harbor.pier:CPE` | Pier | Loads the Pier-native adapter class from this repository. |
| `--ak key=value` | Harbor/Pier agent kwargs | Passes a keyword argument into the CPE adapter constructor. These configure the adapter, not the benchmark framework model routing. |
| `--ak config_url=...` | CPE adapter | Source URL for the CPE YAML config. The adapter installs it as `$HOME/.config/cpe/cpe.yaml` inside the sandbox. Use `file://` URLs during local iteration so the sandbox receives current worktree files. |
| `--ak system_prompt_url=...` | CPE adapter | Source URL for the system prompt artifact. The adapter installs it as `$HOME/.config/cpe/agent_instructions.md` inside the sandbox. DeepSWE profiles all point to that runtime path, so choose GPT vs non-GPT instructions by changing this URL. |
| `--ak auth_url=...` | CPE adapter | Local `file://` URL for the host CPE OAuth credential store. The adapter base64-encodes it into runtime environment only, then writes `$HOME/.config/cpe/auth.json` inside the sandbox immediately before CPE starts. Do not use this for API-key profiles. |
| `--ak model_ref=<ref>` | CPE adapter and CPE CLI | Selects the CPE model profile ref from `cpe.yaml`, such as `gpt`, `glm`, `kimi`, `opus`, or `fable`. This is the value that controls which provider/base URL/config CPE uses. |
| `--ak thinking_level=<value>` | CPE adapter and CPE CLI | Passes CPE `--thinking-level`. Use it only when the selected profile declares `thinkingValues`; omit it for profiles without a CPE thinking selector, such as the current DeepSWE `kimi` and `glm` profiles. |
| `--ak version=<tag>` | CPE adapter | Pins the CPE release installed in the benchmark sandbox, for example `v0.45.3`. Prefer a fixed tag for recorded smokes and benchmark runs. |
| `-m <provider/model>` | Harbor/Pier metadata | Records benchmark model metadata and may affect framework reporting. The CPE adapter does not infer CPE provider selection from `-m`; `--ak model_ref` is authoritative. Keep `-m` aligned for readable results. |
| `-e daytona` | Harbor/Pier environment | Runs the benchmark task in Daytona. Useful for Harbor HumanEvalFix smokes. |
| `-e modal` | Pier environment | Runs the benchmark task in Modal. Prefer this for DeepSWE because tasks request more resources than the local Daytona quota supports. |
| `-n 1` | Harbor/Pier scheduler | Runs one trial/concurrent worker. Increase for larger experiment runs after the smoke passes. |
| `--yes` | Harbor/Pier CLI | Skips interactive confirmation prompts so commands can run unattended. |
| `--agent-timeout-multiplier <n>` | Harbor/Pier timeouts | Multiplies the task's agent execution timeout. Use larger values for real runs; short values are only for bounded adapter smoke tests. |
| `--agent-setup-timeout-multiplier <n>` | Harbor/Pier timeouts | Multiplies the setup timeout for installing CPE, Go, configs, and cache prewarming. Keep this elevated when source fallback or cold environment setup is possible. |
| `--ae NAME=value` | Harbor/Pier agent env | Passes environment variables into the agent runtime process. Use this for API-key profiles such as `Z_API_KEY`, `MOONSHOT_API_KEY`, or `ANTHROPIC_API_KEY`. Avoid printing secret values in logs. |
| `--env-file PATH` | Harbor/Pier env loading | Loads environment variables from a dotenv file instead of spelling them out on the command line. Prefer this when passing multiple secrets. |
| `--override-storage-mb 10000` | Daytona environment | Overrides requested storage for local Daytona-only smokes. Do not use this for leaderboard or reproducibility runs because it changes task resources. |
| `-t <task-ref>` | Harbor standalone task mode | Runs a standalone Harbor package task. Do not use `-t bigcode/python-48` with the HumanEvalFix dataset smoke; use `-i` with `-d` instead, or pin the full digest if using standalone task mode. |

Run Harbor unit tests:
```shell
/Users/shashankpachava/.local/share/uv/tools/harbor/bin/python -m unittest cpe_harbor.test_cpe
```

Run Pier unit tests:
```shell
cd ../pier
PYTHONPATH=$PWD/../cpe uv run python -m unittest cpe_harbor.test_pier
```

Smoke test Harbor on a single HumanEvalFix task with the text-editing config:
```shell
harbor run -d bigcode/humanevalfix -i bigcode/python-48 --agent-import-path cpe_harbor:CPE --ak config_url=file://$PWD/cpe_harbor/configs/text_edit/cpe.yaml --ak system_prompt_url=file://$PWD/cpe_harbor/configs/text_edit/agent_instructions.md --ak model_ref=glm --ak thinking_level=high --ak version=v0.45.3 -m zai/glm-5.1 -e daytona -n 1 --yes --agent-timeout-multiplier 4 --agent-setup-timeout-multiplier 3
```

Smoke test Harbor on a single HumanEvalFix task with the execute-Go-code editing config:
```shell
harbor run -d bigcode/humanevalfix -i bigcode/python-48 --agent-import-path cpe_harbor:CPE --ak config_url=file://$PWD/cpe_harbor/configs/execute_go_code_edits/cpe.yaml --ak system_prompt_url=file://$PWD/cpe_harbor/configs/execute_go_code_edits/agent_instructions.md --ak model_ref=glm --ak thinking_level=high --ak version=v0.45.3 -m zai/glm-5.1 -e daytona -n 1 --yes --agent-timeout-multiplier 4 --agent-setup-timeout-multiplier 3
```

Smoke test Pier on a single DeepSWE task with the DeepSWE ChatGPT/Codex OAuth config. Prefer Modal for DeepSWE because it can satisfy the task resource requests without local Daytona quota overrides:
```shell
cd ../pier
PYTHONPATH=$PWD/../cpe uv run pier run -p ../deep-swe/tasks/abs-module-cache-flags --agent-import-path cpe_harbor.pier:CPE --ak config_url=file://$PWD/../cpe/cpe_harbor/configs/deep-swe/cpe.yaml --ak system_prompt_url=file://$PWD/../cpe/cpe_harbor/configs/deep-swe/gpt_instructions.md --ak auth_url=file:///Users/shashankpachava/Library/Application%20Support/cpe/auth.json --ak model_ref=gpt --ak thinking_level=high --ak version=v0.45.3 -m openai/gpt-5.5 -e modal -n 1 --yes --agent-timeout-multiplier 4 --agent-setup-timeout-multiplier 3
```

Smoke test Pier on a single DeepSWE task with the GLM text-editing config:
```shell
cd ../pier
PYTHONPATH=$PWD/../cpe uv run pier run -p ../deep-swe/tasks/abs-module-cache-flags --agent-import-path cpe_harbor.pier:CPE --ak config_url=file://$PWD/../cpe/cpe_harbor/configs/text_edit/cpe.yaml --ak system_prompt_url=file://$PWD/../cpe/cpe_harbor/configs/text_edit/agent_instructions.md --ak model_ref=glm --ak thinking_level=high --ak version=v0.45.3 -m zai/glm-5.1 -e modal -n 1 --yes --agent-timeout-multiplier 4 --agent-setup-timeout-multiplier 3
```

Smoke test Pier on a single DeepSWE task with the execute-Go-code editing config:
```shell
cd ../pier
PYTHONPATH=$PWD/../cpe uv run pier run -p ../deep-swe/tasks/abs-module-cache-flags --agent-import-path cpe_harbor.pier:CPE --ak config_url=file://$PWD/../cpe/cpe_harbor/configs/execute_go_code_edits/cpe.yaml --ak system_prompt_url=file://$PWD/../cpe/cpe_harbor/configs/execute_go_code_edits/agent_instructions.md --ak model_ref=glm --ak thinking_level=high --ak version=v0.45.3 -m zai/glm-5.1 -e modal -n 1 --yes --agent-timeout-multiplier 4 --agent-setup-timeout-multiplier 3
```

If Modal is unavailable and Daytona must be used for a local adapter smoke test, add `-e daytona --override-storage-mb 10000`. DeepSWE tasks request 20GB, while this local Daytona account currently allows 10GB per sandbox; the storage override is smoke-only and should not be used for leaderboard or reproducibility runs.

For full Harbor experiment runs, omit `-i bigcode/python-48` and use `-n 5` to run five concurrent trials. Allow up to an hour for the full dataset.

Use `-i bigcode/python-48` as the Harbor dataset task filter. Do not combine `-d bigcode/humanevalfix` with `-t bigcode/python-48` for this smoke test: Harbor treats `-t` as a standalone package task reference and resolves `bigcode/python-48@latest`, which can fail quickly with Supabase/Postgres `canceling statement due to statement timeout` before the agent starts. If running the task without the dataset, pin the content digest explicitly, for example `-t bigcode/python-48@sha256:90ef6c5ba74c91cb66f874e2be0ae362c67d9a2f086eb1d2a2f0010a5b00cb4e`.

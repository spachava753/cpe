# Overview

This folder holds the files for executing CPE within Harbor and Pier. The Harbor entrypoint is `cpe_harbor:CPE`; the Pier-native entrypoint is `cpe_harbor.pier:CPE`. See https://www.harborframework.com/docs and https://github.com/datacurve-ai/pier.

## Execution

I already have the `harbor` cli installed using `uv`. Pier is checked out at `../pier` and can be run with `uv run` from that repository.

## Verification

When making changes to `./cpe.py`, update or add tests in `./test_cpe.py`. When making changes to `./pier.py` or shared CPE adapter behavior in `./_shared.py`, update or add tests in `./test_pier.py`. The CPE agents require `config_url`, `system_prompt_url`, `model_ref`, and `thinking_level` kwargs. Use local `file://` URLs for mandatory smoke tests while iterating so Harbor/Pier validate the current worktree. Use commit-SHA raw GitHub URLs for final reproducible benchmark runs.

After changing the Harbor agent or bundled configs, always run at least one single-task Harbor smoke test from this file and record the job result. After changing the Pier agent or bundled configs, always run at least one single-task Pier smoke test against DeepSWE and record the job result. Unit tests alone are not sufficient because they do not exercise benchmark setup, CPE installation, config download/embedding, network allowlists, or direct prompt execution inside the benchmark environment.

Always pass the CPE model profile ref and thinking level explicitly with `--ak model_ref=<ref>` and `--ak thinking_level=<value>`. Harbor/Pier `-m` model names are benchmark metadata and must not be used to infer the CPE profile. Pin the CPE release with `--ak version=<tag>` for recorded smoke tests and benchmark runs; use `latest` only when intentionally testing the moving latest release and agent-install caching is not a concern.

For the bundled GLM configs, make `Z_API_KEY` available in the Harbor/Pier process environment, for example by exporting it in the shell or by using the framework's `--env-file` option.

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

Smoke test Pier on a single DeepSWE task with the text-editing config. Prefer Modal for DeepSWE because it can satisfy the task resource requests without local Daytona quota overrides:
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

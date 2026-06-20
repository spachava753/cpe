# Overview

This folder holds the files for executing CPE within the harbor framework. See https://www.harborframework.com/docs.

## Execution

I already have the `harbor` cli installed using `uv`.

## Verification

When making changes to ./cpe.py, update or add tests in ./test_cpe.py. The CPE Harbor agent requires `config_url`, `system_prompt_url`, `model_ref`, and `thinking_level` kwargs. Use local `file://` URLs for mandatory smoke tests while iterating so Harbor validates the current worktree. Use commit-SHA raw GitHub URLs for final reproducible benchmark runs.

After changing the Harbor agent or bundled configs, always run at least one single-task Harbor smoke test from this file and record the job result. Unit tests alone are not sufficient because they do not exercise Harbor setup, CPE installation, config download, or direct prompt execution inside the benchmark environment.

Always pass the CPE model profile ref and thinking level explicitly with `--ak model_ref=<ref>` and `--ak thinking_level=<value>`. Harbor's `-m` model name is benchmark metadata and must not be used to infer the CPE profile.

For the bundled GLM configs, make `Z_API_KEY` available in the Harbor process environment, for example by exporting it in the shell or by using Harbor's `--env-file` option.

Smoke test a single task with the text-editing config:
```shell
harbor run -d bigcode/humanevalfix -i bigcode/python-48 --agent-import-path cpe_harbor:CPE --ak config_url=file://$PWD/cpe_harbor/configs/text_edit/cpe.yaml --ak system_prompt_url=file://$PWD/cpe_harbor/configs/text_edit/agent_instructions.md --ak model_ref=glm --ak thinking_level=high -m zai/glm-5.1 -e daytona -n 1 --yes --agent-timeout-multiplier 4 --agent-setup-timeout-multiplier 3
```

Smoke test a single task with the execute-Go-code editing config:
```shell
harbor run -d bigcode/humanevalfix -i bigcode/python-48 --agent-import-path cpe_harbor:CPE --ak config_url=file://$PWD/cpe_harbor/configs/execute_go_code_edits/cpe.yaml --ak system_prompt_url=file://$PWD/cpe_harbor/configs/execute_go_code_edits/agent_instructions.md --ak model_ref=glm --ak thinking_level=high -m zai/glm-5.1 -e daytona -n 1 --yes --agent-timeout-multiplier 4 --agent-setup-timeout-multiplier 3
```

For full experiment runs, omit `-i bigcode/python-48` and use `-n 5` to run five concurrent trials. Allow up to an hour for the full dataset.

Use `-i bigcode/python-48` as the dataset task filter. Do not combine `-d bigcode/humanevalfix` with `-t bigcode/python-48` for this smoke test: Harbor treats `-t` as a standalone package task reference and resolves `bigcode/python-48@latest`, which can fail quickly with Supabase/Postgres `canceling statement due to statement timeout` before the agent starts. If running the task without the dataset, pin the content digest explicitly, for example `-t bigcode/python-48@sha256:90ef6c5ba74c91cb66f874e2be0ae362c67d9a2f086eb1d2a2f0010a5b00cb4e`.

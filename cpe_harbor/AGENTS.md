# Overview

This folder holds the files for executing CPE within the harbor framework. See https://www.harborframework.com/docs.

## Execution

I already have the `harbor` cli installed using `uv`.

## Verification

When making changes to ./cpe.py, update or add tests in ./test_cpe.py. The CPE Harbor agent requires both `config_url` and `system_prompt_url` kwargs; use raw GitHub `main` URLs while iterating and commit-SHA URLs for final reproducible benchmark runs.

Smoke test a single task with the text-editing config:
```shell
harbor run -d bigcode/humanevalfix -i bigcode/python-48 --agent-import-path cpe_harbor:CPE --ak config_url=https://raw.githubusercontent.com/spachava753/cpe/main/cpe_harbor/configs/text_edit/cpe.yaml --ak system_prompt_url=https://raw.githubusercontent.com/spachava753/cpe/main/cpe_harbor/configs/text_edit/agent_instructions.md -m zai/glm-5.1 -e daytona -n 3 --yes --agent-timeout-multiplier 4 --ae Z_API_KEY=$Z_API_KEY
```

Smoke test a single task with the execute-Go-code editing config:
```shell
harbor run -d bigcode/humanevalfix -i bigcode/python-48 --agent-import-path cpe_harbor:CPE --ak config_url=https://raw.githubusercontent.com/spachava753/cpe/main/cpe_harbor/configs/execute_go_code_edits/cpe.yaml --ak system_prompt_url=https://raw.githubusercontent.com/spachava753/cpe/main/cpe_harbor/configs/execute_go_code_edits/agent_instructions.md -m zai/glm-5.1 -e daytona -n 3 --yes --agent-timeout-multiplier 4 --ae Z_API_KEY=$Z_API_KEY
```

For full experiment runs, omit `-i bigcode/python-48` and use `-n 5` to run five concurrent trials. Allow up to an hour for the full dataset.

Use `-i bigcode/python-48` as the dataset task filter. Do not combine `-d bigcode/humanevalfix` with `-t bigcode/python-48` for this smoke test: Harbor treats `-t` as a standalone package task reference and resolves `bigcode/python-48@latest`, which can fail quickly with Supabase/Postgres `canceling statement due to statement timeout` before the agent starts. If running the task without the dataset, pin the content digest explicitly, for example `-t bigcode/python-48@sha256:90ef6c5ba74c91cb66f874e2be0ae362c67d9a2f086eb1d2a2f0010a5b00cb4e`.

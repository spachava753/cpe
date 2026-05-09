# Overview

This folder holds the files for executing CPE within the harbor framework. See https://www.harborframework.com/docs.

## Execution

I already have the `harbor` cli installed using `uv`.

## Verification

When making changes to ./cpe.py, besides updating and adding tests in ./test_cpe.py, it may actually be helpful to actually run a verification test which spins up an agent to complete a very simple task. Here is one such command:
```shell
harbor run -d bigcode/humanevalfix -t bigcode/python-48 --agent-import-path cpe_harbor:CPE -m zai/glm-5.1 -e daytona -n 3 --yes --agent-timeout-multiplier 4 --ae Z_API_KEY=$Z_API_KEY
```
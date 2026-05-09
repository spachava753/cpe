# CPE Harbor Configs

This directory contains CPE configuration and system prompt artifacts used by the Harbor CPE agent.

The Harbor agent is intentionally generic: it installs CPE, downloads a config URL to `$HOME/.config/cpe/cpe.yaml`, downloads a system prompt URL to `$HOME/.config/cpe/agent_instructions.md`, and then runs CPE. Add new experiment variants here by creating a new subdirectory with a `cpe.yaml` and `agent_instructions.md` pair.

Use raw GitHub `main` URLs while iterating on active experiments. Use commit-SHA raw URLs for final reproducible benchmark runs.

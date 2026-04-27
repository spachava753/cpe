import os
import shlex
from harbor.agents.installed.base import BaseInstalledAgent, with_prompt_template
from harbor.environments.base import BaseEnvironment
from harbor.models.agent.context import AgentContext


class CPE(BaseInstalledAgent):
    """
    CPE (Chat-based Programming Editor) is a CLI that connects local developer
    workflows to multiple AI model providers. It analyzes, edits, and creates
    code via natural-language prompts, with optional MCP tool integration.
    """

    _GO_VERSION = "1.25.5"
    _OUTPUT_FILENAME = "cpe.txt"

    @staticmethod
    def name() -> str:
        return "cpe"

    def version(self) -> str | None:
        return self._version or "latest"

    def get_version_command(self) -> str | None:
        return 'export PATH="$HOME/go/bin:/usr/local/go/bin:$PATH"; cpe --version'

    def parse_version(self, stdout: str) -> str:
        text = stdout.strip()
        for line in text.splitlines():
            line = line.strip()
            if line:
                return line.removeprefix("cpe version").strip()
        return text

    async def install(self, environment: BaseEnvironment) -> None:
        await self.exec_as_root(
            environment,
            command=(
                "set -euo pipefail; "
                "apt-get update && "
                "apt-get install -y ca-certificates curl git gzip tar && "
                "arch=\"$(uname -m)\"; "
                "case \"$arch\" in "
                "x86_64|amd64) go_arch=amd64 ;; "
                "aarch64|arm64) go_arch=arm64 ;; "
                "*) echo \"unsupported architecture: $arch\" >&2; exit 1 ;; "
                "esac; "
                f"curl -fsSL https://go.dev/dl/go{self._GO_VERSION}.linux-${{go_arch}}.tar.gz "
                "-o /tmp/go.tgz && "
                "rm -rf /usr/local/go && "
                "tar -C /usr/local -xzf /tmp/go.tgz && "
                "ln -sf /usr/local/go/bin/go /usr/local/bin/go && "
                "ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt"
            ),
            env={"DEBIAN_FRONTEND": "noninteractive"},
        )

        module_spec = "github.com/spachava753/cpe@latest"
        if self._version and self._version != "latest":
            module_spec = f"github.com/spachava753/cpe@{self._version}"

        await self.exec_as_agent(
            environment,
            command=(
                "set -euo pipefail; "
                'export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"; '
                f"go install {shlex.quote(module_spec)} && "
                "cpe --version"
            ),
        )

        await self.exec_as_agent(
            environment,
            command=(
                "set -euo pipefail; "
                "mkdir -p \"$HOME/.config/cpe\" && "
                "cat > \"$HOME/.config/cpe/cpe.yaml\" <<'CPE_CONFIG'\n"
                "version: \"1.0\"\n"
                "models:\n"
                "  - ref: glm\n"
                "    display_name: \"GLM 5.1\"\n"
                "    id: glm-5.1\n"
                "    type: zai\n"
                "    base_url: https://api.z.ai/api/coding/paas/v4\n"
                "    api_key_env: Z_API_KEY\n"
                "    context_window: 202752\n"
                "    max_output: 131072\n"
                "    input_cost_per_million: 1\n"
                "    output_cost_per_million: 3.2\n"
                "    systemPromptPath: \"$HOME/.config/cpe/agent_instructions.md\"\n"
                "    timeout: 1h\n"
                "    generationParams:\n"
                "      temperature: 1\n"
                "    mcpServers:\n"
                "      editor:\n"
                "        type: builtin\n"
                "        enabledTools:\n"
                "          - text_edit\n"
                "    codeMode:\n"
                "      enabled: true\n"
                "      largeOutputCharLimit: 150000\n"
                "      maxTimeout: 3600\n"
                "CPE_CONFIG\n"
                "curl -fsSL "
                "https://raw.githubusercontent.com/spachava753/cpe/main/examples/agent_instructions.md "
                "-o \"$HOME/.config/cpe/agent_instructions.md\""
            ),
        )

    def populate_context_post_run(self, context: AgentContext) -> None:
        # CPE does not currently produce a structured trajectory file.
        pass

    def _cpe_model_ref(self) -> str:
        if not self.model_name:
            return "glm"

        if "/" not in self.model_name:
            return self.model_name

        provider, model = self.model_name.split("/", 1)
        if provider != "zai":
            raise ValueError(
                "The bundled CPE Harbor config only defines the Z.ai GLM profile; "
                f"unsupported provider: {provider}"
            )

        if model in {"glm", "glm-5.1"}:
            return "glm"
        return model

    def _run_env(self) -> dict[str, str]:
        z_api_key = os.environ.get("Z_API_KEY")
        if not z_api_key:
            raise ValueError("Z_API_KEY must be set to run CPE with the Z.ai profile")
        return {"Z_API_KEY": z_api_key}

    @with_prompt_template
    async def run(
        self,
        instruction: str,
        environment: BaseEnvironment,
        context: AgentContext,
    ) -> None:
        escaped_instruction = shlex.quote(instruction)
        model_ref = shlex.quote(self._cpe_model_ref())

        await self.exec_as_agent(
            environment,
            command=(
                'export PATH="$HOME/go/bin:/usr/local/go/bin:$PATH"; '
                f"cpe -n -G --skip-stdin -m {model_ref} {escaped_instruction} "
                f"2>&1 | stdbuf -oL tee /logs/agent/{self._OUTPUT_FILENAME}"
            ),
            env=self._run_env(),
        )

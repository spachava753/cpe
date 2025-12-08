import os
import shlex
from pathlib import Path

from harbor.agents.installed.base import BaseInstalledAgent, ExecInput
from harbor.models.agent.context import AgentContext


class CPE(BaseInstalledAgent):
    """
    CPE (Chat-based Programming Editor) is a CLI that connects local developer
    workflows to multiple AI model providers. It analyzes, edits, and creates
    code via natural-language prompts, with optional MCP tool integration.
    """

    @staticmethod
    def name() -> str:
        return "cpe"

    def version(self) -> str | None:
        return self._version or "latest"

    @property
    def _install_agent_template_path(self) -> Path:
        return Path(__file__).parent / "install-cpe.sh.j2"

    def populate_context_post_run(self, context: AgentContext) -> None:
        # CPE does not currently produce a structured trajectory file
        pass

    def create_run_agent_commands(self, instruction: str) -> list[ExecInput]:
        escaped_instruction = shlex.quote(instruction)

        # Determine API key based on model name
        env: dict[str, str] = {}

        if self.model_name:
            provider = self.model_name.split("/")[0] if "/" in self.model_name else ""
            if provider == "anthropic" or not provider:
                api_key = os.environ.get("ANTHROPIC_API_KEY", "")
                if api_key:
                    env["ANTHROPIC_API_KEY"] = api_key
            if provider == "openai":
                api_key = os.environ.get("OPENAI_API_KEY", "")
                if api_key:
                    env["OPENAI_API_KEY"] = api_key
            if provider == "gemini" or provider == "google":
                api_key = os.environ.get("GEMINI_API_KEY", "")
                if api_key:
                    env["GEMINI_API_KEY"] = api_key
        else:
            # Default to Anthropic if no model specified
            api_key = os.environ.get("ANTHROPIC_API_KEY", "")
            if api_key:
                env["ANTHROPIC_API_KEY"] = api_key

        return [
            ExecInput(
                command=(
                    'export PATH="/root/go/bin:/usr/local/go/bin:$PATH" && '
                    f"cpe -n -G --skip-stdin {escaped_instruction} "
                    "2>&1 | tee /logs/agent/cpe.txt"
                ),
                env=env,
            ),
        ]

from pathlib import Path
from typing import Any
from urllib.parse import unquote, urlparse
from urllib.request import urlopen

import yaml
from pier.agents.installed.base import BaseInstalledAgent, with_prompt_template
from pier.agents.network import allowlist_from_urls, collect_url_values
from pier.environments.base import BaseEnvironment
from pier.models.agent.context import AgentContext
from pier.models.agent.install import AgentInstallSpec, InstallStep
from pier.models.agent.network import NetworkAllowlist
from pier.models.trajectories import (
    Agent,
    FinalMetrics,
    Metrics,
    Observation,
    ObservationResult,
    Step,
    ToolCall,
    Trajectory,
)
from pier.utils.trajectory_utils import format_trajectory_json

from ._shared import CPEAgentMixin


class CPE(CPEAgentMixin, BaseInstalledAgent):
    """
    Pier-native adapter for CPE (Chat-based Programming Editor).

    Use this entrypoint with Pier as ``cpe_harbor.pier:CPE``. The historical
    Harbor entrypoint remains ``cpe_harbor:CPE``.
    """

    SUPPORTS_ATIF: bool = True

    _trajectory_agent_cls = Agent
    _trajectory_final_metrics_cls = FinalMetrics
    _trajectory_metrics_cls = Metrics
    _trajectory_observation_cls = Observation
    _trajectory_observation_result_cls = ObservationResult
    _trajectory_step_cls = Step
    _trajectory_tool_call_cls = ToolCall
    _trajectory_cls = Trajectory
    _format_trajectory_json = staticmethod(format_trajectory_json)

    _INSTALL_NETWORK_DOMAINS = (
        "dl.google.com",
        "github.com",
        "go.dev",
        "go.googlesource.com",
        "golang.org",
        "objects.githubusercontent.com",
        "proxy.golang.org",
        "raw.githubusercontent.com",
        "release-assets.githubusercontent.com",
        "sum.golang.org",
    )

    _PROVIDER_DEFAULT_DOMAINS = {
        "anthropic": ("api.anthropic.com",),
        "cerebras": ("api.cerebras.ai",),
        "gemini": (".googleapis.com",),
        "groq": ("api.groq.com",),
        "openai": ("api.openai.com",),
        "openrouter": ("openrouter.ai",),
        "responses": ("api.openai.com",),
        "zai": ("api.z.ai",),
    }

    _OAUTH_DEFAULT_DOMAINS = {
        "anthropic": ("console.anthropic.com",),
        "responses": ("auth.openai.com", "chatgpt.com"),
    }

    def __init__(
        self,
        *args: Any,
        config_url: str | None = None,
        system_prompt_url: str | None = None,
        model_ref: str | None = None,
        thinking_level: str | None = None,
        auth_url: str | None = None,
        **kwargs: Any,
    ) -> None:
        self._init_cpe_options(
            config_url=config_url,
            system_prompt_url=system_prompt_url,
            auth_url=auth_url,
            model_ref=model_ref,
            thinking_level=thinking_level,
        )
        super().__init__(*args, **kwargs)

    @staticmethod
    def name() -> str:
        return "cpe"

    def install_spec(self) -> AgentInstallSpec:
        return AgentInstallSpec(
            agent_name=self.name(),
            version=self._install_version(),
            steps=[
                InstallStep(
                    user="root",
                    env={"DEBIAN_FRONTEND": "noninteractive"},
                    run=(
                        "set -euo pipefail; "
                        "apt-get update && "
                        "apt-get install -y ca-certificates curl gzip tar"
                    ),
                ),
                InstallStep(user="root", run=self._install_go_command()),
                InstallStep(user="agent", run=self._install_cpe_command()),
                InstallStep(user="agent", run=self._warm_go_module_cache_command()),
                InstallStep(user="agent", run=self._install_config_command()),
            ],
            verification_command=self.get_version_command(),
        )

    def network_allowlist(self) -> NetworkAllowlist:
        values: list[Any] = [self._config_url, self._system_prompt_url, self._auth_url]
        values.extend(self._runtime_url_values())
        return allowlist_from_urls(
            values,
            default_domains=self._network_default_domains(),
        )

    def _network_default_domains(self) -> list[str]:
        domains = list(self._INSTALL_NETWORK_DOMAINS)
        selected_model = self._selected_model_config()
        if not selected_model:
            return domains

        model_type = str(selected_model.get("type") or "").strip().lower()
        auth_method = str(selected_model.get("auth_method") or "").strip().lower()
        base_url = selected_model.get("base_url") or selected_model.get("baseURL")

        if not base_url and not (model_type == "responses" and auth_method == "oauth"):
            domains.extend(self._PROVIDER_DEFAULT_DOMAINS.get(model_type, ()))
        if auth_method == "oauth":
            oauth_domains = self._OAUTH_DEFAULT_DOMAINS.get(model_type, ())
            if model_type == "responses" and base_url:
                oauth_domains = tuple(
                    domain for domain in oauth_domains if domain != "chatgpt.com"
                )
            domains.extend(oauth_domains)
        return domains

    def _runtime_url_values(self) -> list[str]:
        selected_model = self._selected_model_config()
        if not selected_model:
            return []
        return collect_url_values(selected_model)

    def _selected_model_config(self) -> dict[str, Any] | None:
        config_text = self._config_text(self._config_url)
        if not config_text:
            return None

        try:
            parsed = yaml.safe_load(config_text)
        except yaml.YAMLError:
            return None
        if not isinstance(parsed, dict):
            return None

        models = parsed.get("models")
        if not isinstance(models, list):
            return None
        for model in models:
            if isinstance(model, dict) and model.get("ref") == self._model_ref:
                return model
        return None

    def _config_text(self, source: str) -> str | None:
        parsed = urlparse(source)
        if parsed.scheme in {"http", "https"}:
            try:
                with urlopen(source, timeout=10) as response:
                    return response.read().decode("utf-8")
            except OSError:
                return None
        return self._local_file_url_text(source)

    @staticmethod
    def _local_file_url_text(source: str) -> str | None:
        parsed = urlparse(source)
        if parsed.scheme != "file":
            return None
        if parsed.netloc not in {"", "localhost"}:
            raise ValueError(f"unsupported file URL host: {parsed.netloc}")
        return Path(unquote(parsed.path)).read_text(encoding="utf-8")

    @with_prompt_template
    async def run(
        self,
        instruction: str,
        environment: BaseEnvironment,
        context: AgentContext,
    ) -> None:
        await self.exec_as_agent(
            environment,
            command=self._run_command(instruction),
            env=self._run_env(),
        )

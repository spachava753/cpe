from pathlib import Path
from typing import Any
from urllib.parse import unquote, urlparse

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

    _DEFAULT_NETWORK_DOMAINS = (
        "api.openai.com",
        "api.z.ai",
        "dl.google.com",
        "github.com",
        "objects.githubusercontent.com",
        "proxy.golang.org",
        "raw.githubusercontent.com",
        "release-assets.githubusercontent.com",
        "sum.golang.org",
        "go.dev",
    )

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
                InstallStep(user="agent", run=self._install_config_command()),
            ],
            verification_command=self.get_version_command(),
        )

    def network_allowlist(self) -> NetworkAllowlist:
        values: list[Any] = [self._config_url, self._system_prompt_url, self._auth_url]
        values.extend(self._config_url_values())
        return allowlist_from_urls(
            values,
            default_domains=self._DEFAULT_NETWORK_DOMAINS,
        )

    def _config_url_values(self) -> list[str]:
        config_text = self._local_file_url_text(self._config_url)
        if not config_text:
            return []

        try:
            parsed = yaml.safe_load(config_text)
        except yaml.YAMLError:
            return []
        return collect_url_values(parsed)

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

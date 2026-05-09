import json
import os
import re
import shlex
import sqlite3
from pathlib import Path
from typing import Any

from harbor.agents.installed.base import BaseInstalledAgent, with_prompt_template
from harbor.environments.base import BaseEnvironment
from harbor.models.agent.context import AgentContext
from harbor.models.trajectories import (
    Agent,
    FinalMetrics,
    Metrics,
    Observation,
    ObservationResult,
    Step,
    ToolCall,
    Trajectory,
)
from harbor.utils.trajectory_utils import format_trajectory_json


class CPE(BaseInstalledAgent):
    """
    CPE (Chat-based Programming Editor) is a CLI that connects local developer
    workflows to multiple AI model providers. It analyzes, edits, and creates
    code via natural-language prompts, with optional MCP tool integration.
    """

    SUPPORTS_ATIF: bool = True

    _INSTALLER_URL = "https://raw.githubusercontent.com/spachava753/cpe/main/install.sh"
    _OUTPUT_FILENAME = "cpe.txt"
    _CONVERSATION_DB_FILENAME = ".cpeconvo"
    _CONFIG_FILENAME = "cpe.yaml"
    _SYSTEM_PROMPT_FILENAME = "agent_instructions.md"

    def __init__(
        self,
        *args: Any,
        config_url: str | None = None,
        system_prompt_url: str | None = None,
        **kwargs: Any,
    ) -> None:
        if not config_url:
            raise ValueError("config_url agent kwarg is required")
        if not system_prompt_url:
            raise ValueError("system_prompt_url agent kwarg is required")

        self._config_url = config_url
        self._system_prompt_url = system_prompt_url
        super().__init__(*args, **kwargs)

    @staticmethod
    def name() -> str:
        return "cpe"

    def version(self) -> str | None:
        return self._version or "latest"

    def get_version_command(self) -> str | None:
        return 'export PATH="$HOME/.local/bin:$PATH"; cpe --version'

    def parse_version(self, stdout: str) -> str:
        text = stdout.strip()
        for line in text.splitlines():
            line = line.strip()
            if line:
                return line.removeprefix("cpe version").strip()
        return text

    def _install_version(self) -> str:
        version = self.version() or "latest"
        if version == "latest":
            return version
        if re.fullmatch(r"v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?", version):
            return version
        if re.fullmatch(r"\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?", version):
            return f"v{version}"
        return version

    async def install(self, environment: BaseEnvironment) -> None:
        await self.exec_as_root(
            environment,
            command=(
                "set -euo pipefail; "
                "apt-get update && "
                "apt-get install -y ca-certificates curl gzip tar"
            ),
            env={"DEBIAN_FRONTEND": "noninteractive"},
        )

        install_version = shlex.quote(self._install_version())
        await self.exec_as_agent(
            environment,
            command=(
                "set -euo pipefail; "
                "mkdir -p \"$HOME/.local/bin\" && "
                "curl -fsSL "
                f"{shlex.quote(self._INSTALLER_URL)} | "
                f"CPE_INSTALL_VERSION={install_version} "
                "CPE_INSTALL_DIR=\"$HOME/.local/bin\" sh && "
                "\"$HOME/.local/bin/cpe\" --version"
            ),
        )

        await self.exec_as_agent(
            environment,
            command=self._install_config_command(),
        )

    def _install_config_command(self) -> str:
        config_url = shlex.quote(self._config_url)
        system_prompt_url = shlex.quote(self._system_prompt_url)
        config_path = f"$HOME/.config/cpe/{self._CONFIG_FILENAME}"
        system_prompt_path = f"$HOME/.config/cpe/{self._SYSTEM_PROMPT_FILENAME}"
        return (
            "set -euo pipefail; "
            "mkdir -p \"$HOME/.config/cpe\" && "
            f"curl -fsSL {config_url} -o \"{config_path}\" && "
            f"curl -fsSL {system_prompt_url} -o \"{system_prompt_path}\""
        )

    def populate_context_post_run(self, context: AgentContext) -> None:
        db_path = self.logs_dir / self._CONVERSATION_DB_FILENAME
        if not db_path.exists():
            self.logger.debug(f"No CPE conversation database found at {db_path}")
            return

        try:
            trajectory = self._convert_conversation_db_to_trajectory(db_path)
        except Exception:
            self.logger.exception("Failed to convert CPE conversation to trajectory")
            return

        if not trajectory:
            return

        trajectory_path = self.logs_dir / "trajectory.json"
        try:
            trajectory_path.write_text(
                format_trajectory_json(trajectory.to_json_dict()), encoding="utf-8"
            )
            self.logger.debug(f"Wrote CPE trajectory to {trajectory_path}")
        except OSError as exc:
            self.logger.debug(f"Failed to write trajectory file {trajectory_path}: {exc}")

        if trajectory.final_metrics:
            if trajectory.final_metrics.total_prompt_tokens is not None:
                context.n_input_tokens = trajectory.final_metrics.total_prompt_tokens
            if trajectory.final_metrics.total_cached_tokens is not None:
                context.n_cache_tokens = trajectory.final_metrics.total_cached_tokens
            if trajectory.final_metrics.total_completion_tokens is not None:
                context.n_output_tokens = trajectory.final_metrics.total_completion_tokens
            if trajectory.final_metrics.total_cost_usd is not None:
                context.cost_usd = trajectory.final_metrics.total_cost_usd

    def _convert_conversation_db_to_trajectory(self, db_path: Path) -> Trajectory | None:
        messages = self._read_cpe_messages(db_path)
        if not messages:
            self.logger.debug("No CPE messages found for trajectory conversion")
            return None

        steps: list[Step] = []
        last_agent_step: Step | None = None
        session_id = str(messages[0]["id"])

        for message in messages:
            role = message["role"]
            timestamp = message.get("created_at")
            blocks = message["blocks"]

            if role == "tool_result":
                if last_agent_step is not None:
                    self._attach_tool_result(last_agent_step, blocks)
                else:
                    steps.append(
                        Step(
                            step_id=len(steps) + 1,
                            timestamp=timestamp,
                            source="system",
                            message=self._blocks_to_text(blocks) or "Tool result",
                        )
                    )
                continue

            if role == "assistant":
                step = self._assistant_message_to_step(
                    message=message,
                    step_id=len(steps) + 1,
                    timestamp=timestamp,
                )
                steps.append(step)
                last_agent_step = step
                continue

            source = "user" if role == "user" else "system"
            steps.append(
                Step(
                    step_id=len(steps) + 1,
                    timestamp=timestamp,
                    source=source,
                    message=self._blocks_to_text(blocks) or "(empty message)",
                )
            )
            last_agent_step = None

        if not steps:
            return None

        return Trajectory(
            schema_version="ATIF-v1.6",
            session_id=session_id,
            agent=Agent(
                name="cpe",
                version=self.version() or "unknown",
                model_name=self.model_name,
            ),
            steps=steps,
            final_metrics=self._final_metrics_from_steps(steps),
        )

    def _read_cpe_messages(self, db_path: Path) -> list[dict[str, Any]]:
        rows: list[sqlite3.Row]
        with sqlite3.connect(f"file:{db_path}?mode=ro", uri=True) as conn:
            conn.row_factory = sqlite3.Row
            message_columns = {
                row["name"]
                for row in conn.execute("PRAGMA table_info(messages)").fetchall()
            }
            select_exprs = [
                "m.id AS message_id",
                "m.role",
                "m.tool_result_error",
                "m.created_at AS message_created_at",
            ]
            for column in (
                "message_extra_fields",
                "model_ref",
                "model_id",
                "model_type",
                "model_display_name",
                "input_tokens",
                "output_tokens",
                "cache_read_tokens",
                "cache_write_tokens",
            ):
                if column in message_columns:
                    select_exprs.append(f"m.{column} AS {column}")
                else:
                    select_exprs.append(f"NULL AS {column}")
            select_exprs.extend(
                [
                    "b.id AS block_id",
                    "b.block_type",
                    "b.modality_type",
                    "b.mime_type",
                    "b.content",
                    "b.extra_fields",
                    "b.sequence_order",
                ]
            )
            rows = conn.execute(
                f"""
                SELECT
                    {", ".join(select_exprs)}
                FROM messages m
                LEFT JOIN blocks b ON b.message_id = m.id
                ORDER BY m.created_at ASC, m.rowid ASC, b.sequence_order ASC
                """
            ).fetchall()

        messages: list[dict[str, Any]] = []
        by_id: dict[str, dict[str, Any]] = {}
        for row in rows:
            message_id = row["message_id"]
            message = by_id.get(message_id)
            if message is None:
                message = {
                    "id": message_id,
                    "role": row["role"],
                    "created_at": self._normalize_timestamp(row["message_created_at"]),
                    "tool_result_error": bool(row["tool_result_error"]),
                    "extra_fields": self._decode_extra_fields(row["message_extra_fields"]),
                    "model_ref": row["model_ref"],
                    "model_id": row["model_id"],
                    "model_type": row["model_type"],
                    "model_display_name": row["model_display_name"],
                    "input_tokens": self._int_or_none(row["input_tokens"]),
                    "output_tokens": self._int_or_none(row["output_tokens"]),
                    "cache_read_tokens": self._int_or_none(row["cache_read_tokens"]),
                    "cache_write_tokens": self._int_or_none(row["cache_write_tokens"]),
                    "blocks": [],
                }
                by_id[message_id] = message
                messages.append(message)

            if row["block_type"] is None:
                continue

            message["blocks"].append(
                {
                    "id": row["block_id"] or "",
                    "block_type": row["block_type"],
                    "modality_type": row["modality_type"],
                    "mime_type": row["mime_type"],
                    "content": row["content"] or "",
                    "extra_fields": self._decode_extra_fields(row["extra_fields"]),
                    "sequence_order": row["sequence_order"],
                }
            )

        return messages

    @staticmethod
    def _normalize_timestamp(value: Any) -> str | None:
        if value is None:
            return None
        text = str(value).strip()
        if not text:
            return None
        if "T" not in text and " " in text:
            text = text.replace(" ", "T", 1)
        return text

    @staticmethod
    def _decode_extra_fields(raw: str | None) -> dict[str, Any] | None:
        if not raw:
            return None
        try:
            decoded = json.loads(raw)
        except json.JSONDecodeError:
            return {"raw_extra_fields": raw}
        return decoded if isinstance(decoded, dict) else {"value": decoded}

    @staticmethod
    def _int_or_none(value: Any) -> int | None:
        if value is None:
            return None
        try:
            return int(value)
        except (TypeError, ValueError):
            return None

    @staticmethod
    def _block_text(block: dict[str, Any]) -> str:
        content = str(block.get("content") or "")
        if block.get("block_type") == "tool_call":
            return ""
        return content

    def _blocks_to_text(self, blocks: list[dict[str, Any]]) -> str:
        parts = [self._block_text(block) for block in blocks]
        return "\n".join(part for part in parts if part).strip()

    def _assistant_message_to_step(
        self,
        *,
        message: dict[str, Any],
        step_id: int,
        timestamp: str | None,
    ) -> Step:
        blocks = message["blocks"]
        text_parts: list[str] = []
        reasoning_parts: list[str] = []
        tool_calls: list[ToolCall] = []
        extra: dict[str, Any] = {"cpe_message_id": message["id"]}
        for key in ("model_ref", "model_type", "model_display_name"):
            if message.get(key):
                extra[key] = message[key]

        for block in blocks:
            block_type = block.get("block_type")
            content = str(block.get("content") or "")
            if block_type == "thinking":
                if content:
                    reasoning_parts.append(content)
            elif block_type == "tool_call":
                tool_call = self._tool_call_from_block(block)
                if tool_call is not None:
                    tool_calls.append(tool_call)
            else:
                if content:
                    text_parts.append(content)

        step = Step(
            step_id=step_id,
            timestamp=timestamp,
            source="agent",
            model_name=message.get("model_id") or self.model_name,
            message="\n".join(text_parts).strip() or "(tool use)",
            reasoning_content="\n\n".join(reasoning_parts).strip() or None,
            tool_calls=tool_calls or None,
            metrics=self._metrics_from_message(message),
            extra=extra,
        )
        return step

    @staticmethod
    def _metrics_from_message(message: dict[str, Any]) -> Metrics | None:
        prompt_tokens = message.get("input_tokens")
        completion_tokens = message.get("output_tokens")
        cached_tokens = message.get("cache_read_tokens")
        cache_write_tokens = message.get("cache_write_tokens")
        if all(
            value is None
            for value in (
                prompt_tokens,
                completion_tokens,
                cached_tokens,
                cache_write_tokens,
            )
        ):
            return None

        extra = None
        if cache_write_tokens is not None:
            extra = {"cache_write_tokens": cache_write_tokens}
        return Metrics(
            prompt_tokens=prompt_tokens,
            completion_tokens=completion_tokens,
            cached_tokens=cached_tokens,
            extra=extra,
        )

    @staticmethod
    def _final_metrics_from_steps(steps: list[Step]) -> FinalMetrics:
        total_prompt_tokens = 0
        total_completion_tokens = 0
        total_cached_tokens = 0
        total_cache_write_tokens = 0
        saw_prompt_tokens = False
        saw_completion_tokens = False
        saw_cached_tokens = False
        saw_cache_write_tokens = False

        for step in steps:
            if step.metrics is None:
                continue
            if step.metrics.prompt_tokens is not None:
                total_prompt_tokens += step.metrics.prompt_tokens
                saw_prompt_tokens = True
            if step.metrics.completion_tokens is not None:
                total_completion_tokens += step.metrics.completion_tokens
                saw_completion_tokens = True
            if step.metrics.cached_tokens is not None:
                total_cached_tokens += step.metrics.cached_tokens
                saw_cached_tokens = True
            if step.metrics.extra and step.metrics.extra.get("cache_write_tokens") is not None:
                total_cache_write_tokens += int(step.metrics.extra["cache_write_tokens"])
                saw_cache_write_tokens = True

        extra = None
        if saw_cache_write_tokens:
            extra = {"total_cache_write_tokens": total_cache_write_tokens}

        return FinalMetrics(
            total_prompt_tokens=total_prompt_tokens if saw_prompt_tokens else None,
            total_completion_tokens=(
                total_completion_tokens if saw_completion_tokens else None
            ),
            total_cached_tokens=total_cached_tokens if saw_cached_tokens else None,
            total_steps=len(steps),
            extra=extra,
        )

    @staticmethod
    def _tool_call_from_block(block: dict[str, Any]) -> ToolCall | None:
        try:
            payload = json.loads(str(block.get("content") or "{}"))
        except json.JSONDecodeError:
            payload = {}

        if not isinstance(payload, dict):
            payload = {}

        name = payload.get("name") or payload.get("tool_name") or "unknown_tool"
        args = payload.get("parameters") or payload.get("arguments") or {}
        if not isinstance(args, dict):
            args = {"value": args}

        call_id = str(block.get("id") or payload.get("id") or f"{name}-unknown")
        return ToolCall(tool_call_id=call_id, function_name=str(name), arguments=args)

    def _attach_tool_result(self, step: Step, blocks: list[dict[str, Any]]) -> None:
        if not step.tool_calls:
            return

        valid_call_ids = {tool_call.tool_call_id for tool_call in step.tool_calls}
        results: list[ObservationResult] = []
        for block in blocks:
            call_id = str(block.get("id") or "")
            if call_id not in valid_call_ids:
                continue
            results.append(
                ObservationResult(
                    source_call_id=call_id,
                    content=self._block_text(block),
                )
            )

        if not results:
            return

        if step.observation is None:
            step.observation = Observation(results=[])
        step.observation.results.extend(results)

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
        z_api_key = self._extra_env.get("Z_API_KEY") or os.environ.get("Z_API_KEY")
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
        db_path = f"/logs/agent/{self._CONVERSATION_DB_FILENAME}"

        await self.exec_as_agent(
            environment,
            command=(
                "mkdir -p /logs/agent/command-0 && "
                'export PATH="$HOME/.local/bin:$PATH" && '
                f"cpe -n --skip-stdin --db-path {shlex.quote(db_path)} "
                f"-m {model_ref} {escaped_instruction} "
                "2>&1 </dev/null | stdbuf -oL tee "
                f"/logs/agent/{self._OUTPUT_FILENAME} /logs/agent/command-0/stdout.txt"
            ),
            env=self._run_env(),
        )

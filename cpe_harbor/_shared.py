import base64
import json
import os
import re
import shlex
import sqlite3
from pathlib import Path
from typing import Any, Callable
from urllib.parse import unquote, urlparse


class CPEAgentMixin:
    """Framework-neutral CPE command and trajectory behavior."""

    _OUTPUT_FILENAME = "cpe.txt"
    _CONVERSATION_DB_FILENAME = ".cpeconvo"
    _CONFIG_FILENAME = "cpe.yaml"
    _SYSTEM_PROMPT_FILENAME = "agent_instructions.md"
    _GO_VERSION = "1.26.4"

    _trajectory_agent_cls: Any = None
    _trajectory_final_metrics_cls: Any = None
    _trajectory_metrics_cls: Any = None
    _trajectory_observation_cls: Any = None
    _trajectory_observation_result_cls: Any = None
    _trajectory_step_cls: Any = None
    _trajectory_tool_call_cls: Any = None
    _trajectory_cls: Any = None
    _format_trajectory_json: Callable[[Any], str] | None = None

    def _init_cpe_options(
        self,
        *,
        config_url: str | None,
        system_prompt_url: str | None,
        model_ref: str | None,
        thinking_level: str | None,
        auth_url: str | None = None,
    ) -> None:
        if not config_url:
            raise ValueError("config_url agent kwarg is required")
        if not system_prompt_url:
            raise ValueError("system_prompt_url agent kwarg is required")
        if not model_ref:
            raise ValueError("model_ref agent kwarg is required")
        if not thinking_level:
            raise ValueError("thinking_level agent kwarg is required")

        self._config_url = config_url
        self._system_prompt_url = system_prompt_url
        self._auth_url = auth_url
        self._model_ref = model_ref
        self._thinking_level = thinking_level

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

    def _install_go_command(self) -> str:
        go_version = shlex.quote(self._GO_VERSION)
        return (
            "set -euo pipefail; "
            "arch=$(uname -m); "
            "case \"$arch\" in "
            "x86_64|amd64) go_arch=amd64 ;; "
            "arm64|aarch64) go_arch=arm64 ;; "
            "*) echo \"unsupported Go architecture: $arch\" >&2; exit 1 ;; "
            "esac; "
            "tmp=$(mktemp -d); "
            "trap 'rm -rf \"$tmp\"' EXIT INT TERM; "
            f"curl -fsSL https://go.dev/dl/go{go_version}.linux-${{go_arch}}.tar.gz "
            "-o \"$tmp/go.tgz\" && "
            "rm -rf /usr/local/go && "
            "tar -C /usr/local -xzf \"$tmp/go.tgz\" && "
            "ln -sf /usr/local/go/bin/go /usr/local/bin/go && "
            "ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt && "
            "go version"
        )

    def _install_cpe_command(self) -> str:
        install_version = self._install_version()
        quoted_install_version = shlex.quote(install_version)
        install_pkg = shlex.quote(f"github.com/spachava753/cpe@{install_version}")
        return (
            "set -euo pipefail; "
            "mkdir -p \"$HOME/.local/bin\"; "
            f"version={quoted_install_version}; "
            "os=$(uname -s | tr '[:upper:]' '[:lower:]'); "
            "case \"$os\" in darwin|linux) ;; *) echo \"unsupported OS: $os\" >&2; exit 1 ;; esac; "
            "arch=$(uname -m); "
            "case \"$arch\" in x86_64|amd64) arch=x86_64 ;; arm64|aarch64) arch=arm64 ;; *) echo \"unsupported architecture: $arch\" >&2; exit 1 ;; esac; "
            "archive=\"cpe_${os}_${arch}.tar.gz\"; "
            "if [ \"$version\" = \"latest\" ]; then "
            "base_url=\"https://github.com/spachava753/cpe/releases/latest/download\"; "
            "else base_url=\"https://github.com/spachava753/cpe/releases/download/${version}\"; fi; "
            "tmp=$(mktemp -d); trap 'rm -rf \"$tmp\"' EXIT INT TERM; "
            "if curl -fsSL \"$base_url/$archive\" -o \"$tmp/$archive\" && "
            "curl -fsSL \"$base_url/checksums.txt\" -o \"$tmp/checksums.txt\" && "
            "checksum_line=$(grep \"[[:space:]]${archive}$\" \"$tmp/checksums.txt\") && "
            "[ -n \"$checksum_line\" ] && "
            "printf '%s\n' \"$checksum_line\" | (cd \"$tmp\" && sha256sum -c - >/dev/null) && "
            "tar -xzf \"$tmp/$archive\" -C \"$tmp\" && [ -f \"$tmp/cpe\" ]; then "
            "install -m 0755 \"$tmp/cpe\" \"$HOME/.local/bin/cpe\"; "
            "else "
            "echo \"CPE release binary install failed; falling back to go install\" >&2; "
            "install_log=\"$HOME/.local/share/cpe-harbor/cpe-install.log\"; "
            "mkdir -p \"$HOME/.local/share/cpe-harbor\"; "
            f"if ! GOMAXPROCS=1 GOGC=25 GOMEMLIMIT=512MiB GOBIN=\"$HOME/.local/bin\" go install -p=1 {install_pkg} >\"$install_log\" 2>&1; then "
            "filtered_log=\"$tmp/cpe-install.filtered.log\"; "
            "grep -v '^go: downloading ' \"$install_log\" >\"$filtered_log\" || true; "
            "if [ -s \"$filtered_log\" ]; then "
            "echo \"go install failed; showing last 80 non-download log lines from $install_log\" >&2; "
            "tail -n 80 \"$filtered_log\" >&2 || true; "
            "else "
            "echo \"go install failed after producing only module download output; showing last 40 raw log lines from $install_log\" >&2; "
            "tail -n 40 \"$install_log\" >&2 || true; "
            "fi; "
            "exit 1; "
            "fi; "
            "fi && "
            "\"$HOME/.local/bin/cpe\" --version"
        )

    def _config_path(self) -> str:
        return f"$HOME/.config/cpe/{self._CONFIG_FILENAME}"

    def _system_prompt_path(self) -> str:
        return f"$HOME/.config/cpe/{self._SYSTEM_PROMPT_FILENAME}"

    def _auth_path(self) -> str:
        return "$HOME/.config/cpe/auth.json"

    def _install_config_command(self) -> str:
        config_path = self._config_path()
        system_prompt_path = self._system_prompt_path()
        commands = [
            "set -euo pipefail",
            "mkdir -p \"$HOME/.config/cpe\"",
            self._install_artifact_command(self._config_url, config_path),
            self._install_artifact_command(
                self._system_prompt_url, system_prompt_path
            ),
        ]
        return "\n".join(commands)

    def _install_artifact_command(self, source: str, target_path: str) -> str:
        parsed = urlparse(source)
        if parsed.scheme == "file":
            if parsed.netloc not in {"", "localhost"}:
                raise ValueError(f"unsupported file URL host: {parsed.netloc}")
            local_path = Path(unquote(parsed.path))
            encoded = base64.b64encode(local_path.read_bytes()).decode("ascii")
            return (
                f"base64 -d > \"{target_path}\" <<'CPE_HARBOR_ARTIFACT'\n"
                f"{encoded}\n"
                "CPE_HARBOR_ARTIFACT"
            )

        return f"curl -fsSL {shlex.quote(source)} -o \"{target_path}\""

    def populate_context_post_run(self, context: Any) -> None:
        db_path = self.logs_dir / self._CONVERSATION_DB_FILENAME
        if not db_path.exists():
            self._debug(f"No CPE conversation database found at {db_path}")
            return

        try:
            trajectory = self._convert_conversation_db_to_trajectory(db_path)
        except Exception:
            self._exception("Failed to convert CPE conversation to trajectory")
            return

        if not trajectory:
            return

        trajectory_path = self.logs_dir / "trajectory.json"
        try:
            formatter = self._format_trajectory_json
            if formatter is None:
                raise RuntimeError("CPE trajectory formatter is not configured")
            trajectory_path.write_text(
                formatter(trajectory.to_json_dict()), encoding="utf-8"
            )
            self._debug(f"Wrote CPE trajectory to {trajectory_path}")
        except OSError as exc:
            self._debug(f"Failed to write trajectory file {trajectory_path}: {exc}")

        if trajectory.final_metrics:
            if trajectory.final_metrics.total_prompt_tokens is not None:
                context.n_input_tokens = trajectory.final_metrics.total_prompt_tokens
            if trajectory.final_metrics.total_cached_tokens is not None:
                context.n_cache_tokens = trajectory.final_metrics.total_cached_tokens
            if trajectory.final_metrics.total_completion_tokens is not None:
                context.n_output_tokens = trajectory.final_metrics.total_completion_tokens
            if trajectory.final_metrics.total_cost_usd is not None:
                context.cost_usd = trajectory.final_metrics.total_cost_usd

    def _convert_conversation_db_to_trajectory(self, db_path: Path) -> Any | None:
        session = self._read_cpe_session_metadata(db_path)
        messages = self._read_cpe_messages(db_path, session.get("last_message_id"))
        if not messages:
            self._debug("No CPE messages found for trajectory conversion")
            return None

        steps: list[Any] = []
        last_agent_step: Any | None = None
        session_id = str(session.get("id") or messages[0]["id"])

        for message in messages:
            role = message["role"]
            timestamp = message.get("created_at")
            blocks = message["blocks"]

            if role == "tool_result":
                if last_agent_step is not None:
                    self._attach_tool_result(last_agent_step, blocks)
                else:
                    steps.append(
                        self._trajectory_step_cls(
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
                self._trajectory_step_cls(
                    step_id=len(steps) + 1,
                    timestamp=timestamp,
                    source=source,
                    message=self._blocks_to_text(blocks) or "(empty message)",
                )
            )
            last_agent_step = None

        if not steps:
            return None

        return self._trajectory_cls(
            schema_version="ATIF-v1.7",
            session_id=session_id,
            agent=self._trajectory_agent_cls(
                name="cpe",
                version=self.version() or "unknown",
                model_name=self.model_name,
            ),
            steps=steps,
            final_metrics=self._final_metrics_from_steps(
                steps, total_cost_usd=session.get("cost_usd")
            ),
        )

    def _read_cpe_session_metadata(self, db_path: Path) -> dict[str, Any]:
        with sqlite3.connect(f"file:{db_path}?mode=ro", uri=True) as conn:
            conn.row_factory = sqlite3.Row
            session_table_count = conn.execute(
                """
                SELECT COUNT(*)
                FROM sqlite_master
                WHERE type = 'table' AND name = 'acp_sessions'
                """
            ).fetchone()[0]
            if session_table_count == 0:
                return {}

            session_columns = {
                row["name"]
                for row in conn.execute("PRAGMA table_info(acp_sessions)").fetchall()
            }
            select_exprs = ["id"]
            for column in ("last_message_id", "cost_usd"):
                if column in session_columns:
                    select_exprs.append(column)
                else:
                    select_exprs.append(f"NULL AS {column}")
            order_by = (
                "created_at DESC, rowid DESC"
                if "created_at" in session_columns
                else "rowid DESC"
            )
            row = conn.execute(
                f"""
                SELECT {", ".join(select_exprs)}
                FROM acp_sessions
                ORDER BY {order_by}
                LIMIT 1
                """
            ).fetchone()

        if row is None:
            return {}
        return {
            "id": row["id"],
            "last_message_id": row["last_message_id"],
            "cost_usd": self._float_or_none(row["cost_usd"]),
        }

    def _read_cpe_messages(
        self, db_path: Path, last_message_id: str | None = None
    ) -> list[dict[str, Any]]:
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
            if last_message_id and "parent_id" in message_columns:
                rows = conn.execute(
                    f"""
                    WITH RECURSIVE session_chain(id, depth) AS (
                        SELECT ? AS id, 0 AS depth
                        UNION ALL
                        SELECT m.parent_id, session_chain.depth + 1
                        FROM messages m
                        JOIN session_chain ON m.id = session_chain.id
                        WHERE m.parent_id IS NOT NULL
                    )
                    SELECT
                        {", ".join(select_exprs)}
                    FROM session_chain
                    JOIN messages m ON m.id = session_chain.id
                    LEFT JOIN blocks b ON b.message_id = m.id
                    WHERE session_chain.id IS NOT NULL
                    ORDER BY session_chain.depth DESC, b.sequence_order ASC
                    """,
                    (last_message_id,),
                ).fetchall()
            else:
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
    def _float_or_none(value: Any) -> float | None:
        if value is None:
            return None
        try:
            return float(value)
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
    ) -> Any:
        blocks = message["blocks"]
        text_parts: list[str] = []
        reasoning_parts: list[str] = []
        tool_calls: list[Any] = []
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

        return self._trajectory_step_cls(
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

    def _metrics_from_message(self, message: dict[str, Any]) -> Any | None:
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
        return self._trajectory_metrics_cls(
            prompt_tokens=prompt_tokens,
            completion_tokens=completion_tokens,
            cached_tokens=cached_tokens,
            extra=extra,
        )

    def _final_metrics_from_steps(
        self, steps: list[Any], *, total_cost_usd: float | None = None
    ) -> Any:
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

        return self._trajectory_final_metrics_cls(
            total_prompt_tokens=total_prompt_tokens if saw_prompt_tokens else None,
            total_completion_tokens=(
                total_completion_tokens if saw_completion_tokens else None
            ),
            total_cached_tokens=total_cached_tokens if saw_cached_tokens else None,
            total_cost_usd=total_cost_usd,
            total_steps=len(steps),
            extra=extra,
        )

    def _tool_call_from_block(self, block: dict[str, Any]) -> Any | None:
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
        return self._trajectory_tool_call_cls(
            tool_call_id=call_id, function_name=str(name), arguments=args
        )

    def _attach_tool_result(self, step: Any, blocks: list[dict[str, Any]]) -> None:
        if not step.tool_calls:
            return

        valid_call_ids = {tool_call.tool_call_id for tool_call in step.tool_calls}
        results: list[Any] = []
        for block in blocks:
            call_id = str(block.get("id") or "")
            if call_id not in valid_call_ids:
                continue
            results.append(
                self._trajectory_observation_result_cls(
                    source_call_id=call_id,
                    content=self._block_text(block),
                )
            )

        if not results:
            return

        if step.observation is None:
            step.observation = self._trajectory_observation_cls(results=[])
        step.observation.results.extend(results)

    def _cpe_model_ref(self) -> str:
        return self._model_ref

    def _is_zai_profile(self) -> bool:
        return self._model_ref == "glm"

    def _run_env(self) -> dict[str, str]:
        env: dict[str, str] = {}
        if self._is_zai_profile():
            z_api_key = self._extra_env.get("Z_API_KEY") or os.environ.get("Z_API_KEY")
            if not z_api_key:
                raise ValueError("Z_API_KEY must be set to run CPE with the Z.ai profile")
            env["Z_API_KEY"] = z_api_key

        if self._auth_url:
            parsed = urlparse(self._auth_url)
            if parsed.scheme != "file":
                raise ValueError("auth_url currently supports local file:// URLs only")
            if parsed.netloc not in {"", "localhost"}:
                raise ValueError(f"unsupported file URL host: {parsed.netloc}")
            auth_path = Path(unquote(parsed.path))
            env["CPE_AUTH_JSON_B64"] = base64.b64encode(auth_path.read_bytes()).decode(
                "ascii"
            )

        return env

    def _run_command(self, instruction: str) -> str:
        escaped_instruction = shlex.quote(instruction)
        config_path = self._config_path()
        db_path = f"/logs/agent/{self._CONVERSATION_DB_FILENAME}"
        model_ref = shlex.quote(self._cpe_model_ref())
        thinking_level = shlex.quote(self._thinking_level)
        auth_setup = (
            "if [ -n \"${CPE_AUTH_JSON_B64:-}\" ]; then "
            "mkdir -p \"$HOME/.config/cpe\" && "
            "printf '%s' \"$CPE_AUTH_JSON_B64\" | base64 -d > \"$HOME/.config/cpe/auth.json\" && "
            "chmod 600 \"$HOME/.config/cpe/auth.json\"; "
            "fi && "
        )
        return (
            "mkdir -p /logs/agent/command-0 && "
            'export PATH="$HOME/.local/bin:$PATH" && '
            f"{auth_setup}"
            f"cpe --config \"{config_path}\" "
            f"--db-path {shlex.quote(db_path)} "
            f"--model {model_ref} "
            f"--thinking-level {thinking_level} -- {escaped_instruction} "
            "2>&1 </dev/null | stdbuf -oL tee "
            f"/logs/agent/{self._OUTPUT_FILENAME} /logs/agent/command-0/stdout.txt"
        )

    def _debug(self, message: str) -> None:
        logger = getattr(self, "logger", None)
        if logger is not None:
            logger.debug(message)

    def _exception(self, message: str) -> None:
        logger = getattr(self, "logger", None)
        if logger is not None:
            logger.exception(message)

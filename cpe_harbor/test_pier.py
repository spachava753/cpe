import base64
import contextlib
import functools
import shlex
import sqlite3
import tempfile
import threading
import unittest
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

try:
    from cpe_harbor.pier import CPE
    from pier.agents.installed.base import BaseInstalledAgent
    from pier.models.trajectories import Trajectory
except Exception as exc:  # pragma: no cover - exercised only without Pier installed.
    raise unittest.SkipTest(f"Pier is not available: {exc}") from exc


class CPEPierAdapterTest(unittest.TestCase):
    def test_constructor_creates_pier_installed_agent(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            agent = self._agent(Path(tmpdir))

            self.assertIsInstance(agent, BaseInstalledAgent)
            self.assertEqual(agent.name(), "cpe")
            self.assertEqual(agent.version(), "latest")

    def test_constructor_requires_explicit_cpe_profile_options(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            with self.assertRaisesRegex(ValueError, "model_ref agent kwarg is required"):
                CPE(
                    Path(tmpdir),
                    model_name="zai/glm-5.1",
                    config_url="https://example.com/cpe.yaml",
                    system_prompt_url="https://example.com/prompt.md",
                    thinking_level="high",
                )

            agent = CPE(
                Path(tmpdir),
                model_name="zai/glm-5.1",
                config_url="https://example.com/cpe.yaml",
                system_prompt_url="https://example.com/prompt.md",
                model_ref="glm",
            )
            self.assertEqual(agent._thinking_level, "")

    def test_install_spec_declares_pier_setup_steps(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            agent = self._agent(Path(tmpdir), version="0.41.0")

            spec = agent.install_spec()

            self.assertEqual(spec.agent_name, "cpe")
            self.assertEqual(spec.version, "v0.41.0")
            self.assertEqual(
                [step.user for step in spec.steps],
                ["root", "root", "agent", "agent", "agent"],
            )
            self.assertEqual(spec.verification_command, agent.get_version_command())
            self.assertIn("apt-get install -y ca-certificates curl gzip tar", spec.steps[0].run)
            self.assertIn("go1.26.4.linux-${go_arch}.tar.gz", spec.steps[1].run)
            self.assertIn("version=v0.41.0", spec.steps[2].run)
            self.assertIn(
                "https://github.com/spachava753/cpe/releases/download/${version}",
                spec.steps[2].run,
            )
            self.assertIn("module cpe-code-mode-cache", spec.steps[3].run)
            self.assertIn(
                "require github.com/modelcontextprotocol/go-sdk v1.6.1",
                spec.steps[3].run,
            )
            self.assertIn("GOPROXY=https://proxy.golang.org,direct", spec.steps[3].run)
            self.assertIn("GOSUMDB=sum.golang.org", spec.steps[3].run)
            self.assertIn("go mod download all", spec.steps[3].run)
            self.assertIn(
                'base64 -d > "$HOME/.config/cpe/cpe.yaml"', spec.steps[4].run
            )
            self.assertIn(
                'base64 -d > "$HOME/.config/cpe/agent_instructions.md"',
                spec.steps[4].run,
            )
            self.assertIn(
                base64.b64encode(self._config_path(Path(tmpdir)).read_bytes()).decode(
                    "ascii"
                ),
                spec.steps[4].run,
            )
            self.assertNotIn("curl -fsSL file://", spec.steps[4].run)

    def test_oauth_credentials_are_runtime_only(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            auth_path = root / "auth.json"
            auth_path.write_text('{"openai":{"type":"oauth"}}\n', encoding="utf-8")
            agent = self._agent(
                root,
                auth_url=auth_path.as_uri(),
                extra_env={"Z_API_KEY": "secret"},
            )

            install_command = agent.install_spec().steps[4].run
            run_command = agent._run_command("fix the bug")
            run_env = agent._run_env()

            self.assertNotIn("auth.json", install_command)
            self.assertNotIn(
                base64.b64encode(auth_path.read_bytes()).decode("ascii"),
                install_command,
            )
            self.assertIn("CPE_AUTH_JSON_B64", run_env)
            self.assertEqual(
                base64.b64decode(run_env["CPE_AUTH_JSON_B64"]),
                auth_path.read_bytes(),
            )
            self.assertIn('base64 -d > "$HOME/.config/cpe/auth.json"', run_command)
            self.assertIn('chmod 600 "$HOME/.config/cpe/auth.json"', run_command)
            self.assertNotIn(run_env["CPE_AUTH_JSON_B64"], run_command)

    def test_network_allowlist_includes_install_and_selected_runtime_domains(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            config_path = self._config_path(root)
            config_path.write_text(
                """
version: "1.0"
models:
  - ref: glm
    type: openai
    base_url: https://api.z.ai/api/coding/paas/v4
    mcpServers:
      docs:
        transport: http
        url: https://mcp.example.com/sse
  - ref: custom
    type: openai
    base_url: https://gateway.example.com/v1
""".strip()
                + "\n",
                encoding="utf-8",
            )
            prompt_path = self._prompt_path(root)
            prompt_path.write_text("prompt\n", encoding="utf-8")
            auth_path = root / "auth.json"
            auth_path.write_text('{"openai":{"type":"oauth"}}\n', encoding="utf-8")
            agent = CPE(
                root,
                model_name="zai/glm-5.1",
                config_url=config_path.as_uri(),
                system_prompt_url="https://raw.githubusercontent.com/spachava753/cpe/main/prompt.md",
                auth_url=auth_path.as_uri(),
                model_ref="glm",
                thinking_level="high",
            )

            allowlist = agent.network_allowlist()

            self.assertIn("api.z.ai", allowlist.domains)
            self.assertIn("mcp.example.com", allowlist.domains)
            self.assertIn("github.com", allowlist.domains)
            self.assertIn("go.dev", allowlist.domains)
            self.assertIn("go.googlesource.com", allowlist.domains)
            self.assertIn("golang.org", allowlist.domains)
            self.assertIn("proxy.golang.org", allowlist.domains)
            self.assertIn("raw.githubusercontent.com", allowlist.domains)
            self.assertIn("release-assets.githubusercontent.com", allowlist.domains)
            self.assertNotIn("gateway.example.com", allowlist.domains)
            self.assertNotIn("api.anthropic.com", allowlist.domains)
            self.assertNotIn("api.groq.com", allowlist.domains)
            self.assertNotIn(".googleapis.com", allowlist.domains)
            self.assertNotIn("openrouter.ai", allowlist.domains)

    def test_network_allowlist_uses_selected_profile_provider_defaults(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            config_path = self._config_path(root)
            config_path.write_text(
                """
version: "1.0"
models:
  - ref: claude
    type: anthropic
    auth_method: oauth
  - ref: gpt
    type: openai
""".strip()
                + "\n",
                encoding="utf-8",
            )
            self._prompt_path(root).write_text("prompt\n", encoding="utf-8")
            agent = CPE(
                root,
                model_name="anthropic/claude-sonnet-4-5",
                config_url=config_path.as_uri(),
                system_prompt_url=self._prompt_path(root).as_uri(),
                model_ref="claude",
                thinking_level="high",
            )

            allowlist = agent.network_allowlist()

            self.assertIn("api.anthropic.com", allowlist.domains)
            self.assertIn("console.anthropic.com", allowlist.domains)
            self.assertNotIn("api.openai.com", allowlist.domains)

    def test_network_allowlist_uses_responses_oauth_defaults(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            config_path = self._config_path(root)
            config_path.write_text(
                """
version: "1.0"
models:
  - ref: gpt
    type: responses
    auth_method: oauth
""".strip()
                + "\n",
                encoding="utf-8",
            )
            self._prompt_path(root).write_text("prompt\n", encoding="utf-8")
            agent = CPE(
                root,
                model_name="openai/gpt-5.5",
                config_url=config_path.as_uri(),
                system_prompt_url=self._prompt_path(root).as_uri(),
                model_ref="gpt",
                thinking_level="high",
            )

            allowlist = agent.network_allowlist()

            self.assertIn("auth.openai.com", allowlist.domains)
            self.assertIn("chatgpt.com", allowlist.domains)
            self.assertNotIn("api.openai.com", allowlist.domains)

    def test_network_allowlist_for_deep_swe_oauth_config_uses_chatgpt_codex(self) -> None:
        config_root = Path(__file__).parent / "configs" / "deep-swe"
        with tempfile.TemporaryDirectory() as tmpdir:
            agent = CPE(
                Path(tmpdir),
                model_name="openai/gpt-5.5",
                config_url=(config_root / "cpe.yaml").as_uri(),
                system_prompt_url=(config_root / "agent_instructions.md").as_uri(),
                model_ref="gpt",
                thinking_level="high",
            )

            allowlist = agent.network_allowlist()

            self.assertIn("auth.openai.com", allowlist.domains)
            self.assertIn("chatgpt.com", allowlist.domains)
            self.assertNotIn("api.openai.com", allowlist.domains)

    def test_network_allowlist_reads_http_config_artifacts(self) -> None:
        class QuietHandler(SimpleHTTPRequestHandler):
            def log_message(self, format: str, *args: object) -> None:
                pass

        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            config_path = self._config_path(root)
            config_path.write_text(
                """
version: "1.0"
models:
  - ref: custom
    type: openai
    base_url: https://gateway.example.com/v1
""".strip()
                + "\n",
                encoding="utf-8",
            )
            self._prompt_path(root).write_text("prompt\n", encoding="utf-8")
            handler = functools.partial(QuietHandler, directory=str(root))
            server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                config_url = f"http://127.0.0.1:{server.server_port}/cpe.yaml"
                agent = CPE(
                    root,
                    model_name="openai/custom",
                    config_url=config_url,
                    system_prompt_url=self._prompt_path(root).as_uri(),
                    model_ref="custom",
                    thinking_level="high",
                )

                allowlist = agent.network_allowlist()
            finally:
                server.shutdown()
                server.server_close()
                thread.join(timeout=5)

            self.assertIn("127.0.0.1", allowlist.domains)
            self.assertIn("gateway.example.com", allowlist.domains)
            self.assertNotIn("api.openai.com", allowlist.domains)

    def test_run_command_uses_current_cpe_prompt_cli(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            agent = self._agent(Path(tmpdir))
            instruction = "fix the bug"

            command = agent._run_command(instruction)

            self.assertIn('export PATH="$HOME/.local/bin:$PATH"', command)
            self.assertIn('cpe --config "$HOME/.config/cpe/cpe.yaml"', command)
            self.assertIn("--db-path /logs/agent/.cpeconvo", command)
            self.assertIn("--model glm --thinking-level high --", command)
            self.assertIn(shlex.quote(instruction), command)
            self.assertIn("| stdbuf -oL tee", command)
            self.assertNotIn("--skip-stdin", command)

    def test_run_command_omits_thinking_level_when_unset(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            agent = self._agent(Path(tmpdir), thinking_level=None)

            command = agent._run_command("fix the bug")

            self.assertIn("--model glm --", command)
            self.assertNotIn("--thinking-level", command)

    def test_run_env_uses_explicit_model_ref(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            agent = self._agent(Path(tmpdir), extra_env={"Z_API_KEY": "secret"})
            agent.model_name = "openai/gpt-5.5"

            self.assertEqual(agent._run_env(), {"Z_API_KEY": "secret"})

            agent._model_ref = "gpt"
            agent._extra_env = {}
            agent.model_name = "zai/glm-5.1"
            self.assertEqual(agent._run_env(), {})

    def test_conversation_conversion_uses_pier_trajectory_models(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            agent = self._agent(root)
            db_path = self._write_conversation_db(root)

            trajectory = agent._convert_conversation_db_to_trajectory(db_path)

            self.assertIsInstance(trajectory, Trajectory)
            assert trajectory is not None
            self.assertEqual(trajectory.schema_version, "ATIF-v1.7")
            self.assertEqual(trajectory.session_id, "session-1")
            self.assertEqual(trajectory.final_metrics.total_prompt_tokens, 11)
            self.assertEqual(trajectory.final_metrics.total_completion_tokens, 7)
            self.assertEqual(trajectory.final_metrics.total_cached_tokens, 3)
            self.assertEqual(trajectory.final_metrics.total_cost_usd, 0.42)
            self.assertEqual(trajectory.final_metrics.extra, {"total_cache_write_tokens": 2})
            self.assertEqual(trajectory.steps[1].model_name, "glm-5.1")
            self.assertEqual(trajectory.steps[1].extra["model_ref"], "glm")

    @staticmethod
    def _config_path(root: Path) -> Path:
        return root / "cpe.yaml"

    @staticmethod
    def _prompt_path(root: Path) -> Path:
        return root / "agent_instructions.md"

    def _agent(
        self,
        root: Path,
        *,
        version: str | None = None,
        auth_url: str | None = None,
        thinking_level: str | None = "high",
        extra_env: dict[str, str] | None = None,
    ) -> CPE:
        config_path = self._config_path(root)
        if not config_path.exists():
            config_path.write_text(
                """
version: "1.0"
models:
  - ref: glm
    type: openai
    base_url: https://api.z.ai/api/coding/paas/v4
""".strip()
                + "\n",
                encoding="utf-8",
            )
        prompt_path = self._prompt_path(root)
        if not prompt_path.exists():
            prompt_path.write_text("agent prompt\n", encoding="utf-8")
        return CPE(
            root,
            model_name="zai/glm-5.1",
            config_url=config_path.as_uri(),
            system_prompt_url=prompt_path.as_uri(),
            auth_url=auth_url,
            model_ref="glm",
            thinking_level=thinking_level,
            version=version,
            extra_env=extra_env,
        )

    @staticmethod
    def _write_conversation_db(root: Path) -> Path:
        db_path = root / ".cpeconvo"
        with contextlib.closing(sqlite3.connect(db_path)) as conn:
            with conn:
                conn.executescript(
                    """
                    CREATE TABLE messages (
                        id TEXT PRIMARY KEY,
                        parent_id TEXT,
                        compaction_parent_id TEXT,
                        role TEXT NOT NULL,
                        tool_result_error BOOLEAN NOT NULL DEFAULT 0,
                        message_extra_fields TEXT,
                        model_ref TEXT,
                        model_id TEXT,
                        model_type TEXT,
                        model_display_name TEXT,
                        input_tokens INTEGER,
                        output_tokens INTEGER,
                        cache_read_tokens INTEGER,
                        cache_write_tokens INTEGER,
                        created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
                    );
                    CREATE TABLE blocks (
                        id TEXT,
                        message_id TEXT NOT NULL,
                        block_type TEXT NOT NULL,
                        modality_type INTEGER NOT NULL,
                        mime_type TEXT NOT NULL,
                        content TEXT NOT NULL,
                        extra_fields TEXT,
                        sequence_order INTEGER NOT NULL,
                        created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                        PRIMARY KEY (message_id, sequence_order)
                    );
                    CREATE TABLE acp_sessions (
                        id TEXT PRIMARY KEY,
                        last_message_id TEXT,
                        cwd TEXT NOT NULL,
                        title TEXT NOT NULL,
                        model_ref TEXT NOT NULL,
                        thinking_level TEXT NOT NULL DEFAULT '',
                        cost_usd REAL NOT NULL DEFAULT 0,
                        created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
                    );
                    INSERT INTO messages (id, role, created_at)
                    VALUES ('u1', 'user', '2026-01-01 00:00:00');
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('u1', 'content', 0, 'text/plain', 'prompt', 0);
                    INSERT INTO messages (
                        id, parent_id, role, model_ref, model_id, model_type,
                        model_display_name, input_tokens, output_tokens,
                        cache_read_tokens, cache_write_tokens, created_at
                    ) VALUES (
                        'a1', 'u1', 'assistant', 'glm', 'glm-5.1', 'zai',
                        'GLM 5.1', 11, 7, 3, 2, '2026-01-01 00:00:01'
                    );
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('a1', 'content', 0, 'text/plain', 'answer', 0);
                    INSERT INTO messages (id, role, created_at)
                    VALUES ('other', 'user', '2026-01-01 00:00:02');
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('other', 'content', 0, 'text/plain', 'unrelated', 0);
                    INSERT INTO acp_sessions (
                        id, last_message_id, cwd, title, model_ref, thinking_level,
                        cost_usd, created_at
                    ) VALUES (
                        'session-1', 'a1', '/workspace', 'benchmark', 'glm',
                        'high', 0.42, '2026-01-01 00:00:03'
                    );
                    """
                )
        return db_path


if __name__ == "__main__":
    unittest.main()

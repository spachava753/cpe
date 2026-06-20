import base64
import contextlib
import shlex
import sqlite3
import tempfile
import unittest
from pathlib import Path

try:
    from cpe_harbor import CPE
except Exception as exc:  # pragma: no cover - exercised only without Harbor installed.
    raise unittest.SkipTest(f"Harbor is not available: {exc}") from exc


class CPETrajectoryConversionTest(unittest.TestCase):
    def test_new_conversation_schema_populates_token_metrics(self) -> None:
        db_path = self._write_conversation_db(include_metadata_columns=True)
        trajectory = self._agent()._convert_conversation_db_to_trajectory(db_path)

        self.assertIsNotNone(trajectory)
        assert trajectory is not None
        self.assertEqual(trajectory.schema_version, "ATIF-v1.7")
        self.assertEqual(trajectory.session_id, "session-1")
        self.assertEqual(trajectory.final_metrics.total_prompt_tokens, 11)
        self.assertEqual(trajectory.final_metrics.total_completion_tokens, 7)
        self.assertEqual(trajectory.final_metrics.total_cached_tokens, 3)
        self.assertEqual(trajectory.final_metrics.total_cost_usd, 0.42)
        self.assertEqual(trajectory.final_metrics.total_steps, 2)
        self.assertEqual(
            trajectory.final_metrics.extra, {"total_cache_write_tokens": 2}
        )

        agent_step = trajectory.steps[1]
        self.assertEqual(agent_step.model_name, "glm-5.1")
        self.assertIsNotNone(agent_step.metrics)
        assert agent_step.metrics is not None
        self.assertEqual(agent_step.metrics.prompt_tokens, 11)
        self.assertEqual(agent_step.metrics.completion_tokens, 7)
        self.assertEqual(agent_step.metrics.cached_tokens, 3)
        self.assertEqual(agent_step.metrics.extra, {"cache_write_tokens": 2})
        self.assertEqual(agent_step.extra["model_ref"], "glm")
        self.assertEqual(agent_step.extra["model_type"], "zai")
        self.assertEqual(agent_step.extra["model_display_name"], "GLM 5.1")

    def test_old_conversation_schema_omits_token_metrics(self) -> None:
        db_path = self._write_conversation_db(include_metadata_columns=False)
        trajectory = self._agent()._convert_conversation_db_to_trajectory(db_path)

        self.assertIsNotNone(trajectory)
        assert trajectory is not None
        self.assertIsNone(trajectory.steps[1].metrics)
        self.assertIsNone(trajectory.final_metrics.total_prompt_tokens)
        self.assertIsNone(trajectory.final_metrics.total_completion_tokens)
        self.assertIsNone(trajectory.final_metrics.total_cached_tokens)
        self.assertIsNone(trajectory.final_metrics.total_cost_usd)
        self.assertIsNone(trajectory.final_metrics.extra)
        self.assertEqual(trajectory.final_metrics.total_steps, 2)

    def test_conversation_chain_follows_compaction_parent(self) -> None:
        db_path = self._write_compacted_conversation_db()
        trajectory = self._agent()._convert_conversation_db_to_trajectory(db_path)

        self.assertIsNotNone(trajectory)
        assert trajectory is not None
        self.assertEqual(
            [step.message for step in trajectory.steps],
            ["before compact", "first answer", "compaction summary", "after compact"],
        )
        self.assertEqual(
            [step.source for step in trajectory.steps],
            ["user", "agent", "user", "agent"],
        )
        self.assertEqual(trajectory.steps[1].extra["cpe_message_id"], "a1")
        self.assertEqual(trajectory.steps[3].extra["cpe_message_id"], "a2")
        self.assertEqual(trajectory.final_metrics.total_steps, 4)

    def test_install_version_accepts_release_tag_forms(self) -> None:
        agent = self._agent()

        agent._version = "latest"
        self.assertEqual(agent._install_version(), "latest")

        agent._version = "v0.41.0"
        self.assertEqual(agent._install_version(), "v0.41.0")

        agent._version = "0.41.0"
        self.assertEqual(agent._install_version(), "v0.41.0")

    def test_constructor_requires_config_url(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            with self.assertRaisesRegex(ValueError, "config_url agent kwarg is required"):
                CPE(
                    Path(tmpdir),
                    model_name="zai/glm-5.1",
                    system_prompt_url="https://example.com/prompt.md",
                )

    def test_constructor_requires_system_prompt_url(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            with self.assertRaisesRegex(
                ValueError, "system_prompt_url agent kwarg is required"
            ):
                CPE(
                    Path(tmpdir),
                    model_name="zai/glm-5.1",
                    config_url="https://example.com/cpe.yaml",
                )

    def test_constructor_requires_model_ref(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            with self.assertRaisesRegex(ValueError, "model_ref agent kwarg is required"):
                CPE(
                    Path(tmpdir),
                    model_name="zai/glm-5.1",
                    config_url="https://example.com/cpe.yaml",
                    system_prompt_url="https://example.com/prompt.md",
                    thinking_level="high",
                )

    def test_constructor_requires_thinking_level(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            with self.assertRaisesRegex(
                ValueError, "thinking_level agent kwarg is required"
            ):
                CPE(
                    Path(tmpdir),
                    model_name="zai/glm-5.1",
                    config_url="https://example.com/cpe.yaml",
                    system_prompt_url="https://example.com/prompt.md",
                    model_ref="glm",
                )

    def test_install_config_command_downloads_required_artifacts(self) -> None:
        agent = self._agent()
        agent._config_url = "https://example.com/config.yaml?token=it's-real"
        agent._system_prompt_url = "https://example.com/prompt.md?token=it's-real"

        command = agent._install_config_command()

        self.assertIn("mkdir -p \"$HOME/.config/cpe\"", command)
        self.assertIn(
            f"curl -fsSL {shlex.quote(agent._config_url)} "
            "-o \"$HOME/.config/cpe/cpe.yaml\"",
            command,
        )
        self.assertIn(
            f"curl -fsSL {shlex.quote(agent._system_prompt_url)} "
            "-o \"$HOME/.config/cpe/agent_instructions.md\"",
            command,
        )

    def test_install_config_command_embeds_local_file_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = Path(tmpdir) / "cpe.yaml"
            prompt_path = Path(tmpdir) / "agent_instructions.md"
            config_path.write_text("version: '1.0'\n", encoding="utf-8")
            prompt_path.write_text("agent prompt\n", encoding="utf-8")

            agent = self._agent()
            agent._config_url = config_path.as_uri()
            agent._system_prompt_url = prompt_path.as_uri()

            command = agent._install_config_command()

            self.assertIn(
                'base64 -d > "$HOME/.config/cpe/cpe.yaml"',
                command,
            )
            self.assertIn(
                'base64 -d > "$HOME/.config/cpe/agent_instructions.md"',
                command,
            )
            self.assertIn(
                base64.b64encode(config_path.read_bytes()).decode("ascii"),
                command,
            )
            self.assertIn(
                base64.b64encode(prompt_path.read_bytes()).decode("ascii"),
                command,
            )
            self.assertNotIn("curl -fsSL file://", command)

    def test_install_go_command_downloads_official_toolchain(self) -> None:
        agent = self._agent()

        command = agent._install_go_command()

        self.assertIn("go1.26.4.linux-${go_arch}.tar.gz", command)
        self.assertIn("ln -sf /usr/local/go/bin/go /usr/local/bin/go", command)
        self.assertIn("go version", command)

    def test_install_cpe_command_uses_release_binary_with_go_install_fallback(self) -> None:
        agent = self._agent()
        agent._version = "0.41.0"

        command = agent._install_cpe_command()

        self.assertIn("version=v0.41.0", command)
        self.assertIn(
            "https://github.com/spachava753/cpe/releases/download/${version}",
            command,
        )
        self.assertIn('archive="cpe_${os}_${arch}.tar.gz"', command)
        self.assertIn("checksums.txt", command)
        self.assertIn("sha256sum -c", command)
        self.assertIn("install -m 0755", command)
        self.assertIn("falling back to go install", command)
        self.assertIn(
            "install_log=\"$HOME/.local/share/cpe-harbor/cpe-install.log\"",
            command,
        )
        self.assertIn(
            "GOMAXPROCS=1 GOGC=25 GOMEMLIMIT=512MiB "
            "GOBIN=\"$HOME/.local/bin\" go install -p=1 "
            "github.com/spachava753/cpe@v0.41.0",
            command,
        )
        self.assertIn(">\"$install_log\" 2>&1", command)
        self.assertIn("grep -v '^go: downloading '", command)
        self.assertIn("tail -n 80 \"$filtered_log\"", command)
        self.assertIn("tail -n 40 \"$install_log\"", command)
        self.assertIn("\"$HOME/.local/bin/cpe\" --version", command)

    def test_warm_go_module_cache_command_downloads_code_mode_dependencies(self) -> None:
        agent = self._agent()

        command = agent._warm_go_module_cache_command()

        self.assertIn("module cpe-code-mode-cache", command)
        self.assertIn("go 1.26.4", command)
        self.assertIn(
            "require github.com/modelcontextprotocol/go-sdk v1.6.1",
            command,
        )
        self.assertIn("GOPROXY=https://proxy.golang.org,direct", command)
        self.assertIn("GOSUMDB=sum.golang.org", command)
        self.assertIn("go mod download all", command)

    def test_run_command_uses_current_direct_prompt_cli(self) -> None:
        agent = self._agent()
        instruction = "- fix the failing test"

        command = agent._run_command(instruction)

        self.assertIn('export PATH="$HOME/.local/bin:$PATH"', command)
        self.assertIn('cpe --config "$HOME/.config/cpe/cpe.yaml"', command)
        self.assertIn("--db-path /logs/agent/.cpeconvo", command)
        self.assertIn("--model glm --thinking-level high --", command)
        self.assertIn(shlex.quote(instruction), command)
        self.assertIn("</dev/null", command)
        self.assertIn("| stdbuf -oL tee", command)
        self.assertNotIn("--skip-stdin", command)
        self.assertNotIn(" -n ", command)

    def test_cpe_model_ref_is_always_explicit(self) -> None:
        agent = self._agent()
        agent._model_ref = "custom-profile"
        agent.model_name = "anthropic/claude-sonnet"

        self.assertEqual(agent._cpe_model_ref(), "custom-profile")

    def test_run_env_uses_explicit_model_ref_only(self) -> None:
        agent = self._agent()
        agent._extra_env = {"Z_API_KEY": "secret"}
        agent.model_name = "openai/gpt-5.5"
        self.assertEqual(agent._run_env(), {"Z_API_KEY": "secret"})

        agent._model_ref = "gpt"
        agent.model_name = "zai/glm-5.1"
        agent._extra_env = {}
        self.assertEqual(agent._run_env(), {})

    def test_bundled_glm_configs_use_implemented_provider_type(self) -> None:
        config_root = Path(__file__).parent / "configs"
        for relative_path in (
            Path("text_edit") / "cpe.yaml",
            Path("execute_go_code_edits") / "cpe.yaml",
        ):
            config_text = (config_root / relative_path).read_text(encoding="utf-8")

            self.assertIn("type: openai", config_text)
            self.assertNotIn("type: zai", config_text)
            self.assertIn("thinkingValues:", config_text)
            self.assertIn("- value: high", config_text)

    def test_bundled_prompts_include_execute_go_code_module_import_guard(self) -> None:
        config_root = Path(__file__).parent / "configs"
        for prompt_path in (
            config_root / "text_edit" / "agent_instructions.md",
            config_root / "execute_go_code_edits" / "agent_instructions.md",
        ):
            prompt = prompt_path.read_text(encoding="utf-8")

            self.assertIn(
                "IMPORTANT: YOU CANNOT IMPORT THE MODULE YOU ARE WORKING ON",
                prompt,
            )

    def test_execute_go_code_prompt_does_not_name_text_editor_tool(self) -> None:
        prompt_path = (
            Path(__file__).parent
            / "configs"
            / "execute_go_code_edits"
            / "agent_instructions.md"
        )
        prompt = prompt_path.read_text(encoding="utf-8").lower()

        self.assertNotIn("text_edit", prompt)
        self.assertNotIn("text edit", prompt)

    def test_deep_swe_config_uses_single_runtime_prompt_path(self) -> None:
        config_root = Path(__file__).parent / "configs" / "deep-swe"
        config_text = (config_root / "cpe.yaml").read_text(encoding="utf-8")
        agent_prompt = (config_root / "agent_instructions.md").read_text(
            encoding="utf-8"
        )
        gpt_prompt = (config_root / "gpt_instructions.md").read_text(
            encoding="utf-8"
        )

        self.assertIn("ref: gpt", config_text)
        self.assertIn("type: responses", config_text)
        self.assertIn("auth_method: oauth", config_text)
        self.assertIn("thinkingValues:", config_text)
        self.assertIn("- value: high", config_text)
        self.assertIn(
            "systemPromptPath: &agentPrompt $HOME/.config/cpe/agent_instructions.md",
            config_text,
        )
        self.assertIn("systemPromptPath: *agentPrompt", config_text)
        self.assertNotIn("gpt_instructions.md", config_text)
        self.assertNotIn("gptPrompt", config_text)
        self.assertNotIn("disable_edit_tool: true", config_text)
        self.assertIn("codeMode:", config_text)
        self.assertIn(
            "IMPORTANT: YOU CANNOT IMPORT THE MODULE YOU ARE WORKING ON",
            agent_prompt,
        )
        self.assertIn(
            "IMPORTANT: YOU CANNOT IMPORT THE MODULE YOU ARE WORKING ON",
            gpt_prompt,
        )

    def test_deep_swe_oauth_config_declares_required_thinking_level(self) -> None:
        config_text = (
            Path(__file__).parent / "configs" / "deep-swe" / "cpe.yaml"
        ).read_text(encoding="utf-8")

        self.assertIn("type: responses", config_text)
        self.assertIn("auth_method: oauth", config_text)
        self.assertIn("thinkingValues:", config_text)
        self.assertIn("- value: high", config_text)

    def test_experiment_configs_select_expected_editing_tools(self) -> None:
        configs_dir = Path(__file__).parent / "configs"
        text_edit_config = (configs_dir / "text_edit" / "cpe.yaml").read_text(
            encoding="utf-8"
        )
        execute_go_config = (
            configs_dir / "execute_go_code_edits" / "cpe.yaml"
        ).read_text(encoding="utf-8")

        self.assertNotIn("disable_edit_tool: true", text_edit_config)
        self.assertIn("codeMode:", text_edit_config)
        self.assertNotIn("mcpServers:", execute_go_config)
        self.assertIn("disable_edit_tool: true", execute_go_config)
        self.assertIn("codeMode:", execute_go_config)

    @staticmethod
    def _agent() -> CPE:
        agent = object.__new__(CPE)
        agent._version = "test"
        agent._model_ref = "glm"
        agent._thinking_level = "high"
        agent._extra_env = {}
        agent.model_name = "zai/glm-5.1"
        return agent

    def _write_conversation_db(self, *, include_metadata_columns: bool) -> Path:
        db_path = Path(tempfile.mkdtemp()) / ".cpeconvo"
        with contextlib.closing(sqlite3.connect(db_path)) as conn:
            with conn:
                self._create_schema(conn, include_metadata_columns=include_metadata_columns)
                conn.execute(
                    """
                    INSERT INTO messages (id, role, created_at)
                    VALUES ('u1', 'user', '2026-01-01 00:00:00')
                    """
                )
                conn.execute(
                    """
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('u1', 'content', 0, 'text/plain', 'prompt', 0)
                    """
                )
                if include_metadata_columns:
                    conn.execute(
                        """
                        INSERT INTO messages (
                            id, parent_id, role, model_ref, model_id, model_type,
                            model_display_name, input_tokens, output_tokens,
                            cache_read_tokens, cache_write_tokens, created_at
                        ) VALUES (
                            'a1', 'u1', 'assistant', 'glm', 'glm-5.1', 'zai',
                            'GLM 5.1', 11, 7, 3, 2, '2026-01-01 00:00:01'
                        )
                        """
                    )
                else:
                    conn.execute(
                        """
                        INSERT INTO messages (id, parent_id, role, created_at)
                        VALUES ('a1', 'u1', 'assistant', '2026-01-01 00:00:01')
                        """
                    )
                conn.execute(
                    """
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('a1', 'content', 0, 'text/plain', 'answer', 0)
                    """
                )
                if include_metadata_columns:
                    conn.execute(
                        """
                        INSERT INTO messages (id, role, created_at)
                        VALUES ('other', 'user', '2026-01-01 00:00:02')
                        """
                    )
                    conn.execute(
                        """
                        INSERT INTO blocks (
                            message_id, block_type, modality_type, mime_type, content,
                            sequence_order
                        ) VALUES ('other', 'content', 0, 'text/plain', 'unrelated', 0)
                        """
                    )
                    conn.execute(
                        """
                        INSERT INTO acp_sessions (
                            id, last_message_id, cwd, title, model_ref, thinking_level,
                            cost_usd, created_at
                        ) VALUES (
                            'session-1', 'a1', '/workspace', 'benchmark', 'glm', '',
                            0.42, '2026-01-01 00:00:03'
                        )
                        """
                    )
        return db_path

    def _write_compacted_conversation_db(self) -> Path:
        db_path = Path(tempfile.mkdtemp()) / ".cpeconvo"
        with contextlib.closing(sqlite3.connect(db_path)) as conn:
            with conn:
                self._create_schema(conn, include_metadata_columns=True)
                conn.executescript(
                    """
                    INSERT INTO messages (id, role, created_at)
                    VALUES ('u1', 'user', '2026-01-01 00:00:00');
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('u1', 'content', 0, 'text/plain', 'before compact', 0);
                    INSERT INTO messages (
                        id, parent_id, role, model_ref, model_id, model_type,
                        model_display_name, created_at
                    ) VALUES (
                        'a1', 'u1', 'assistant', 'glm', 'glm-5.1', 'zai',
                        'GLM 5.1', '2026-01-01 00:00:01'
                    );
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('a1', 'content', 0, 'text/plain', 'first answer', 0);
                    INSERT INTO messages (
                        id, compaction_parent_id, role, created_at
                    ) VALUES (
                        'u2', 'a1', 'user', '2026-01-01 00:00:02'
                    );
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('u2', 'content', 0, 'text/plain', 'compaction summary', 0);
                    INSERT INTO messages (
                        id, parent_id, role, model_ref, model_id, model_type,
                        model_display_name, created_at
                    ) VALUES (
                        'a2', 'u2', 'assistant', 'glm', 'glm-5.1', 'zai',
                        'GLM 5.1', '2026-01-01 00:00:03'
                    );
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('a2', 'content', 0, 'text/plain', 'after compact', 0);
                    INSERT INTO messages (id, role, created_at)
                    VALUES ('other', 'user', '2026-01-01 00:00:04');
                    INSERT INTO blocks (
                        message_id, block_type, modality_type, mime_type, content,
                        sequence_order
                    ) VALUES ('other', 'content', 0, 'text/plain', 'unrelated', 0);
                    INSERT INTO acp_sessions (
                        id, last_message_id, cwd, title, model_ref, thinking_level,
                        cost_usd, created_at
                    ) VALUES (
                        'session-1', 'a2', '/workspace', 'benchmark', 'glm', '',
                        0.42, '2026-01-01 00:00:05'
                    );
                    """
                )
        return db_path

    @staticmethod
    def _create_schema(
        conn: sqlite3.Connection, *, include_metadata_columns: bool
    ) -> None:
        metadata_columns = ""
        session_schema = ""
        if include_metadata_columns:
            metadata_columns = """
                message_extra_fields TEXT,
                model_ref TEXT,
                model_id TEXT,
                model_type TEXT,
                model_display_name TEXT,
                input_tokens INTEGER,
                output_tokens INTEGER,
                cache_read_tokens INTEGER,
                cache_write_tokens INTEGER,
            """
            session_schema = """
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
            """

        conn.executescript(
            f"""
            CREATE TABLE messages (
                id TEXT PRIMARY KEY,
                parent_id TEXT,
                compaction_parent_id TEXT,
                role TEXT NOT NULL,
                tool_result_error BOOLEAN NOT NULL DEFAULT 0,
                {metadata_columns}
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
            {session_schema}
            """
        )


if __name__ == "__main__":
    unittest.main()

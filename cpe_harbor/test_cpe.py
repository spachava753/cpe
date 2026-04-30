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
        self.assertEqual(trajectory.final_metrics.total_prompt_tokens, 11)
        self.assertEqual(trajectory.final_metrics.total_completion_tokens, 7)
        self.assertEqual(trajectory.final_metrics.total_cached_tokens, 3)
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
        self.assertIsNone(trajectory.final_metrics.extra)
        self.assertEqual(trajectory.final_metrics.total_steps, 2)

    @staticmethod
    def _agent() -> CPE:
        agent = object.__new__(CPE)
        agent._version = "test"
        agent.model_name = "zai/glm-5.1"
        return agent

    def _write_conversation_db(self, *, include_metadata_columns: bool) -> Path:
        db_path = Path(tempfile.mkdtemp()) / ".cpeconvo"
        with sqlite3.connect(db_path) as conn:
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
        return db_path

    @staticmethod
    def _create_schema(
        conn: sqlite3.Connection, *, include_metadata_columns: bool
    ) -> None:
        metadata_columns = ""
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
            """
        )


if __name__ == "__main__":
    unittest.main()

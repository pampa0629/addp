import json
import unittest

from services.runs import (
    ERROR_MESSAGE_MAX_LENGTH,
    STEP_FACTS_MAX_BYTES,
    attach_step_facts,
    complete_run_step,
    resume_agent_run,
    retry_agent_run,
    set_run_status,
    summarize_tool_result,
)


class AgentRunTests(unittest.TestCase):
    def test_tool_summary_does_not_persist_preview_rows(self):
        content = json.dumps(
            {
                "preview_type": "table",
                "data": {"rows": [{"secret_sample": "must-not-persist"}], "total": 166},
                "metadata": {"locator": "addp://engine/8/path/public/railway"},
            },
            ensure_ascii=False,
        )

        summary = summarize_tool_result(content)

        self.assertIn('"preview_type":"table"', summary)
        self.assertIn("result_size_bytes", summary)
        self.assertNotIn("secret_sample", summary)
        self.assertNotIn("locator", summary)

    def test_tool_summary_records_list_count_only(self):
        summary = summarize_tool_result('[{"id":1},{"id":2}]')

        self.assertEqual(json.loads(summary), {"item_count": 2})

    def test_tool_summary_does_not_persist_truncated_json_prefix(self):
        content = (
            '{"results":[{"locator":"addp://engine/8/path/public/railway",'
            '"secret_sample":"must-not-persist"}]'
            "\n...[结果过长已截断]"
        )

        summary = summarize_tool_result(content)

        self.assertEqual(
            json.loads(summary),
            {
                "value_type": "text",
                "result_size_bytes": len(content.encode("utf-8")),
            },
        )
        self.assertNotIn("locator", summary)
        self.assertNotIn("secret_sample", summary)


class _Result:
    def __init__(self, value):
        self.value = value

    def scalar_one_or_none(self):
        return self.value


class _DB:
    def __init__(self, run):
        self.run = run
        self.flush_count = 0

    async def execute(self, _statement):
        return _Result(self.run)

    async def flush(self):
        self.flush_count += 1


class _EntityDB:
    def __init__(self, entity):
        self.entity = entity
        self.flush_count = 0

    async def get(self, _model, _entity_id):
        return self.entity

    async def flush(self):
        self.flush_count += 1


class AgentRunLifecycleTests(unittest.IsolatedAsyncioTestCase):
    async def test_owner_approval_resumes_the_same_waiting_agent_run(self):
        import uuid

        from models.interaction import Interaction
        from models.run import AgentRun

        run = AgentRun(
            session_id=12,
            user_id=3,
            tenant_id=5,
            initial_protocol_run_id="initial-1",
            status="waiting",
            checkpoint={},
        )
        run.id = uuid.uuid4()
        interaction = Interaction(
            session_id=12,
            user_id=3,
            tenant_id=5,
            agent_run_id=run.id,
            kind="owner_approval",
            owner="develop",
            status="completed",
            prompt="需要审批",
            answer={
                "status": "approved",
                "approval_id": str(uuid.uuid4()),
                "request_fingerprint": "a" * 64,
            },
        )
        db = _DB(run)

        resumed = await resume_agent_run(
            db,
            interactions=[interaction],
            session_id=12,
            user_id=3,
            tenant_id=5,
        )

        self.assertIs(resumed, run)
        self.assertEqual(resumed.id, interaction.agent_run_id)
        self.assertEqual(resumed.status, "running")

    async def test_retry_reuses_failed_agent_run(self):
        from models.run import AgentRun

        run = AgentRun(
            session_id=12,
            user_id=3,
            tenant_id=5,
            initial_protocol_run_id="initial-1",
            status="failed",
            checkpoint={},
            error_source="client",
            error_code="client_disconnected",
            error_message="断线",
        )
        run.id = __import__("uuid").uuid4()
        db = _DB(run)

        retried = await retry_agent_run(
            db,
            agent_run_id=run.id,
            session_id=12,
            user_id=3,
            tenant_id=5,
        )

        self.assertIs(retried, run)
        self.assertEqual(run.status, "running")
        self.assertIsNone(run.error_source)
        self.assertIsNone(run.error_code)
        self.assertEqual(db.flush_count, 1)

    async def test_run_error_message_is_limited_at_persistence_boundary(self):
        from models.run import AgentRun

        run = AgentRun(
            session_id=12,
            user_id=3,
            tenant_id=5,
            initial_protocol_run_id="initial-1",
            status="running",
            checkpoint={},
        )
        run.id = __import__("uuid").uuid4()
        db = _EntityDB(run)

        await set_run_status(
            db,
            agent_run_id=run.id,
            status="failed",
            error_source="runtime",
            error_code="runtime_exception",
            error_message="x" * (ERROR_MESSAGE_MAX_LENGTH + 200),
        )

        self.assertEqual(len(run.error_message), ERROR_MESSAGE_MAX_LENGTH)
        self.assertEqual(db.flush_count, 1)

    async def test_step_error_message_is_limited_at_persistence_boundary(self):
        from models.run_step import AgentRunStep

        step = AgentRunStep(
            agent_run_id=__import__("uuid").uuid4(),
            sequence=1,
            step_type="tool_call",
            status="running",
            protocol_invocation_id="initial-1",
        )
        step.id = __import__("uuid").uuid4()
        db = _EntityDB(step)

        await complete_run_step(
            db,
            step_id=step.id,
            status="failed",
            error_source="owner",
            error_code="owner_api_error",
            error_message="x" * (ERROR_MESSAGE_MAX_LENGTH + 200),
        )

        self.assertEqual(len(step.error_message), ERROR_MESSAGE_MAX_LENGTH)
        self.assertEqual(db.flush_count, 1)

    async def test_step_facts_reject_total_payload_over_byte_limit(self):
        from models.run_step import AgentRunStep

        step = AgentRunStep(
            agent_run_id=__import__("uuid").uuid4(),
            sequence=1,
            step_type="tool_call",
            status="running",
            protocol_invocation_id="initial-1",
        )
        step.id = __import__("uuid").uuid4()
        db = _EntityDB(step)

        with self.assertRaisesRegex(ValueError, "step facts exceed"):
            await attach_step_facts(
                db,
                step_id=step.id,
                facts={"resources": [{"name": "x" * STEP_FACTS_MAX_BYTES}]},
            )

        self.assertEqual(db.flush_count, 0)


if __name__ == "__main__":
    unittest.main()

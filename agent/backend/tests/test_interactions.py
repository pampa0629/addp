import unittest
import uuid

from services.interactions import (
    InteractionAnswerError,
    InteractionNotFoundError,
    InteractionOwnerStateError,
    InteractionStateError,
    create_clarification,
    create_owner_approval,
    format_resume_display,
    format_resume_message,
    resolve_interaction,
)


class _Result:
    def __init__(self, value):
        self.value = value

    def scalar_one_or_none(self):
        return self.value


class _DB:
    def __init__(self, result=None):
        self.result = result
        self.added = []
        self.statement = None
        self.flush_count = 0

    def add(self, value):
        self.added.append(value)

    async def flush(self):
        self.flush_count += 1

    async def execute(self, statement):
        self.statement = statement
        return _Result(self.result)


class InteractionTests(unittest.IsolatedAsyncioTestCase):
    async def test_text_answer_resumes_without_inventing_candidate_facts(self):
        from models.interaction import Interaction

        interaction = Interaction(
            id=uuid.uuid4(), session_id=12, user_id=3, tenant_id=5,
            agent_run_id=uuid.uuid4(), kind="clarification", status="pending",
            prompt="请选择行粒度", options=[{"value": "unwind", "label": "展开数组"}],
        )
        resolved = await resolve_interaction(
            _DB(interaction), interaction_id=str(interaction.id),
            session_id=12, user_id=3, tenant_id=5,
            payload={"text": "  一文档一行，只投影标量字段。  "},
        )
        self.assertIs(resolved, interaction)
        self.assertEqual(resolved.answer, {"text": "一文档一行，只投影标量字段。"})
        self.assertEqual(resolved.status, "completed")
        self.assertEqual(format_resume_display(resolved), "一文档一行，只投影标量字段。")
        self.assertIn("请选择行粒度", format_resume_message(resolved))

    async def test_text_only_confirms_a_unique_exact_persisted_candidate(self):
        from models.interaction import Interaction

        candidates = [
            {"value": "addp://one", "label": "Outdoor.Outdoors", "candidate": {"name": "Outdoors", "locator": "addp://one"}},
            {"value": "addp://two", "label": "Outdoor.Photos", "candidate": {"name": "Photos", "locator": "addp://two"}},
        ]
        for text, expected in (("Outdoors", candidates[0]), ("outdoors", {"text": "outdoors"}),
                               ("随便选一个", {"text": "随便选一个"})):
            with self.subTest(text=text):
                interaction = Interaction(id=uuid.uuid4(), kind="clarification", status="pending", options=candidates)
                await resolve_interaction(_DB(interaction), interaction_id=str(interaction.id),
                                          session_id=12, user_id=3, tenant_id=5, payload={"text": text})
                self.assertEqual(interaction.answer, expected)
        candidates[1]["candidate"]["name"] = "Outdoors"
        interaction = Interaction(id=uuid.uuid4(), kind="clarification", status="pending", options=candidates)
        await resolve_interaction(_DB(interaction), interaction_id=str(interaction.id),
                                  session_id=12, user_id=3, tenant_id=5, payload={"text": "Outdoors"})
        self.assertEqual(interaction.answer, {"text": "Outdoors"})

    async def test_text_cannot_approve_a_review_or_an_owner_operation(self):
        from models.interaction import Interaction

        review = Interaction(id=uuid.uuid4(), kind="clarification", status="pending", options=[
            {"value": "fingerprint", "label": "确认创建", "candidate": {"operation_review": {"tool": "transfer.task.create"}}},
        ])
        for text in ("确认创建", "fingerprint"):
            review.status = "pending"
            await resolve_interaction(_DB(review), interaction_id=str(review.id), session_id=12,
                                      user_id=3, tenant_id=5, payload={"text": text})
            self.assertEqual(review.answer, {"text": text})
        owner = Interaction(id=uuid.uuid4(), kind="owner_approval", status="pending")
        with self.assertRaises(InteractionAnswerError):
            await resolve_interaction(_DB(owner), interaction_id=str(owner.id), session_id=12,
                                      user_id=3, tenant_id=5, payload={"text": "批准"})
        self.assertEqual(owner.status, "pending")

    async def test_text_shape_and_length_are_strict_and_rejection_does_not_consume(self):
        from models.interaction import Interaction

        for payload in ({"text": ""}, {"text": "  "}, {"text": 1}, {"text": "x" * 2001},
                        {"text": "Outdoors", "candidate": {"locator": "addp://invented"}},
                        {"text": "确认创建", "value": "fingerprint"}):
            with self.subTest(payload_keys=list(payload)):
                interaction = Interaction(id=uuid.uuid4(), kind="clarification", status="pending", options=[])
                with self.assertRaises(InteractionAnswerError):
                    await resolve_interaction(_DB(interaction), interaction_id=str(interaction.id),
                                              session_id=12, user_id=3, tenant_id=5, payload=payload)
                self.assertEqual(interaction.status, "pending")

    async def test_text_at_limit_is_accepted_and_duplicate_submission_is_rejected(self):
        from models.interaction import Interaction

        interaction = Interaction(id=uuid.uuid4(), kind="clarification", status="pending", options=[])
        db = _DB(interaction)
        arguments = dict(interaction_id=str(interaction.id), session_id=12, user_id=3, tenant_id=5,
                         payload={"text": "字" * 2000})
        await resolve_interaction(db, **arguments)
        self.assertEqual(interaction.answer, {"text": "字" * 2000})
        with self.assertRaises(InteractionStateError):
            await resolve_interaction(db, **arguments)

    async def test_clarification_persists_recoverable_options(self):
        db = _DB()
        agent_run_id = uuid.uuid4()
        interaction = await create_clarification(
            db,
            session_id=12,
            user_id=3,
            tenant_id=5,
            agent_run_id=agent_run_id,
            tool_call_id="tool-1",
            prompt="请选择数据源",
            candidates=[{"name": "railway", "locator": "locator-1"}],
        )

        self.assertEqual(db.added, [interaction])
        self.assertEqual(db.flush_count, 1)
        self.assertEqual(interaction.agent_run_id, agent_run_id)
        self.assertEqual(interaction.status, "pending")
        self.assertEqual(interaction.options[0]["value"], "locator-1")
        choice_schema, text_schema = interaction.response_schema["oneOf"]
        self.assertEqual(choice_schema["required"], ["value", "label"])
        self.assertEqual(choice_schema["not"], {"required": ["text"]})
        self.assertEqual(text_schema["required"], ["text"])
        self.assertFalse(text_schema["additionalProperties"])
        self.assertEqual(text_schema["properties"]["text"]["maxLength"], 2000)

    async def test_clarification_preserves_runtime_option_value_and_candidate_fact(self):
        db = _DB()
        agent_run_id = uuid.uuid4()
        interaction = await create_clarification(
            db,
            session_id=12,
            user_id=3,
            tenant_id=5,
            agent_run_id=agent_run_id,
            tool_call_id="clarify-1",
            prompt="请选择工作流运行时",
            candidates=[
                {
                    "label": "GeoPython Workflow",
                    "value": 20,
                    "candidate": {"id": 20, "engine_type": "geopython_workflow"},
                }
            ],
        )

        self.assertEqual(interaction.options[0]["value"], 20)
        self.assertEqual(
            interaction.options[0]["candidate"],
            {"id": 20, "engine_type": "geopython_workflow"},
        )

    async def test_resume_completes_pending_interaction(self):
        from models.interaction import Interaction

        interaction = Interaction(
            id=uuid.uuid4(),
            session_id=12,
            user_id=3,
            tenant_id=5,
            agent_run_id=uuid.uuid4(),
            status="pending",
            prompt="请选择数据源",
            options=[
                {
                    "value": "locator-1",
                    "label": "railway",
                    "candidate": {"locator": "locator-1"},
                }
            ],
        )
        db = _DB(interaction)
        payload = {"value": "locator-1", "label": "railway"}

        resolved = await resolve_interaction(
            db,
            interaction_id=str(interaction.id),
            session_id=12,
            user_id=3,
            tenant_id=5,
            payload=payload,
        )

        self.assertIs(resolved, interaction)
        self.assertEqual(interaction.status, "completed")
        self.assertEqual(
            interaction.answer,
            {"value": "locator-1", "label": "railway", "candidate": {"locator": "locator-1"}},
        )
        self.assertIsNotNone(interaction.completed_at)
        self.assertEqual(
            format_resume_message(interaction),
            "用户已完成澄清，选择：railway；候选事实：{'locator': 'locator-1'}",
        )
        self.assertEqual(db.flush_count, 1)

    async def test_resume_query_is_scoped_to_session_user_and_tenant(self):
        interaction_id = uuid.uuid4()
        db = _DB()

        with self.assertRaises(InteractionNotFoundError):
            await resolve_interaction(
                db,
                interaction_id=str(interaction_id),
                session_id=12,
                user_id=3,
                tenant_id=5,
                payload={"value": "locator-1", "label": "railway"},
            )

        statement_text = str(db.statement)
        self.assertIn("interactions.session_id", statement_text)
        self.assertIn("interactions.user_id", statement_text)
        self.assertIn("interactions.tenant_id", statement_text)
        self.assertIn("FOR UPDATE", statement_text)

    async def test_completed_interaction_cannot_be_resumed_again(self):
        from models.interaction import Interaction

        interaction = Interaction(
            id=uuid.uuid4(),
            session_id=12,
            user_id=3,
            tenant_id=5,
            agent_run_id=uuid.uuid4(),
            status="completed",
            prompt="请选择数据源",
        )

        with self.assertRaises(InteractionStateError):
            await resolve_interaction(
                _DB(interaction),
                interaction_id=str(interaction.id),
                session_id=12,
                user_id=3,
                tenant_id=5,
                payload={"value": "locator-1", "label": "railway"},
            )

    async def test_resume_rejects_value_outside_persisted_options(self):
        from models.interaction import Interaction

        interaction = Interaction(
            id=uuid.uuid4(),
            session_id=12,
            user_id=3,
            tenant_id=5,
            agent_run_id=uuid.uuid4(),
            status="pending",
            prompt="请选择数据源",
            options=[{"value": "locator-1", "label": "railway", "candidate": {"locator": "locator-1"}}],
        )

        with self.assertRaises(InteractionAnswerError):
            await resolve_interaction(
                _DB(interaction),
                interaction_id=str(interaction.id),
                session_id=12,
                user_id=3,
                tenant_id=5,
                payload={"value": "invented-locator", "label": "invented"},
            )

    async def test_owner_approval_check_only_completes_after_owner_approved(self):
        from models.interaction import Interaction

        interaction = Interaction(
            id=uuid.uuid4(),
            session_id=12,
            user_id=3,
            tenant_id=5,
            agent_run_id=uuid.uuid4(),
            kind="owner_approval",
            owner="develop",
            owner_interaction_id=str(uuid.uuid4()),
            owner_request_fingerprint="a" * 64,
            open_url="/develop/approvals/owner-1",
            request_summary={"task_count": 2},
            status="pending",
            prompt="需要审批",
            options=[],
        )

        async def pending_loader(_approval_id, _token):
            return {"status": "pending", "request_fingerprint": "a" * 64}

        with self.assertRaises(InteractionAnswerError):
            await resolve_interaction(
                _DB(interaction),
                interaction_id=str(interaction.id),
                session_id=12,
                user_id=3,
                tenant_id=5,
                payload={"action": "check", "approved": True},
                source_token="user-token",
                owner_approval_loader=pending_loader,
            )

        with self.assertRaises(InteractionOwnerStateError):
            await resolve_interaction(
                _DB(interaction),
                interaction_id=str(interaction.id),
                session_id=12,
                user_id=3,
                tenant_id=5,
                payload={"action": "check"},
                source_token="user-token",
                owner_approval_loader=pending_loader,
            )
        self.assertEqual(interaction.status, "pending")

        async def approved_loader(approval_id, token):
            self.assertEqual(approval_id, interaction.owner_interaction_id)
            self.assertEqual(token, "user-token")
            return {"status": "approved", "request_fingerprint": "a" * 64}

        db = _DB(interaction)
        resolved = await resolve_interaction(
            db,
            interaction_id=str(interaction.id),
            session_id=12,
            user_id=3,
            tenant_id=5,
            payload={"action": "check"},
            source_token="user-token",
            owner_approval_loader=approved_loader,
        )
        self.assertIs(resolved, interaction)
        self.assertEqual(interaction.status, "completed")
        self.assertEqual(interaction.answer["approval_id"], interaction.owner_interaction_id)
        self.assertIn("只提交 approval_id=", format_resume_message(interaction))

    async def test_owner_approval_projection_does_not_store_workflow_payload(self):
        db = _DB()
        interaction = await create_owner_approval(
            db,
            session_id=12,
            user_id=3,
            tenant_id=5,
            agent_run_id=uuid.uuid4(),
            tool_call_id="workflow-call",
            owner="develop",
            owner_interaction_id=str(uuid.uuid4()),
            open_url="/develop/approvals/owner-1",
            request_fingerprint="b" * 64,
            request_summary={"workflow_engine_id": 20, "task_count": 2},
            expires_at=None,
        )

        self.assertEqual(interaction.kind, "owner_approval")
        self.assertEqual(interaction.request_summary["task_count"], 2)
        self.assertFalse(hasattr(interaction, "request_payload"))
        self.assertEqual(interaction.response_schema["properties"]["action"], {"const": "check"})


if __name__ == "__main__":
    unittest.main()

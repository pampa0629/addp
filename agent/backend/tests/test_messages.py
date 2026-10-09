import unittest
import uuid
from datetime import datetime, timezone
from unittest.mock import AsyncMock, Mock, patch

from ag_ui.core import RunAgentInput
from starlette.requests import Request

from api.chat import _save_user_input, get_messages
from models.interaction import Interaction
from models.message import Message
from models.session import Session
from services.interactions import format_resume_message

from services.messages import MESSAGE_PARTS_MAX_BYTES, bounded_message_parts


class MessagePersistenceTests(unittest.TestCase):
    def test_message_parts_are_copied_before_persistence(self):
        source = [{"type": "text", "text": "hello"}]

        bounded = bounded_message_parts(source)
        source[0]["text"] = "changed"

        self.assertEqual(bounded[0]["text"], "hello")

    def test_message_parts_reject_total_payload_over_byte_limit(self):
        with self.assertRaisesRegex(ValueError, "message parts exceed"):
            bounded_message_parts([{"type": "text", "text": "x" * MESSAGE_PARTS_MAX_BYTES}])


class ClarificationDisplayTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.interaction = Interaction(
            id=uuid.uuid4(), session_id=12, user_id=3, tenant_id=5,
            agent_run_id=uuid.uuid4(), kind="clarification", status="completed",
            prompt="请选择", answer={
                "label": "确认创建", "value": "review-fingerprint",
                "candidate": {"operation_review": {"digest": "ontology-digest", "revision": 3}},
            },
        )

    async def test_resume_persists_label_without_losing_model_context_or_answer(self):
        db = AsyncMock()
        saved = []
        db.add = saved.append
        body = RunAgentInput.model_validate({
            "threadId": "12", "runId": "resume-display", "state": {},
            "messages": [], "tools": [], "context": [], "forwardedProps": {},
            "resume": [{"interruptId": str(self.interaction.id), "status": "resolved",
                        "payload": {"value": "review-fingerprint", "label": "伪造名称"}}],
        })
        with patch("api.chat.resolve_interaction", return_value=self.interaction):
            resolved = await _save_user_input(
                db, body=body, session=Session(id=12), user_id=3, tenant_id=5,
                source_token="test-token",
            )
        self.assertEqual(resolved, [self.interaction])
        self.assertEqual(saved[0].parts, [{"type": "text", "text": "确认创建"}])
        self.assertEqual(saved[0].content, format_resume_message(self.interaction))
        self.assertIn("ontology-digest", saved[0].content)
        self.assertEqual(self.interaction.answer["value"], "review-fingerprint")

    async def test_history_rebuilds_reply_from_owned_interaction_not_stored_internal_text(self):
        original = format_resume_message(self.interaction)
        message = Message(
            id=1, session_id=12, role="user", content=original,
            protocol_message_id=f"resume:{self.interaction.id}",
            parts=[{"type": "text", "text": original}], created_at=datetime.now(timezone.utc),
        )
        ordinary = Message(
            id=2, session_id=12, role="user", content="请解释 digest 的含义",
            protocol_message_id="user-2", parts=[{"type": "text", "text": "请解释 digest 的含义"}],
            created_at=datetime.now(timezone.utc),
        )
        messages = Mock()
        messages.scalars.return_value.all.return_value = [message, ordinary]
        interactions = Mock()
        interactions.scalars.return_value.all.return_value = [self.interaction]
        db = AsyncMock()
        db.execute.side_effect = [messages, interactions]
        request = Request({"type": "http", "headers": []})
        request.state.principal_id = 3
        request.state.tenant_id = 5
        with patch("api.chat._get_owned_session", return_value=Session(id=12)):
            result = await get_messages(12, request, db)
        self.assertEqual(result[0]["content"], "确认创建")
        self.assertEqual(result[0]["parts"], [{"type": "text", "text": "确认创建"}])
        self.assertEqual(result[1]["parts"], ordinary.parts)
        self.assertEqual(message.content, original)
        self.assertEqual(message.parts[0]["text"], original)
        query = str(db.execute.call_args_list[1].args[0])
        for scope in ("session_id", "user_id", "tenant_id"):
            self.assertIn(f"interactions.{scope}", query)


if __name__ == "__main__":
    unittest.main()

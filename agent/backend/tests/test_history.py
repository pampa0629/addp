import json
import unittest
from datetime import datetime
from unittest.mock import patch
from uuid import uuid4

from fastapi import HTTPException
from sqlalchemy import JSON, MetaData, create_engine
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import Session as DatabaseSession

from api.chat import get_message_index, get_messages, get_workspace
from config import settings
from models.interaction import Interaction
from models.message import Message
from models.run import AgentRun
from models.session import Session
from tests.test_session_tenant_isolation import AsyncDatabaseAdapter, request_for


class ConversationHistoryTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.engine = create_engine("sqlite:///:memory:")
        metadata = MetaData()
        for model in (Session, Message, AgentRun, Interaction):
            table = model.__table__.to_metadata(metadata)
            for column in table.columns:
                if isinstance(column.type, JSONB):
                    column.type = JSON()
        with self.engine.begin() as connection:
            connection.exec_driver_sql("ATTACH DATABASE ':memory:' AS agent")
            metadata.create_all(connection)
        self.database = DatabaseSession(self.engine)
        self.database.add_all([Session(id=1, user_id=7, tenant_id=10), Session(id=2, user_id=8, tenant_id=10)])
        self.database.flush()
        self.stamp = datetime(2026, 11, 7)
        self.database.add_all(Message(id=i, session_id=1, role="user" if i == 1 else "assistant",
                                      content=f"message {i}", parts=[], created_at=self.stamp) for i in range(1, 66))
        self.database.add(Message(id=100, session_id=2, role="user", content="foreign", parts=[], created_at=self.stamp))
        self.database.commit()
        self.db = AsyncDatabaseAdapter(self.database)
        self.request = request_for(7, 10)
        self.signing = patch.object(settings, "AGENT_SERVICE_CLIENT_SECRET", "isolated-history-test-secret")
        self.signing.start()
        self.addCleanup(self.signing.stop)

    def tearDown(self):
        self.database.close()
        self.engine.dispose()

    async def test_keyset_keeps_ties_and_upper_bound_while_messages_are_appended(self):
        first = await get_messages(1, self.request, self.db)
        self.assertEqual([m["id"] for m in first["data"]], list(range(46, 66)))
        self.assertIsNone(first["page"]["newer_cursor"])
        self.database.add(Message(id=66, session_id=1, role="user", content="new message", parts=[], created_at=self.stamp))
        self.database.commit()
        older = await get_messages(1, self.request, self.db, cursor=first["page"]["older_cursor"])
        self.assertEqual([m["id"] for m in older["data"]], list(range(26, 46)))
        newer = await get_messages(1, self.request, self.db, cursor=older["page"]["newer_cursor"])
        self.assertEqual([m["id"] for m in newer["data"]], list(range(46, 66)))
        latest = await get_messages(1, self.request, self.db)
        self.assertEqual(latest["data"][-1]["id"], 66)

    async def test_anchor_and_cursor_are_owned_and_integrity_protected(self):
        first = await get_messages(1, self.request, self.db)
        cursor = first["page"]["older_cursor"]
        anchored = await get_messages(1, self.request, self.db, limit=10, anchor_message_id=32)
        self.assertEqual([m["id"] for m in anchored["data"]], list(range(27, 37)))
        attempts = [
            (400, lambda: get_messages(1, self.request, self.db, cursor=cursor, anchor_message_id=32)),
            (400, lambda: get_messages(1, self.request, self.db, cursor="invalid")),
            (400, lambda: get_messages(1, self.request, self.db, cursor="")),
            (400, lambda: get_messages(1, self.request, self.db, cursor=cursor[:8] + "A" + cursor[9:])),
            (400, lambda: get_message_index(1, self.request, "results", self.db, cursor=cursor)),
            (404, lambda: get_messages(2, self.request, self.db, cursor=cursor)),
            (404, lambda: get_messages(1, self.request, self.db, anchor_message_id=100)),
            (404, lambda: get_workspace(1, request_for(7, 20), self.db)),
        ]
        for code, attempt in attempts:
            with self.assertRaises(HTTPException) as error:
                await attempt()
            self.assertEqual(error.exception.status_code, code)

    async def test_old_references_are_readable_without_their_payload_or_latest_window(self):
        message = self.database.get(Message, 3)
        message.parts = [{
            "type": "presentation_ref", "protocol": "a2ui", "surface_id": "old-draft",
            "content": {"operations": [{"updateComponents": {"components": [{
                "component": "TransferDraftSnapshot", "name": "historical draft",
                "parameters": json.dumps({"config": {"source": {"locator": "addp://engine/11/path/demo?type=collection"},
                                                     "target": {"parent_locator": "addp://engine/2/path/public?type=schema"}}}),
                "private": "must-not-reach-index",
            }]}}]},
        }]
        self.database.commit()
        page = await get_message_index(1, self.request, "resources", self.db)
        seen = []
        while True:
            seen.extend(page["data"])
            if not page["page"]["older_cursor"]:
                break
            page = await get_message_index(1, self.request, "resources", self.db, cursor=page["page"]["older_cursor"])
        entry = next(row for row in seen if row["message_id"] == 3)
        self.assertEqual(len(entry["entries"]), 2)
        self.assertNotIn("must-not-reach-index", json.dumps(seen))
        self.assertNotIn("parameters", json.dumps(seen))
        self.assertEqual((await get_workspace(1, self.request, self.db))["goal"], "message 1")
        draft = await get_messages(1, self.request, self.db, anchor_message_id=3)
        self.assertIn("must-not-reach-index", json.dumps(draft["data"]))

    async def test_pending_counts_do_not_depend_on_the_loaded_message_window(self):
        run_id, interaction_id = uuid4(), uuid4()
        self.database.add(AgentRun(id=run_id, session_id=1, user_id=7, tenant_id=10, initial_protocol_run_id="history"))
        self.database.flush()
        pending = Interaction(id=interaction_id, session_id=1, user_id=7, tenant_id=10, agent_run_id=run_id,
                              kind="clarification", status="pending", prompt="select", options=[], request_summary={})
        self.database.add(pending)
        self.database.get(Message, 2).parts = [{"type": "interaction_ref", "interaction_id": str(interaction_id),
                                               "kind": "clarification", "status": "pending"}]
        self.database.commit()
        self.assertNotIn(2, [m["id"] for m in (await get_messages(1, self.request, self.db))["data"]])
        context = await get_workspace(1, self.request, self.db)
        self.assertEqual((context["pending_clarifications"], context["clarification_id"]), (1, str(interaction_id)))
        pending.status = "completed"
        self.database.commit()
        context = await get_workspace(1, self.request, self.db)
        self.assertEqual((context["pending_clarifications"], context["clarification_id"]), (0, None))
        history = await get_messages(1, self.request, self.db, anchor_message_id=2)
        row = next(m for m in history["data"] if m["id"] == 2)
        self.assertEqual(row["parts"][0]["status"], "completed")

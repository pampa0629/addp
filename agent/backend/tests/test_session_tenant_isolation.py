import unittest

from fastapi import HTTPException
from sqlalchemy import create_engine
from sqlalchemy.orm import Session as DatabaseSession
from starlette.requests import Request

from ag_ui.core import RunAgentInput

from api.chat import _get_owned_session, chat, get_messages
from api.sessions import delete_session, get_session, list_sessions
from models.session import Session


class AsyncDatabaseAdapter:
    def __init__(self, database):
        self.database = database

    async def execute(self, statement):
        return self.database.execute(statement)

    async def delete(self, entity):
        self.database.delete(entity)

    async def commit(self):
        self.database.commit()


def request_for(user_id, tenant_id):
    request = Request({"type": "http", "method": "GET", "path": "/", "headers": []})
    request.state.principal_id = user_id
    request.state.tenant_id = tenant_id
    return request


class AgentSessionTenantIsolationTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.engine = create_engine("sqlite:///:memory:")
        with self.engine.begin() as connection:
            connection.exec_driver_sql("ATTACH DATABASE ':memory:' AS agent")
            Session.__table__.create(connection)
        self.database = DatabaseSession(self.engine)
        own = Session(user_id=7, tenant_id=10, title="current tenant")
        other_tenant = Session(user_id=7, tenant_id=20, title="other tenant")
        other_user = Session(user_id=8, tenant_id=10, title="other user")
        self.database.add_all([own, other_tenant, other_user])
        self.database.commit()
        self.own_id = own.id
        self.other_tenant_id = other_tenant.id
        self.other_user_id = other_user.id
        self.db = AsyncDatabaseAdapter(self.database)
        self.request = request_for(user_id=7, tenant_id=10)

    def tearDown(self):
        self.database.close()
        self.engine.dispose()

    async def test_list_only_returns_current_tenant_and_user(self):
        sessions = await list_sessions(self.request, self.db)

        self.assertEqual([session["id"] for session in sessions], [self.own_id])

    async def test_detail_message_and_delete_hide_other_tenant(self):
        own = await get_session(self.own_id, self.request, self.db)
        self.assertEqual(own["id"], self.own_id)

        for action in (
            lambda: get_session(self.other_tenant_id, self.request, self.db),
            lambda: delete_session(self.other_tenant_id, self.request, self.db),
            lambda: get_messages(self.other_tenant_id, self.request, self.db),
            lambda: _get_owned_session(self.db, self.other_tenant_id, 7, 10),
            lambda: get_session(self.other_user_id, self.request, self.db),
        ):
            with self.assertRaises(HTTPException) as denied:
                await action()
            self.assertEqual(denied.exception.status_code, 404)

        self.assertIsNotNone(self.database.get(Session, self.other_tenant_id))

    async def test_chat_rejects_another_tenants_session_before_starting_a_run(self):
        body = RunAgentInput.model_validate({
            "threadId": str(self.other_tenant_id),
            "runId": "cross-tenant-attempt",
            "state": {},
            "messages": [],
            "tools": [],
            "context": [],
            "forwardedProps": {},
        })

        with self.assertRaises(HTTPException) as denied:
            await chat(self.request, body, self.db)

        self.assertEqual(denied.exception.status_code, 404)

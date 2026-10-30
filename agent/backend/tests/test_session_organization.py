import json
import unittest
from unittest.mock import AsyncMock, patch

from fastapi import HTTPException
from sqlalchemy import create_engine
from sqlalchemy.orm import Session as DatabaseSession
from ag_ui.core import RunAgentInput

from api.sessions import SessionUpdate, VersionInput, list_sessions, update_session, delete_session, get_session, list_skills
from api.chat import chat
from models.session import Session
from tests.test_session_tenant_isolation import AsyncDatabaseAdapter, request_for


class DatabaseAdapter(AsyncDatabaseAdapter):
    async def refresh(self, value):
        self.database.refresh(value)


class SessionOrganizationTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.engine = create_engine('sqlite:///:memory:')
        with self.engine.begin() as conn:
            conn.exec_driver_sql("ATTACH DATABASE ':memory:' AS agent")
            Session.__table__.create(conn)
            conn.exec_driver_sql('CREATE TABLE agent.messages (id INTEGER PRIMARY KEY, session_id INTEGER, role TEXT, content TEXT, protocol_message_id TEXT, parts JSON, created_at DATETIME)')
        self.database = DatabaseSession(self.engine)
        self.database.add_all([Session(id=1, user_id=7, tenant_id=10, title='100%_literal'),
                               Session(id=2, user_id=7, tenant_id=10, title='100xxliteral', pinned=True),
                               Session(id=3, user_id=7, tenant_id=10, title='archived', archived=True),
                               Session(id=4, user_id=7, tenant_id=20, title='foreign')])
        self.database.commit()
        self.db = DatabaseAdapter(self.database)
        self.request = request_for(7, 10)

    def tearDown(self):
        self.database.close(); self.engine.dispose()

    async def test_filter_before_count_pin_sort_and_bounded_paging(self):
        page = await list_sessions(self.request, self.db, page_size=1)
        self.assertEqual(page['total'], 2)
        self.assertEqual(page['data'][0]['id'], 2)
        literal = await list_sessions(self.request, self.db, search='%_')
        self.assertEqual([row['id'] for row in literal['data']], [1])
        archived = await list_sessions(self.request, self.db, archived=True)
        self.assertEqual([row['id'] for row in archived['data']], [3])
        self.assertTrue((await get_session(3, self.request, self.db))['archived'])

    async def test_organization_version_conflict_preserves_all_fields_and_delete(self):
        updated = await update_session(1, self.request, SessionUpdate(version=1, title='renamed', pinned=True, archived=True), self.db)
        self.assertEqual((updated['version'], updated['title'], updated['pinned'], updated['archived']), (2, 'renamed', True, True))
        conflict = await update_session(1, self.request, SessionUpdate(version=1, title='overwrite'), self.db)
        self.assertEqual(json.loads(conflict.body)['error_code'], 'resource_version_conflict')
        deletion = await delete_session(1, self.request, VersionInput(version=1), self.db)
        self.assertEqual(deletion.status_code, 409)
        current = await get_session(1, self.request, self.db)
        self.assertEqual(current, updated)
        restored = await update_session(1, self.request, SessionUpdate(version=2, archived=False), self.db)
        self.assertFalse(restored['archived']); self.assertEqual(restored['version'], 3)
        await delete_session(1, self.request, VersionInput(version=3), self.db)
        self.assertIsNone(self.database.get(Session, 1))

    async def test_cross_tenant_mutations_hide_resource_even_with_known_version(self):
        for action in (lambda: update_session(4, self.request, SessionUpdate(version=1, archived=True), self.db),
                       lambda: delete_session(4, self.request, VersionInput(version=1), self.db)):
            with self.assertRaises(HTTPException) as denied:
                await action()
            self.assertEqual(denied.exception.status_code, 404)

    async def test_archived_admission_blocks_new_input_resume_and_retry_before_side_effects(self):
        for payload in ({'messages': [{'id': 'm', 'role': 'user', 'content': 'hello'}]},
                        {'resume': [{'interruptId': 'i', 'status': 'resolved', 'payload': {'text': 'yes'}}]}, {}):
            body = RunAgentInput.model_validate(dict(threadId='3', runId='archived', state={}, messages=[], tools=[], context=[], forwardedProps={}, **{k:v for k,v in payload.items() if k != 'messages'}))
            if 'messages' in payload:
                body = body.model_copy(update={'messages': RunAgentInput.model_validate(dict(threadId='3', runId='archived', state={}, tools=[], context=[], forwardedProps={}, **payload)).messages})
            with patch('api.chat._save_user_input', new=AsyncMock()) as save, patch('api.chat.retry_agent_run', new=AsyncMock()) as retry:
                result = await chat(self.request, body, self.db)
            self.assertEqual(result.status_code, 409)
            self.assertEqual(json.loads(result.body)['error_code'], 'session_archived')
            save.assert_not_awaited(); retry.assert_not_awaited()

    async def test_archive_during_owner_read_is_rechecked_before_interaction_mutation(self):
        from types import SimpleNamespace
        body=RunAgentInput.model_validate(dict(threadId='1',runId='race',state={},messages=[],tools=[],context=[],forwardedProps={},
            resume=[{'interruptId':'approval','status':'resolved','payload':{'action':'check'}}]))
        self.request.state.token='token'
        order=[]
        async def owned(*args,**kwargs):
            order.append('lock' if kwargs.get('lock') else 'read')
            return SimpleNamespace(archived=bool(kwargs.get('lock')))
        async def prefetch(*args): order.append('owner_read'); return AsyncMock()
        with patch('api.chat._get_owned_session',new=owned), patch('api.chat._prefetch_owner_approvals',new=prefetch), patch('api.chat._save_user_input',new=AsyncMock()) as save:
            result=await chat(self.request,body,self.db)
        self.assertEqual(result.status_code,409)
        self.assertEqual(order,['read','owner_read','lock'])
        save.assert_not_awaited()

    async def test_skill_discovery_is_the_runtime_registry(self):
        from agents.main_agent import _get_skill_registry
        skills = await list_skills()
        self.assertEqual({item['name'] for item in skills}, set(_get_skill_registry()))

    def test_updates_refuse_empty_null_boolean_versions_and_unknown_fields(self):
        from pydantic import ValidationError
        for arguments in ({'version': 1}, {'version': True, 'title': 'x'}, {'version': 1, 'title': ' '},
                          {'version': 1, 'archived': None}, {'version': 1, 'archived': True, 'tenant_id': 1}):
            with self.assertRaises(ValidationError):
                SessionUpdate.model_validate(arguments)

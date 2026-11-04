import unittest
from contextlib import asynccontextmanager
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from sqlalchemy import JSON, MetaData, create_engine
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import Session as DatabaseSession

from agents.context import HISTORY_MESSAGE_LIMIT
from api.chat import _load_agent_history
from models.message import Message
from models.session import Session
from tests.test_session_tenant_isolation import AsyncDatabaseAdapter
from utils.summary import _select_incremental_messages, maybe_update_summary


class IncrementalSummaryTests(unittest.TestCase):
    def test_watermark_advances_without_recompressing_previous_messages(self):
        first_batch = [SimpleNamespace(id=index) for index in range(1, HISTORY_MESSAGE_LIMIT + 6)]

        first_messages, first_watermark = _select_incremental_messages(first_batch)

        self.assertEqual([message.id for message in first_messages], [1, 2, 3, 4, 5])
        self.assertEqual(first_watermark, 5)

        second_batch = [
            SimpleNamespace(id=index)
            for index in range(first_watermark + 1, HISTORY_MESSAGE_LIMIT + 8)
        ]
        second_messages, second_watermark = _select_incremental_messages(second_batch)

        self.assertEqual([message.id for message in second_messages], [6, 7])
        self.assertEqual(second_watermark, 7)

    def test_does_not_advance_watermark_inside_recent_window(self):
        recent_messages = [
            SimpleNamespace(id=index)
            for index in range(1, HISTORY_MESSAGE_LIMIT + 1)
        ]

        messages, watermark = _select_incremental_messages(recent_messages)

        self.assertEqual(messages, [])
        self.assertIsNone(watermark)


class SummaryPersistenceTests(unittest.IsolatedAsyncioTestCase):
    """Real SQL predicates/commits in T1 SQLite; not PostgreSQL locking evidence."""

    def setUp(self):
        self.engine = create_engine('sqlite:///:memory:')
        metadata = MetaData()
        for model in (Session, Message):
            table = model.__table__.to_metadata(metadata)
            for column in table.columns:
                if isinstance(column.type, JSONB):
                    column.type = JSON()
        with self.engine.begin() as connection:
            connection.exec_driver_sql("ATTACH DATABASE ':memory:' AS agent")
            metadata.create_all(connection)
        with DatabaseSession(self.engine) as database:
            database.add_all([
                Session(id=1, user_id=7, tenant_id=10, title='summary', summary='existing goal'),
                Session(id=2, user_id=7, tenant_id=20, title='other tenant'),
            ])
            database.flush()
            database.add_all([Message(id=index, session_id=1, role='user' if index % 2 else 'assistant',
                content=f'complete-message-{index}' if index != 6 else 'cancelled-private-prefix',
                is_complete=index != 6) for index in range(1, 27)])
            database.add(Message(id=100, session_id=2, role='user', content='foreign-session-secret'))
            database.commit()

    def tearDown(self):
        self.engine.dispose()

    @asynccontextmanager
    async def database(self):
        class Adapter(AsyncDatabaseAdapter):
            async def get(self, model, identity):
                return self.database.get(model, identity)

            @asynccontextmanager
            async def begin(self):
                with self.database.begin():
                    yield

        with DatabaseSession(self.engine) as database:
            yield Adapter(database)

    def persisted(self):
        with DatabaseSession(self.engine) as database:
            session = database.get(Session, 1)
            return session.summary, session.summary_message_id

    async def test_complete_messages_only_and_incremental_watermark_commit_together(self):
        model = SimpleNamespace(ainvoke=AsyncMock(return_value=SimpleNamespace(content='merged goal')))
        with patch('utils.summary.AsyncSessionLocal', new=self.database), \
             patch('utils.summary.get_llm', return_value=model) as get_llm:
            await maybe_update_summary(1)
            self.assertEqual(self.persisted(), ('merged goal', 5))
            prompt = model.ainvoke.call_args.args[0][0].content
            self.assertIn('existing goal', prompt)
            for index in range(1, 6):
                self.assertIn(f'complete-message-{index}\n', prompt)
            for hidden in ('cancelled-private-prefix', 'foreign-session-secret', 'complete-message-7'):
                self.assertNotIn(hidden, prompt)
            get_llm.assert_called_once_with(10, 'general-chat')
            with DatabaseSession(self.engine) as database:
                database.add_all([Message(id=index, session_id=1, role='user', content=f'complete-message-{index}')
                                  for index in (27, 28)])
                database.commit()
            await maybe_update_summary(1)
            self.assertEqual(self.persisted(), ('merged goal', 8))
            prompt = model.ainvoke.call_args.args[0][0].content
            self.assertIn('merged goal', prompt)
            self.assertIn('complete-message-7\n', prompt)
            self.assertIn('complete-message-8\n', prompt)
            self.assertNotIn('complete-message-1\n', prompt)
            self.assertNotIn('cancelled-private-prefix', prompt)

    async def test_empty_or_failed_model_preserves_summary_and_watermark_without_private_logs(self):
        for result in (SimpleNamespace(content='  '), RuntimeError('private-provider-error')):
            model = SimpleNamespace(ainvoke=AsyncMock())
            if isinstance(result, Exception):
                model.ainvoke.side_effect = result
            else:
                model.ainvoke.return_value = result
            with patch('utils.summary.AsyncSessionLocal', new=self.database), \
                 patch('utils.summary.get_llm', return_value=model), \
                 self.assertLogs('agent.summary') as logs:
                await maybe_update_summary(1)
            self.assertEqual(self.persisted(), ('existing goal', None))
            self.assertNotIn('private-provider-error', str(logs.output))

    async def test_older_inflight_result_does_not_overwrite_newer_committed_summary(self):
        async def newer_result(_messages):
            with DatabaseSession(self.engine) as database:
                session = database.get(Session, 1)
                session.summary, session.summary_message_id = 'newer committed goal', 8
                database.commit()
            return SimpleNamespace(content='older inflight goal')
        model = SimpleNamespace(ainvoke=AsyncMock(side_effect=newer_result))
        with patch('utils.summary.AsyncSessionLocal', new=self.database), \
             patch('utils.summary.get_llm', return_value=model):
            await maybe_update_summary(1)
        self.assertEqual(self.persisted(), ('newer committed goal', 8))

    async def test_history_loader_excludes_partial_and_foreign_messages_before_window(self):
        async with self.database() as database:
            messages, total = await _load_agent_history(database, 1)
        self.assertEqual(total, 25)
        self.assertEqual(len(messages), HISTORY_MESSAGE_LIMIT)
        self.assertEqual(messages[0]['content'], 'complete-message-7')
        self.assertEqual(messages[-1]['content'], 'complete-message-26')
        self.assertNotIn('cancelled-private-prefix', str(messages))
        self.assertNotIn('foreign-session-secret', str(messages))


if __name__ == "__main__":
    unittest.main()

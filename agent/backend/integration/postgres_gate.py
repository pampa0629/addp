"""Real PostgreSQL gate: all Agent fixtures and migrations roll back on exit."""
import asyncio
import os
from pathlib import Path
import shutil
import tempfile
from unittest.mock import AsyncMock, patch
from types import SimpleNamespace

from sqlalchemy import text, select, func
from sqlalchemy.ext.asyncio import AsyncSession, create_async_engine

from services.schema import migrate, MIGRATION_ROOT
from api.sessions import SessionUpdate, VersionInput, get_session, list_sessions, update_session, delete_session, list_session_runs
from api.runs import get_agent_run
from services.plans import read_run_plan
from services.runs import create_agent_run, retry_agent_run
from models.run_event import AgentRunEvent
from models.session import Session
from models.message import Message
from api.chat import _project_assistant_message, _load_agent_history
from services.run_events import append_run_event
from starlette.requests import Request


def request():
    result = Request(dict(type='http', method='GET', path='/', headers=[]))
    result.state.principal_id = 7
    result.state.tenant_id = 10
    return result


async def check_partial_messages(db, run):
    run_id = run.id
    # Message projection and replay delta are one transaction; a failed write exposes neither.
    for content, delta in [('你🙂', '你🙂'), ('你🙂好', '好')]:
        await _project_assistant_message(db, session_id=1, message_id='partial', content=content,
            parts=[{'type': 'text', 'text': content}], complete=False)
        await append_run_event(db, agent_run_id=run_id, protocol_invocation_id='stream-fixture',
            event_payload={'type': 'TEXT_MESSAGE_CONTENT', 'messageId': 'partial', 'delta': delta})
        await db.commit()
    messages = (await db.execute(select(Message).where(Message.protocol_message_id == 'partial'))).scalars().all()
    assert len(messages) == 1 and messages[0].content == '你🙂好' and messages[0].is_complete is False
    assert (await _load_agent_history(db, 1))[0] == []
    before = (await db.execute(select(func.count()).select_from(AgentRunEvent))).scalar_one()
    nested = await db.begin_nested()
    await _project_assistant_message(db, session_id=1, message_id='partial', content='uncommitted',
        parts=[{'type':'text','text':'uncommitted'}], complete=False)
    await append_run_event(db, agent_run_id=run_id, protocol_invocation_id='stream-fixture',
        event_payload={'type':'TEXT_MESSAGE_CONTENT','messageId':'partial','delta':'uncommitted'})
    await nested.rollback()
    db.expire_all()
    stored = (await db.execute(select(Message).where(Message.protocol_message_id == 'partial'))).scalar_one()
    assert stored.content == '你🙂好'
    assert (await db.execute(select(func.count()).select_from(AgentRunEvent))).scalar_one() == before
    # Explicit retry uses another protocol message, preserving the incomplete prior attempt.
    await _project_assistant_message(db, session_id=1, message_id='retry-answer', content='complete',
        parts=[{'type':'text','text':'complete'}], complete=True)
    await db.commit()
    history, count = await _load_agent_history(db, 1)
    assert history == [{'role':'assistant','content':'complete'}] and count == 1
    # Exercise the summary's actual SQL predicate as well as the immediate context predicate.
    for index in range(25):
        db.add(Message(session_id=1, role='user', content=f'complete-{index}', is_complete=True))
    await db.commit()
    model = SimpleNamespace(ainvoke=AsyncMock(return_value=SimpleNamespace(content='safe summary')))
    from utils.summary import maybe_update_summary
    with patch('utils.summary.AsyncSessionLocal', side_effect=lambda: AsyncSession(
            bind=db.bind, expire_on_commit=False, join_transaction_mode='create_savepoint')), \
         patch('utils.summary.get_llm', return_value=model):
        await maybe_update_summary(1)
    model.ainvoke.assert_awaited_once()
    prompt = model.ainvoke.await_args.args[0][0].content
    assert '你🙂好' not in prompt and 'complete' in prompt


async def main():
    from sqlalchemy.engine import make_url
    url = make_url(os.environ['AGENT_POSTGRES_TEST_DSN']).set(drivername='postgresql+asyncpg', query={})
    engine = create_async_engine(url)
    try:
        async with engine.connect() as connection:
            # Hold the same owner lock throughout safety checks and every test.
            await connection.execute(text("SELECT pg_advisory_lock(hashtextextended('addp:agent:postgres-gate',0))"))
            await connection.commit()
            exists = (await connection.execute(text("SELECT to_regnamespace('agent')"))).scalar_one()
            await connection.rollback()
            if exists is not None:
                raise RuntimeError('Agent gate refuses a pre-existing agent schema')
            try:
                for legacy in (False, True):
                    transaction = await connection.begin()
                    try:
                        if legacy:
                            await connection.execute(text('CREATE SCHEMA agent'))
                            await connection.execute(text('''CREATE TABLE agent.sessions(id SERIAL PRIMARY KEY, user_id INT NOT NULL,
                                tenant_id INT NOT NULL, title VARCHAR(255), created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now())'''))
                            await connection.execute(text("INSERT INTO agent.sessions(user_id,tenant_id,title) VALUES(7,10,'legacy')"))
                        await migrate(connection)
                        await migrate(connection)
                        versions = (await connection.execute(text('SELECT version FROM agent.schema_migrations ORDER BY version'))).scalars().all()
                        assert versions == [1,2,3]
                        session = AsyncSession(bind=connection, expire_on_commit=False, join_transaction_mode='create_savepoint')
                        async with session:
                            if legacy:
                                preserved = await get_session(1, request(), session)
                                assert preserved['title'] == 'legacy' and preserved['version'] == 1
                            else:
                                session.add(Session(user_id=7,tenant_id=10,title='100%_literal'))
                                await session.commit()
                            changed = await update_session(1,request(),SessionUpdate(version=1,title='100%_renamed',pinned=True,archived=True),session)
                            assert changed['version'] == 2 and changed['archived']
                            stale = await update_session(1,request(),SessionUpdate(version=1,title='stale'),session)
                            assert stale.status_code == 409
                            stale_delete = await delete_session(1,request(),VersionInput(version=1),session)
                            assert stale_delete.status_code == 409
                            page = await list_sessions(request(),session,search='%_',archived=True)
                            assert page['total'] == 1 and page['data'][0]['title'] == '100%_renamed'
                            run = await create_agent_run(session,session_id=1,user_id=7,tenant_id=10,protocol_run_id='plan-fixture',reply_detail='concise')
                            await check_partial_messages(session, run)
                            await session.refresh(run)
                            run.status='failed'; await session.commit(); await session.refresh(run)
                            assert run.checkpoint['reply_detail']=='concise'
                            retried=await retry_agent_run(session,agent_run_id=run.id,session_id=1,user_id=7,tenant_id=10)
                            assert retried.id==run.id and retried.checkpoint['reply_detail']=='concise'
                            run.status='cancelled'; await session.commit()
                            todos=[{'content':'Inspect','status':'pending'}]
                            session.add(AgentRunEvent(agent_run_id=run.id,sequence=3,protocol_invocation_id='plan-fixture',event_type='ACTIVITY_SNAPSHOT',
                                payload={'type':'ACTIVITY_SNAPSHOT','activityType':'addp-plan','content':{'agent_run_id':str(run.id),'todos':todos}}))
                            await session.commit()
                            assert await read_run_plan(session,run.id,7,10) == todos
                            assert await read_run_plan(session,run.id,7,20) == []
                            runs=await list_session_runs(1,request(),session)
                            assert runs['total']==1 and runs['data'][0]['status']=='cancelled'
                            detail=await get_agent_run(run.id,request(),session)
                            assert detail['plan']==todos and detail['steps']==[]
                            foreign = request(); foreign.state.tenant_id = 20
                            assert (await list_sessions(foreign,session,archived=True))['total'] == 0
                        for statement,expected in (("UPDATE agent.startup_schema_revision SET version=999",'agent_database_version_ahead'),
                                                   ("DELETE FROM agent.schema_migrations WHERE version=1",'agent_migration_history_mismatch')):
                            nested=await connection.begin_nested()
                            try:
                                await connection.execute(text(statement))
                                try: await migrate(connection)
                                except ValueError as error: assert str(error)==expected
                                else: raise AssertionError('invalid migration identity accepted')
                            finally: await nested.rollback()
                        with tempfile.TemporaryDirectory(prefix='addp-agent-migration-') as tmp:
                            root=Path(tmp)
                            for path in MIGRATION_ROOT.iterdir(): shutil.copy(path,root/path.name)
                            path=root/'000001_initial.sql'; path.write_text(path.read_text()+'\n-- changed\n')
                            try: await migrate(connection,root)
                            except ValueError as error: assert str(error)=='agent_migration_history_mismatch'
                            else: raise AssertionError('altered history accepted')
                        print('PASS', 'legacy upgrade' if legacy else 'fresh initialization', 'idempotency, downgrade/history rejection, version conflicts, search, run/plan readback and Tenant isolation')
                    finally:
                        await transaction.rollback()
                transaction=await connection.begin()
                try:
                    with tempfile.TemporaryDirectory(prefix='addp-agent-failed-migration-') as tmp:
                        root=Path(tmp)
                        for path in MIGRATION_ROOT.iterdir(): shutil.copy(path,root/path.name)
                        (root/f'{len(list(MIGRATION_ROOT.glob("*.sql")))+1:06d}_failure.sql').write_text('CREATE TABLE agent.failure_probe(id INT); SELECT agent.missing_function();')
                        try: await migrate(connection,root)
                        except Exception as error: assert getattr(error, 'sqlstate', None) == '42883', type(error).__name__
                        else: raise AssertionError('broken migration accepted')
                finally: await transaction.rollback()
                assert (await connection.execute(text("SELECT to_regnamespace('agent')"))).scalar_one() is None
                await connection.rollback()
                print('PASS failed migration rolls back DDL and history; zero Agent schema residue')
            finally:
                await connection.execute(text("SELECT pg_advisory_unlock(hashtextextended('addp:agent:postgres-gate',0))"))
                await connection.commit()
    finally:
        await engine.dispose()


if __name__ == '__main__':
    asyncio.run(main())

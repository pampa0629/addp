"""Real PostgreSQL gate: all Agent fixtures and migrations roll back on exit."""
import asyncio
import os
from pathlib import Path
import shutil
import tempfile

from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncSession, create_async_engine

from services.schema import migrate, MIGRATION_ROOT
from api.sessions import SessionUpdate, VersionInput, get_session, list_sessions, update_session, delete_session, list_session_runs
from api.runs import get_agent_run
from services.plans import read_run_plan
from models.run import AgentRun
from models.run_event import AgentRunEvent
from uuid import uuid4
from models.session import Session
from starlette.requests import Request


def request():
    result = Request(dict(type='http', method='GET', path='/', headers=[]))
    result.state.principal_id = 7
    result.state.tenant_id = 10
    return result


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
                        assert versions == [1,2]
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
                            run = AgentRun(id=uuid4(),session_id=1,user_id=7,tenant_id=10,status='cancelled',initial_protocol_run_id='plan-fixture')
                            session.add(run); await session.commit()
                            todos=[{'content':'Inspect','status':'pending'}]
                            session.add(AgentRunEvent(agent_run_id=run.id,sequence=1,protocol_invocation_id='plan-fixture',event_type='ACTIVITY_SNAPSHOT',
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
                        (root/'000003_failure.sql').write_text('CREATE TABLE agent.failure_probe(id INT); SELECT agent.missing_function();')
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

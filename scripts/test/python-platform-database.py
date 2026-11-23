"""Exercise each module via its installed runtime identity in the disposable gate."""
import asyncio
import sys
from uuid import UUID

from sqlalchemy import select, text
from sqlalchemy.exc import DBAPIError

import database
from repositories.inference_scenario_binding_repository import InferenceScenarioBindingRepository

owner, action = sys.argv[1:]
platform_profile = "d20a4867-d09d-4c58-826f-1893ab023f00"
tenant_profile = "7355428e-0901-4326-87a6-3690dee0c20c"


async def check_history():
    from addp_common.infra.database import open_async_database, platform_target
    from addp_common.infra.schema import install
    from services.schema import migrate

    engine = open_async_database(platform_target("agent", installation=True))
    try:
        async with engine.begin() as conn:
            digest = await conn.scalar(text("SELECT sha256 FROM agent.schema_migrations WHERE version=1"))
            await conn.execute(text("UPDATE agent.schema_migrations SET sha256=:digest WHERE version=1"), {"digest": "0" * 64})
        for verify in (database.init_db, lambda: install("agent", migrate)):
            try:
                await verify()
            except ValueError as exc:
                assert str(exc) == "agent_migration_history_mismatch", str(exc)
            else:
                raise AssertionError("altered Agent migration digest was accepted")
        async with engine.begin() as conn:
            await conn.execute(text("UPDATE agent.schema_migrations SET sha256=:digest WHERE version=1"), {"digest": digest})
        await database.init_db()
    finally:
        await engine.dispose()


async def main():
    if action == "history":
        await check_history()
        return
    if action == "unprepared":
        try:
            await database.init_db()
        except DBAPIError as exc:
            # The owner namespace exists but the version table does not.
            if getattr(exc.orig, "pgcode", None) != "42P01" and exc.orig.args[0] != 1146:
                raise
            return
        raise AssertionError("unprepared module started")

    if action == "prior":
        from pathlib import Path
        from tempfile import TemporaryDirectory
        from addp_common.infra.database import provider
        from addp_common.infra.schema import install
        from services.schema import MIGRATION_ROOT, MYSQL_ROOT, migrate

        root = MYSQL_ROOT if provider() == "mysql" else MIGRATION_ROOT
        with TemporaryDirectory() as previous:
            for path in sorted(root.glob("*.sql"))[:-1]:
                (Path(previous) / path.name).write_bytes(path.read_bytes())
            await install("agent", lambda conn: migrate(conn, Path(previous)))
        try:
            await database.init_db()
        except ValueError as exc:
            assert "database version mismatch" in str(exc)
        else:
            raise AssertionError("Agent started on a previous schema version")
        return

    await database.init_db()
    params = dict(scenario_code="gate", expected_version=0, updated_by=1)
    if owner == "agent":
        from models.message import Message
        from models.run import AgentRun
        from models.session import Session
        async with database.AsyncSessionLocal() as db:
            repo = InferenceScenarioBindingRepository(db)
            if action == "write":
                await repo.save(scope_type="platform", tenant_id=None, model_profile_id=platform_profile, **params)
                await repo.save(scope_type="tenant", tenant_id=7, model_profile_id=tenant_profile, **params)
                session = Session(user_id=3, tenant_id=7, title="原始对话")
                db.add(session)
                await db.flush()
                db.add(Message(session_id=session.id, role="user", content="你好", parts=[{"text": "你好"}]))
                run = AgentRun(session_id=session.id, user_id=3, tenant_id=7, initial_protocol_run_id="gate", status="running")
                db.add(run)
                await db.flush()
                assert run.created_at.tzinfo is not None
                await db.commit()
            assert (await repo.resolve(7, "gate")).model_profile_id == tenant_profile
            assert (await repo.resolve(8, "gate")).model_profile_id == platform_profile
            message = (await db.scalars(select(Message))).one()
            assert message.parts == [{"text": "你好"}] and message.is_complete
            run = (await db.scalars(select(AgentRun))).one()
            assert isinstance(run.id, UUID) and run.created_at.tzinfo is not None
    else:
        with database.SessionLocal() as db:
            repo = InferenceScenarioBindingRepository(db)
            if action == "write":
                repo.save(scope_type="platform", tenant_id=None, model_profile_id=platform_profile, **params)
                repo.save(scope_type="tenant", tenant_id=7, model_profile_id=tenant_profile, **params)
            assert repo.resolve(7, "gate").model_profile_id == tenant_profile
            assert repo.resolve(8, "gate").model_profile_id == platform_profile


async def run():
    try:
        await main()
    finally:
        await database.close_db()


asyncio.run(run())
print(f"PASS {owner} {action}")

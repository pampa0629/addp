"""Prepare Python-owned schemas at deployment and verify their version at startup."""

import re

from sqlalchemy import text


def revision_query(provider, owner):
    if not re.fullmatch(r"[a-z][a-z0-9_]*", owner):
        raise ValueError("invalid platform database owner")
    table = owner + ".startup_schema_revision"
    if provider == "postgresql":
        return text(f"SELECT version, 0 FROM {table} WHERE singleton = TRUE")
    if provider == "mysql":
        return text(f"SELECT version, pending_version FROM {table} WHERE owner=:owner")
    raise ValueError(f"unsupported platform database dialect {provider!r}")


def require_version(connection, owner, version):
    """Read-only check, also usable with AsyncConnection.run_sync."""
    row = connection.execute(revision_query(connection.dialect.name, owner), {"owner": owner}).one_or_none()
    if row is None or tuple(row) != (version, 0):
        raise ValueError(f"{owner} database version mismatch or unfinished migration; prepare database before startup")


async def prepare_schema(connection, owner, version, apply):
    """Apply owner SQL on one deployment connection supplied by the CLI.

    PostgreSQL uses the caller's transaction. MySQL commits the pending marker
    before DDL and holds a connection lock across its implicit commits. The
    owner verifies its history even if the completed version is current.
    """
    provider = connection.dialect.name
    revision = revision_query(provider, owner)
    params = {"owner": owner, "version": version}
    table = owner + ".startup_schema_revision"
    if provider == "postgresql":
        await connection.execute(text("SELECT pg_advisory_xact_lock(hashtextextended(:key,0))"), {"key": "addp.schema/" + owner})
        if not await connection.scalar(text("SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=:owner)"), params):
            await connection.execute(text(f"CREATE SCHEMA {owner}"))
        await connection.execute(text(f"""CREATE TABLE IF NOT EXISTS {table} (
            singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton), version BIGINT NOT NULL)"""))
        row = (await connection.execute(revision, params)).one_or_none()
        current = row[0] if row else 0
        if current > version:
            raise ValueError(f"{owner}_database_version_ahead")
        await apply(connection, current)
        if current != version:
            await connection.execute(text(f"""INSERT INTO {table}(singleton, version) VALUES(TRUE,:version)
                ON CONFLICT (singleton) DO UPDATE SET version=EXCLUDED.version"""), params)
        return

    lock = "SHA2(CONCAT('addp.schema/',DATABASE(),'/',:owner),256)"
    if await connection.scalar(text("SELECT DATABASE()")) != owner:
        raise ValueError(f"{owner} installation requires its own database")
    if await connection.scalar(text(f"SELECT GET_LOCK({lock},30)"), params) != 1:
        raise RuntimeError(f"{owner} database migration lock unavailable")
    try:
        await connection.execute(text(f"""CREATE TABLE IF NOT EXISTS {table} (
            owner VARCHAR(64) PRIMARY KEY, version BIGINT NOT NULL, pending_version BIGINT NOT NULL) ENGINE=InnoDB"""))
        row = (await connection.execute(revision, params)).one_or_none()
        current, pending = row if row else (0, 0)
        if current > version:
            raise ValueError(f"{owner}_database_version_ahead")
        if pending not in (0, version):
            raise ValueError(f"{owner} database has an unfinished migration to version {pending}")
        if current != version or pending:
            await connection.execute(text(f"""INSERT INTO {table} (owner,version,pending_version) VALUES (:owner,0,:version)
                ON DUPLICATE KEY UPDATE pending_version=:version"""), params)
            await connection.commit()
        await apply(connection, current)
        if current != version or pending:
            await connection.execute(text(f"UPDATE {table} SET version=:version,pending_version=0 WHERE owner=:owner"), params)
        await connection.commit()
    finally:
        await connection.rollback()
        if await connection.scalar(text(f"SELECT RELEASE_LOCK({lock})"), params) != 1:
            raise RuntimeError(f"{owner} database migration lock lost")


async def install(owner, migrate):
    from .database import open_async_database, platform_target

    engine = open_async_database(platform_target(owner, installation=True))
    try:
        async with engine.connect() as conn:
            await migrate(conn)
            await conn.commit()
    finally:
        await engine.dispose()

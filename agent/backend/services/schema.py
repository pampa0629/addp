"""Agent-owned immutable, transactional forward migrations."""
import hashlib
from pathlib import Path
import re

from sqlalchemy import text
from addp_common.infra.schema import prepare_schema

MIGRATION_ROOT = Path(__file__).resolve().parents[1] / 'migrations'


MYSQL_ROOT = MIGRATION_ROOT.with_name("migrations_mysql")


def migration_catalog(root=MIGRATION_ROOT):
    files = sorted(root.iterdir())
    catalog = []
    for version, path in enumerate(files, 1):
        if not path.is_file() or not re.fullmatch(rf'{version:06d}_[a-z0-9_]+\.sql', path.name):
            raise ValueError('agent_migration_catalog_invalid')
        sql = path.read_text()
        catalog.append((version, path.name, hashlib.sha256(path.read_bytes()).hexdigest(), sql))
    if not catalog:
        raise ValueError('agent_migration_catalog_empty')
    return catalog


async def verify_history(connection, catalog, current):
    applied = (await connection.execute(text(
        "SELECT version, filename, sha256 FROM agent.schema_migrations ORDER BY version"
    ))).all()
    if len(applied) != current or [tuple(row) for row in applied] != [entry[:3] for entry in catalog[:current]]:
        raise ValueError("agent_migration_history_mismatch")


async def migrate(connection, root=None):
    postgres = connection.dialect.name == "postgresql"
    catalog = migration_catalog(root or (MIGRATION_ROOT if postgres else MYSQL_ROOT))

    async def apply(conn, current):
        if postgres:
            history = """version BIGINT PRIMARY KEY CHECK(version > 0), filename TEXT NOT NULL,
                sha256 TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()"""
        else:
            history = """version BIGINT PRIMARY KEY CHECK(version > 0), filename VARCHAR(255) NOT NULL,
                sha256 CHAR(64) NOT NULL, applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)"""
        if current == 0:
            await conn.execute(text(f"CREATE TABLE IF NOT EXISTS agent.schema_migrations ({history})"))
        await verify_history(conn, catalog, current)
        for version, filename, digest, sql in catalog[current:]:
            if postgres:
                raw = await conn.get_raw_connection()
                await raw.driver_connection.execute(sql)
            else:
                # A MySQL baseline file contains one replayable CREATE TABLE.
                await conn.exec_driver_sql(sql)
        # MySQL DDL commits implicitly. Publish all new history rows together
        # with the completed version after the last DDL succeeds.
        if current < len(catalog):
            await conn.execute(text("INSERT INTO agent.schema_migrations(version,filename,sha256) VALUES (:v,:f,:h)"),
                               [dict(v=v, f=f, h=h) for v, f, h, _ in catalog[current:]])
    await prepare_schema(connection, "agent", len(catalog), apply)

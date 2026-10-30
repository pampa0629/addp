"""Agent-owned immutable, transactional forward migrations."""
import hashlib
from pathlib import Path
import re

from sqlalchemy import text

MIGRATION_ROOT = Path(__file__).resolve().parents[1] / 'migrations'


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


async def migrate(connection, root=MIGRATION_ROOT):
    # The caller owns one transaction. A failed migration rolls back its DDL and
    # version record together; concurrent startup cannot run the same DDL twice.
    catalog = migration_catalog(root)
    await connection.execute(text("SELECT pg_advisory_xact_lock(hashtextextended('addp.schema/agent', 0))"))
    await connection.execute(text('CREATE SCHEMA IF NOT EXISTS agent'))
    await connection.execute(text('''CREATE TABLE IF NOT EXISTS agent.schema_migrations (
        version BIGINT PRIMARY KEY CHECK(version > 0), filename TEXT NOT NULL, sha256 TEXT NOT NULL,
        applied_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp())'''))
    await connection.execute(text('CREATE TABLE IF NOT EXISTS agent.startup_schema_revision (singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton), version BIGINT NOT NULL)'))
    current = (await connection.execute(text('SELECT COALESCE(MAX(version),0) FROM agent.startup_schema_revision'))).scalar_one()
    applied = (await connection.execute(text('SELECT version, filename, sha256 FROM agent.schema_migrations ORDER BY version'))).all()
    if current > len(catalog):
        raise ValueError('agent_database_version_ahead')
    if current != len(applied):
        raise ValueError('agent_migration_history_mismatch')
    if len(applied) > len(catalog):
        raise ValueError('agent_database_version_ahead')
    for index, row in enumerate(applied):
        if tuple(row) != catalog[index][:3]:
            raise ValueError('agent_migration_history_mismatch')
    raw = await connection.get_raw_connection()
    for version, filename, digest, sql in catalog[len(applied):]:
        await raw.driver_connection.execute(sql)
        await connection.execute(text('INSERT INTO agent.schema_migrations(version,filename,sha256) VALUES (:version,:filename,:digest)'),
                                 dict(version=version,filename=filename,digest=digest))
    await connection.execute(text('INSERT INTO agent.startup_schema_revision(singleton,version) VALUES(true,:version) ON CONFLICT(singleton) DO UPDATE SET version=excluded.version'), dict(version=len(catalog)))

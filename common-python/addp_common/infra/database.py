"""Platform PostgreSQL sessions for Python backends."""

import os
from dataclasses import dataclass
from zoneinfo import ZoneInfo

from sqlalchemy import URL, create_engine
from sqlalchemy.ext.asyncio import create_async_engine


@dataclass(frozen=True)
class DatabaseConfig:
    host: str
    port: int | None
    user: str
    password: str | None
    name: str


def _postgres_url(target: DatabaseConfig, driver: str) -> URL:
    return URL.create(
        driver,
        host=target.host,
        port=target.port,
        username=target.user,
        password=target.password,
        database=target.name,
    )


def _timezone() -> str:
    provider = os.environ.get("ADDP_INFRA_DATABASE_PROVIDER") or "postgres"
    if provider != "postgres":
        raise ValueError(f"unsupported platform database provider {provider!r}")
    return ZoneInfo(os.environ.get("TZ") or "Asia/Shanghai").key


def open_async_database(target: DatabaseConfig, **pool_options):
    return create_async_engine(
        _postgres_url(target, "postgresql+asyncpg"),
        connect_args={"server_settings": {"timezone": _timezone()}},
        **pool_options,
    )


def open_sync_database(target: DatabaseConfig, **pool_options):
    return create_engine(
        _postgres_url(target, "postgresql+psycopg2"),
        connect_args={
            "options": f"-c timezone={_timezone()}",
            # Use password authentication rather than host Kerberos tickets.
            "gssencmode": "disable",
            "connect_timeout": 5,
        },
        **pool_options,
    )

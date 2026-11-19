"""Platform PostgreSQL sessions for Python backends."""

import os
from zoneinfo import ZoneInfo

from sqlalchemy import URL, create_engine
from sqlalchemy.ext.asyncio import create_async_engine


def _timezone() -> str:
    provider = os.environ.get("ADDP_INFRA_DATABASE_PROVIDER") or "postgres"
    if provider != "postgres":
        raise ValueError(f"unsupported platform database provider {provider!r}")
    return ZoneInfo(os.environ.get("TZ") or "Asia/Shanghai").key


def open_async_database(target: URL, **pool_options):
    return create_async_engine(
        target.set(drivername="postgresql+asyncpg"),
        connect_args={"server_settings": {"timezone": _timezone()}},
        **pool_options,
    )


def open_sync_database(target: URL, **pool_options):
    return create_engine(
        target.set(drivername="postgresql+psycopg2"),
        connect_args={
            "options": f"-c timezone={_timezone()}",
            # Use password authentication rather than host Kerberos tickets.
            "gssencmode": "disable",
            "connect_timeout": 5,
        },
        **pool_options,
    )

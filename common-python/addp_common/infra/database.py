"""Deployment-selected SQLAlchemy connections for the platform database."""

import os
import ssl
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


def provider():
    name = os.environ.get("ADDP_INFRA_DATABASE_PROVIDER") or "postgres"
    if name not in ("postgres", "mysql"):
        raise ValueError(f"unsupported platform database provider {name!r}")
    return name


def platform_target(owner, *, installation=False):
    selected = provider()
    prefix = "ADDP_DATABASE_DEPLOY" if installation else "ADDP_DATABASE_RUNTIME"
    user, password = os.getenv(prefix + "_USER"), os.getenv(prefix + "_PASSWORD")
    if not user or not password:
        raise ValueError(f"{prefix} credentials are required")
    if selected == "mysql":
        return DatabaseConfig(os.environ["ADDP_MYSQL_HOST"], int(os.environ["ADDP_MYSQL_PORT"]), user, password, owner)
    return DatabaseConfig(os.environ["POSTGRES_HOST"], int(os.environ["POSTGRES_PORT"]), user, password, os.environ["POSTGRES_DB"])


def _url(target, driver):
    return URL.create(driver, host=target.host, port=target.port, username=target.user,
                      password=target.password, database=target.name)


def _mysql_args():
    zone = ZoneInfo(os.environ.get("TZ") or "Asia/Shanghai").key
    options = {"charset": "utf8mb4", "connect_timeout": 5,
               "init_command": f"SET SESSION time_zone='{zone}',SESSION transaction_isolation='READ-COMMITTED'"}
    mode = os.getenv("ADDP_MYSQL_SSLMODE") or "disable"
    if mode == "verify-full":
        options["ssl"] = ssl.create_default_context()
    elif mode != "disable":
        raise ValueError(f"unsupported platform MySQL TLS mode {mode!r}")
    return options


def open_async_database(target: DatabaseConfig, **pool_options):
    if provider() == "mysql":
        return create_async_engine(_url(target, "mysql+aiomysql"), connect_args=_mysql_args(), **pool_options)
    return create_async_engine(_url(target, "postgresql+asyncpg"), **pool_options)


def open_sync_database(target: DatabaseConfig, **pool_options):
    if provider() == "mysql":
        return create_engine(_url(target, "mysql+pymysql"), connect_args=_mysql_args(), **pool_options)
    timezone = ZoneInfo(os.environ.get("TZ") or "Asia/Shanghai").key
    return create_engine(_url(target, "postgresql+psycopg2"), connect_args={
        "options": f"-c timezone={timezone}", "gssencmode": "disable", "connect_timeout": 5,
    }, **pool_options)

import os
import subprocess
import sys

import pytest
from sqlalchemy import URL, make_url

from addp_common.infra.database import open_sync_database


def test_unknown_platform_provider_rejected_before_engine_creation(monkeypatch):
    monkeypatch.setenv("ADDP_INFRA_DATABASE_PROVIDER", "mysql")
    with pytest.raises(ValueError, match="unsupported platform database provider 'mysql'"):
        open_sync_database(URL.create("postgresql", host="invalid"))


def test_platform_session_against_postgres():
    dsn = os.environ.get("COPILOT_POSTGRES_TEST_DSN")
    if not dsn:
        pytest.skip("set COPILOT_POSTGRES_TEST_DSN and use make test-copilot")
    target = make_url(dsn)
    # The fresh process binds the same engine imported by Copilot at startup.
    subprocess.run(
        [
            sys.executable, "-c",
            "from database import engine; from sqlalchemy import text\n"
            "with engine.connect() as connection:\n"
            "    name, zone = connection.execute(text(\"SELECT current_database(), current_setting('TimeZone')\")).one()\n"
            "    assert (name, zone) == (engine.url.database, 'Asia/Tokyo'), (name, zone)\n",
        ],
        check=True,
        timeout=20,
        env={
            **os.environ,
            "POSTGRES_HOST": target.host,
            "POSTGRES_PORT": str(target.port),
            "POSTGRES_USER": target.username,
            "POSTGRES_PASSWORD": target.password,
            "POSTGRES_DB": target.database,
            "ADDP_INFRA_DATABASE_PROVIDER": "postgres",
            "TZ": "Asia/Tokyo",
        },
    )

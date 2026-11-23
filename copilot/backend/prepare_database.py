import asyncio
from pathlib import Path

from addp_common.infra.schema import install, prepare_schema

ROOT = Path(__file__).resolve().parent
VERSION = 1


async def migrate(conn):
    async def apply(connection, current):
        if current == VERSION:
            return
        if connection.dialect.name == "mysql":
            await connection.exec_driver_sql((ROOT / "migrations_mysql" / "001_scenario_bindings.sql").read_text())
        else:
            raw = await connection.get_raw_connection()
            for path in sorted((ROOT / "migrations").glob("*.sql")):
                await raw.driver_connection.execute(path.read_text())
    await prepare_schema(conn, "copilot", VERSION, apply)


if __name__ == "__main__":
    asyncio.run(install("copilot", migrate))

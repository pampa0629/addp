from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from addp_common.infra.database import open_async_database, platform_target, provider
from addp_common.infra.schema import require_version
from services.schema import MIGRATION_ROOT, MYSQL_ROOT, migration_catalog, verify_history

AsyncSessionLocal = async_sessionmaker(class_=AsyncSession, expire_on_commit=False)


async def init_db():
    engine = open_async_database(platform_target("agent"), pool_pre_ping=True)
    try:
        catalog = migration_catalog(MYSQL_ROOT if provider() == "mysql" else MIGRATION_ROOT)
        async with engine.connect() as conn:
            await conn.run_sync(require_version, "agent", len(catalog))
            await verify_history(conn, catalog, len(catalog))
    except BaseException:
        await engine.dispose()
        raise
    AsyncSessionLocal.configure(bind=engine)


async def close_db():
    engine = AsyncSessionLocal.kw.get("bind")
    if engine is not None:
        AsyncSessionLocal.configure(bind=None)
        await engine.dispose()


async def get_db():
    async with AsyncSessionLocal() as session:
        yield session

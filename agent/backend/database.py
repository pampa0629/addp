from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from addp_common.infra.database import DatabaseConfig, open_async_database
from config import settings

engine = open_async_database(
    DatabaseConfig(
        user=settings.POSTGRES_USER,
        password=settings.POSTGRES_PASSWORD,
        host=settings.POSTGRES_HOST,
        port=settings.POSTGRES_PORT,
        name=settings.POSTGRES_DB,
    ),
    pool_pre_ping=True,
)
AsyncSessionLocal = async_sessionmaker(engine, class_=AsyncSession, expire_on_commit=False)


async def init_db():
    from services.schema import migrate
    async with engine.begin() as conn:
        await migrate(conn)


async def get_db():
    async with AsyncSessionLocal() as session:
        try:
            yield session
        finally:
            await session.close()

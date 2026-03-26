from sqlalchemy.ext.asyncio import create_async_engine, AsyncSession, async_sessionmaker
from sqlalchemy import text
from config import settings
from models import Base

DATABASE_URL = (
    f"postgresql+asyncpg://{settings.POSTGRES_USER}:{settings.POSTGRES_PASSWORD}"
    f"@{settings.POSTGRES_HOST}:{settings.POSTGRES_PORT}/{settings.POSTGRES_DB}"
)

engine = create_async_engine(DATABASE_URL, echo=False)
AsyncSessionLocal = async_sessionmaker(engine, class_=AsyncSession, expire_on_commit=False)


async def init_db():
    async with engine.begin() as conn:
        # 创建 agent schema
        await conn.execute(text(f"CREATE SCHEMA IF NOT EXISTS {settings.AGENT_DB_SCHEMA}"))
        # 创建所有表
        await conn.run_sync(Base.metadata.create_all)
        # 幂等迁移：为已有表补充新列（create_all 不会自动添加列）
        await conn.execute(text(
            "ALTER TABLE agent.sessions ADD COLUMN IF NOT EXISTS summary TEXT"
        ))


async def get_db():
    async with AsyncSessionLocal() as session:
        try:
            yield session
        finally:
            await session.close()

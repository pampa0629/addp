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
        await conn.execute(text(
            "ALTER TABLE agent.sessions ADD COLUMN IF NOT EXISTS summary_message_id INTEGER"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.runs ADD COLUMN IF NOT EXISTS metrics JSONB NOT NULL DEFAULT '{}'::jsonb"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.runs ADD COLUMN IF NOT EXISTS context_metrics JSONB NOT NULL DEFAULT '{}'::jsonb"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.runs ADD COLUMN IF NOT EXISTS error_source VARCHAR(30)"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.runs ADD COLUMN IF NOT EXISTS error_code VARCHAR(100)"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.runs DROP COLUMN IF EXISTS error_type"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.run_steps ADD COLUMN IF NOT EXISTS error_source VARCHAR(30)"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.run_steps ADD COLUMN IF NOT EXISTS error_code VARCHAR(100)"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.messages ADD COLUMN IF NOT EXISTS protocol_message_id VARCHAR(100)"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.messages ADD COLUMN IF NOT EXISTS parts JSONB NOT NULL DEFAULT '[]'::jsonb"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.messages DROP COLUMN IF EXISTS message_type"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.messages DROP COLUMN IF EXISTS result_type"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.messages DROP COLUMN IF EXISTS result_data"
        ))
        await conn.execute(text(
            "CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_messages_session_protocol_id "
            "ON agent.messages (session_id, protocol_message_id) "
            "WHERE protocol_message_id IS NOT NULL"
        ))
        # AgentRun clean break：Interaction 只关联服务端 AgentRun UUID，不保留协议 run_id 双轨。
        await conn.execute(text("""
            DO $$
            BEGIN
                IF EXISTS (
                    SELECT 1
                    FROM information_schema.columns
                    WHERE table_schema = 'agent'
                      AND table_name = 'interactions'
                      AND column_name = 'run_id'
                ) THEN
                    DELETE FROM agent.interactions;
                    ALTER TABLE agent.interactions DROP COLUMN run_id;
                END IF;
            END
            $$
        """))
        await conn.execute(text(
            "ALTER TABLE agent.interactions ADD COLUMN IF NOT EXISTS agent_run_id UUID"
        ))
        await conn.execute(text(
            "DELETE FROM agent.interactions WHERE agent_run_id IS NULL"
        ))
        await conn.execute(text(
            "ALTER TABLE agent.interactions ALTER COLUMN agent_run_id SET NOT NULL"
        ))
        await conn.execute(text("""
            DO $$
            BEGIN
                IF NOT EXISTS (
                    SELECT 1
                    FROM pg_constraint
                    WHERE conrelid = 'agent.interactions'::regclass
                      AND confrelid = 'agent.runs'::regclass
                      AND contype = 'f'
                ) THEN
                    ALTER TABLE agent.interactions
                    ADD CONSTRAINT fk_agent_interactions_run
                    FOREIGN KEY (agent_run_id) REFERENCES agent.runs(id) ON DELETE CASCADE;
                END IF;
            END
            $$
        """))
        await conn.execute(text(
            "CREATE INDEX IF NOT EXISTS idx_agent_interactions_agent_run_id "
            "ON agent.interactions (agent_run_id)"
        ))


async def get_db():
    async with AsyncSessionLocal() as session:
        try:
            yield session
        finally:
            await session.close()

import asyncio

from sqlalchemy.orm import declarative_base, sessionmaker

from addp_common.infra.database import open_sync_database, platform_target
from addp_common.infra.schema import require_version
from prepare_database import VERSION

Base = declarative_base()
SessionLocal = sessionmaker(autocommit=False, autoflush=False)


def open_runtime():
    return open_sync_database(platform_target("copilot"), pool_pre_ping=False, pool_size=5,
                              max_overflow=10, pool_recycle=3600)


async def init_db():
    def bind():
        engine = open_runtime()
        try:
            with engine.connect() as conn:
                require_version(conn, "copilot", VERSION)
        except BaseException:
            engine.dispose()
            raise
        SessionLocal.configure(bind=engine)
    await asyncio.to_thread(bind)


async def close_db():
    engine = SessionLocal.kw.get("bind")
    if engine is not None:
        SessionLocal.configure(bind=None)
        await asyncio.to_thread(engine.dispose)


def get_db():
    with SessionLocal() as session:
        yield session

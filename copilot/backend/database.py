"""
数据库连接和初始化
"""
from sqlalchemy import create_engine
from sqlalchemy.orm import declarative_base, sessionmaker
from config import settings

# 创建数据库引擎（优化配置避免延迟）
engine = create_engine(
    settings.database_url,
    pool_pre_ping=False,  # 禁用连接预检查
    pool_size=5,
    max_overflow=10,
    pool_recycle=3600,
    connect_args={"connect_timeout": 5}
)

# 创建会话工厂
SessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)

# 创建模型基类
Base = declarative_base()

# 在模块级别导入所有模型（避免在异步函数中导入导致的延迟）
from models import conversation, message, llm_config  # noqa: E402


async def init_db():
    """初始化数据库"""
    import asyncio
    loop = asyncio.get_event_loop()

    # 在线程池中执行同步数据库操作
    def _sync_create():
        Base.metadata.create_all(bind=engine)

    await loop.run_in_executor(None, _sync_create)

    print("✅ Database tables created successfully", flush=True)


def get_db():
    """获取数据库会话 (用于 FastAPI Depends)"""
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()

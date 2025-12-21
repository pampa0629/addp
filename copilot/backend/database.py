"""
数据库连接和初始化
"""
from sqlalchemy import create_engine
from sqlalchemy.ext.declarative import declarative_base
from sqlalchemy.orm import sessionmaker
from config import settings

# 创建数据库引擎
engine = create_engine(
    settings.database_url,
    pool_pre_ping=True,
    pool_size=10,
    max_overflow=20
)

# 创建会话工厂
SessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)

# 创建模型基类
Base = declarative_base()


async def init_db():
    """初始化数据库"""
    # 导入所有模型以确保它们被注册到 Base.metadata
    from models import conversation, message, llm_config

    # 创建所有表 (在生产环境应使用 Alembic 迁移)
    # 注意：需要先通过 SQL 脚本创建 schema
    Base.metadata.create_all(bind=engine)
    print("✅ Database tables created successfully")


def get_db():
    """获取数据库会话 (用于 FastAPI Depends)"""
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()

"""
Jupyter Engine 配置模块
从项目根目录的 .env 文件加载配置（与 Go 模块保持一致）
"""
import os
from pathlib import Path
from dotenv import load_dotenv

# 加载根目录的 .env 文件
project_root = Path(__file__).resolve().parent.parent.parent
env_path = project_root / '.env'
load_dotenv(dotenv_path=env_path)

class Config:
    """Jupyter Engine 配置（从环境变量加载）"""

    # API 配置
    API_PORT = int(os.getenv('API_PORT', '8097'))

    # MinIO 配置（与 Manager 模块保持一致）
    MINIO_API_PORT = os.getenv('MINIO_API_PORT', '19000')
    MINIO_ENDPOINT = os.getenv('MINIO_ENDPOINT') or f"localhost:{MINIO_API_PORT}"
    MINIO_ACCESS_KEY = os.getenv('MINIO_ROOT_USER', 'minioadmin')
    MINIO_SECRET_KEY = os.getenv('MINIO_ROOT_PASSWORD', 'minioadmin')
    MINIO_USE_SSL = os.getenv('MINIO_USE_SSL', 'false').lower() == 'true'
    MINIO_BUCKET = 'develop'  # Notebook 专用 bucket

    # canonical AuthContext 解析服务
    SYSTEM_URL = os.getenv('SYSTEM_URL', 'http://localhost:8180')

    # 日志配置
    LOG_LEVEL = os.getenv('LOG_LEVEL', 'INFO').upper()

    # Workspace 配置
    WORKSPACE_DIR = os.getenv('WORKSPACE_DIR', '/workspace/notebooks')

    @classmethod
    def validate(cls):
        """验证必要配置是否存在"""
        if not cls.MINIO_ACCESS_KEY:
            raise ValueError("MINIO_ACCESS_KEY (or MINIO_ROOT_USER) is required")
        if not cls.MINIO_SECRET_KEY:
            raise ValueError("MINIO_SECRET_KEY (or MINIO_ROOT_PASSWORD) is required")

    @classmethod
    def print_config(cls):
        """打印配置信息（用于启动时调试）"""
        print("=" * 60)
        print("Jupyter Engine 配置:")
        print(f"  API_PORT: {cls.API_PORT}")
        print(f"  MINIO_ENDPOINT: {cls.MINIO_ENDPOINT}")
        print(f"  MINIO_BUCKET: {cls.MINIO_BUCKET}")
        print(f"  MINIO_USE_SSL: {cls.MINIO_USE_SSL}")
        print(f"  SYSTEM_URL: {cls.SYSTEM_URL}")
        print(f"  LOG_LEVEL: {cls.LOG_LEVEL}")
        print(f"  WORKSPACE_DIR: {cls.WORKSPACE_DIR}")
        print("=" * 60)

# 全局配置实例
config = Config()

# 启动时验证配置
try:
    config.validate()
except ValueError as e:
    print(f"❌ 配置验证失败: {e}")
    print("请检查 .env 文件中的配置")
    exit(1)

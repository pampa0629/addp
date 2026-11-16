"""
Jupyter Engine 配置模块
从项目根目录的 .env 文件加载配置（与 Go 模块保持一致）
"""
import os
import tempfile
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

    # canonical AuthContext 解析服务
    SYSTEM_URL = os.getenv('SYSTEM_URL', 'http://localhost:8180')

    # 日志配置
    LOG_LEVEL = os.getenv('LOG_LEVEL', 'INFO').upper()

    # Workspace 配置
    WORKSPACE_DIR = os.getenv('WORKSPACE_DIR', os.path.join(tempfile.gettempdir(), 'addp-jupyter-notebooks'))
    SESSION_TTL_SECONDS = int(os.getenv('JUPYTER_SESSION_TTL_SECONDS', '3600'))

    @classmethod
    def print_config(cls):
        """打印配置信息（用于启动时调试）"""
        print("=" * 60)
        print("Jupyter Engine 配置:")
        print(f"  API_PORT: {cls.API_PORT}")
        print(f"  SYSTEM_URL: {cls.SYSTEM_URL}")
        print(f"  LOG_LEVEL: {cls.LOG_LEVEL}")
        print(f"  WORKSPACE_DIR: {cls.WORKSPACE_DIR}")
        print(f"  SESSION_TTL_SECONDS: {cls.SESSION_TTL_SECONDS}")
        print("=" * 60)

# 全局配置实例
config = Config()

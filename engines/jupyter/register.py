#!/usr/bin/env python3
"""
Jupyter Engine 自注册脚本
启动时向 System 模块注册引擎信息
"""
import os
import requests
import time
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# 配置
SYSTEM_API_URL = os.getenv("SYSTEM_API_URL", "http://system-backend:8180")
JUPYTER_URL = os.getenv("JUPYTER_URL", "http://jupyter-engine:8097")
JUPYTER_LAB_URL = os.getenv("JUPYTER_LAB_URL", "http://localhost:8088/lab")
INTERNAL_API_KEY = os.getenv("INTERNAL_API_KEY", "")
MAX_RETRIES = 10
RETRY_INTERVAL = 5  # 秒


def register_engine():
    """向 System 模块注册 Jupyter Engine"""

    # 从 JUPYTER_URL 解析 host 和 port
    # JUPYTER_URL 格式: http://jupyter-engine:8097 或 http://localhost:8097
    import urllib.parse
    parsed_url = urllib.parse.urlparse(JUPYTER_URL)
    host = parsed_url.hostname or "localhost"
    port = parsed_url.port or 8097
    protocol = parsed_url.scheme or "http"

    capabilities = {
        "schema_version": "engine.capabilities/v1",
        "engine_type": "jupyter",
        "engine_family": "script",
        "compute": {
            "script": {
                "supported": True,
                "modes": ["notebook", "lab"],
                "languages": ["python", "shell"]
            }
        },
        "extensions": {
            "script_runtime": {
                "features": ["interactive", "parametrized", "async"]
            }
        }
    }

    # 构造引擎注册数据（与其他工作流引擎保持一致）
    engine_data = {
        "engine_type": "jupyter",
        "name": "Jupyter Engine",
        "description": "ADDP 内置 Jupyter Notebook 执行引擎，支持 Python 和 Shell 脚本",
        "connection_info": {
            "host": host,
            "port": port,
            "protocol": protocol
        },
        "capabilities": capabilities,
        "is_builtin": True
    }

    logger.info(f"开始注册 Jupyter Engine 到 System 模块...")
    logger.info(f"System API URL: {SYSTEM_API_URL}")
    logger.info(f"Jupyter Engine URL: {JUPYTER_URL}")

    # 重试逻辑（等待 System 模块启动）
    for attempt in range(1, MAX_RETRIES + 1):
        try:
            logger.info(f"尝试注册 (尝试 {attempt}/{MAX_RETRIES})...")

            # 调用 System 模块的引擎注册 API
            headers = {}
            if INTERNAL_API_KEY:
                headers["X-Internal-API-Key"] = INTERNAL_API_KEY

            response = requests.post(
                f"{SYSTEM_API_URL}/api/v1/internal/engines/register",
                json=engine_data,
                headers=headers,
                timeout=10
            )

            if response.status_code in [200, 202]:  # 202 Accepted 表示注册成功
                result = response.json()
                logger.info(f"✅ Jupyter Engine 注册成功！")
                logger.info(f"   Engine ID: {result.get('engine_id')}")
                logger.info(f"   Message: {result.get('message')}")
                return True
            elif response.status_code == 409:
                logger.info("ℹ️  Jupyter Engine 已存在，注册完成")
                return True
            else:
                logger.warning(f"⚠️  注册失败，状态码: {response.status_code}")
                logger.warning(f"   响应: {response.text}")

        except requests.exceptions.ConnectionError as e:
            logger.warning(f"⚠️  无法连接到 System 模块 ({SYSTEM_API_URL})")
            logger.warning(f"   等待 {RETRY_INTERVAL} 秒后重试...")
        except requests.exceptions.Timeout:
            logger.warning(f"⚠️  请求超时")
            logger.warning(f"   等待 {RETRY_INTERVAL} 秒后重试...")
        except Exception as e:
            logger.error(f"❌ 注册出错: {e}")

        if attempt < MAX_RETRIES:
            time.sleep(RETRY_INTERVAL)

    logger.error(f"❌ Jupyter Engine 注册失败，已尝试 {MAX_RETRIES} 次")
    return False


if __name__ == "__main__":
    success = register_engine()

    if success:
        logger.info("🎉 Jupyter Engine 注册流程完成")
        exit(0)
    else:
        logger.error("💔 Jupyter Engine 注册失败")
        # 注册失败不影响引擎启动
        exit(0)

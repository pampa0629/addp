"""
System 服务客户端（Python 版本）
用于调用 System 模块的内部 API
"""
import os
import httpx
from typing import Optional, Dict, Any


class SystemClient:
    """System 服务客户端"""

    def __init__(self, base_url: str, internal_api_key: str, timeout: int = 30):
        """
        初始化 System 客户端

        Args:
            base_url: System 服务地址，如 http://localhost:8180
            internal_api_key: 内部 API Key（用于服务间调用）
            timeout: 请求超时时间（秒）
        """
        self.base_url = base_url.rstrip('/')
        self.internal_api_key = internal_api_key
        self.timeout = timeout
        self.client = httpx.Client(
            timeout=timeout,
            headers={
                'Content-Type': 'application/json',
                'X-Internal-API-Key': internal_api_key
            }
        )

    def register_module(
        self,
        module_name: str,
        module_url: str,
        route_prefix: str,
        health_check_url: Optional[str] = None,
        metadata: Optional[Dict[str, Any]] = None
    ) -> bool:
        """
        注册模块（幂等操作）

        Args:
            module_name: 模块名称，如 'copilot'
            module_url: 模块地址，如 'http://localhost:8087'
            route_prefix: 路由前缀，如 '/copilot'
            health_check_url: 健康检查地址，如 'http://localhost:8087/health'
            metadata: 附加元数据

        Returns:
            是否注册成功
        """
        url = f"{self.base_url}/api/internal/modules/register"

        payload = {
            "module_name": module_name,
            "module_url": module_url,
            "route_prefix": route_prefix,
        }

        if health_check_url:
            payload["health_check_url"] = health_check_url

        if metadata:
            payload["metadata"] = metadata

        try:
            resp = self.client.post(url, json=payload)
            resp.raise_for_status()
            print(f"✅ 模块注册成功: {module_name}")
            return True
        except httpx.HTTPStatusError as e:
            print(f"❌ 模块注册失败 ({e.response.status_code}): {e.response.text}")
            return False
        except httpx.RequestError as e:
            print(f"❌ 模块注册请求失败: {str(e)}")
            return False

    def send_heartbeat(self, module_name: str) -> bool:
        """
        发送心跳

        Args:
            module_name: 模块名称

        Returns:
            是否成功
        """
        url = f"{self.base_url}/api/internal/modules/heartbeat"

        payload = {"module_name": module_name}

        try:
            resp = self.client.post(url, json=payload)
            resp.raise_for_status()
            return True
        except (httpx.HTTPStatusError, httpx.RequestError):
            # 心跳失败不打印日志，避免刷屏
            return False

    def close(self):
        """关闭客户端连接"""
        self.client.close()


def create_system_client_from_env() -> Optional[SystemClient]:
    """
    从环境变量创建 System 客户端

    需要的环境变量:
    - SYSTEM_SERVICE_URL: System 服务地址
    - INTERNAL_API_KEY: 内部 API Key

    Returns:
        SystemClient 实例，如果环境变量不完整则返回 None
    """
    system_url = os.getenv('SYSTEM_SERVICE_URL')
    internal_key = os.getenv('INTERNAL_API_KEY')

    if not system_url or not internal_key:
        print("⚠️  缺少环境变量 SYSTEM_SERVICE_URL 或 INTERNAL_API_KEY，跳过模块注册")
        return None

    return SystemClient(system_url, internal_key)

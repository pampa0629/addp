"""
模块注册与心跳管理
"""
import asyncio
import sys
from typing import Optional
from clients.system_client import SystemClient


class ModuleRegistry:
    """模块注册与心跳管理器"""

    def __init__(
        self,
        system_client: SystemClient,
        module_name: str,
        module_url: str,
        route_prefix: str,
        health_check_url: str,
        heartbeat_interval: int = 30
    ):
        """
        初始化模块注册管理器

        Args:
            system_client: System 服务客户端
            module_name: 模块名称
            module_url: 模块地址
            route_prefix: 路由前缀
            health_check_url: 健康检查地址
            heartbeat_interval: 心跳间隔（秒），默认 30 秒
        """
        self.system_client = system_client
        self.module_name = module_name
        self.module_url = module_url
        self.route_prefix = route_prefix
        self.health_check_url = health_check_url
        self.heartbeat_interval = heartbeat_interval
        self._heartbeat_task: Optional[asyncio.Task] = None
        self._running = False

    def register(self) -> bool:
        """
        注册模块到 System

        Returns:
            是否注册成功
        """
        print(f"📝 注册模块到 System: {self.module_name}")
        return self.system_client.register_module(
            module_name=self.module_name,
            module_url=self.module_url,
            route_prefix=self.route_prefix,
            health_check_url=self.health_check_url,
            metadata={
                "module": self.module_name
            }
        )

    async def start_heartbeat(self):
        """启动心跳任务"""
        if self._running:
            print("⚠️  心跳任务已在运行")
            return

        self._running = True
        print(f"💓 启动心跳任务（间隔 {self.heartbeat_interval} 秒）")

        while self._running:
            try:
                await asyncio.sleep(self.heartbeat_interval)
                # 在线程池中执行同步的心跳请求
                loop = asyncio.get_event_loop()
                await loop.run_in_executor(
                    None,
                    self.system_client.send_heartbeat,
                    self.module_name
                )
            except asyncio.CancelledError:
                print("💓 心跳任务被取消")
                break
            except Exception as e:
                print(f"⚠️  心跳发送失败: {str(e)}")
                # 继续运行，不中断心跳

    def stop_heartbeat(self):
        """停止心跳任务并关闭客户端"""
        self._running = False
        if self._heartbeat_task:
            self._heartbeat_task.cancel()
        # 关闭 HTTP 客户端
        self.system_client.close()
        print("✅ 模块注册心跳已停止")


async def register_module_on_startup(
    module_name: str,
    module_url: str,
    route_prefix: str,
    health_check_url: str,
    system_client: Optional[SystemClient] = None
) -> Optional[ModuleRegistry]:
    """
    在应用启动时注册模块

    Args:
        module_name: 模块名称
        module_url: 模块地址
        route_prefix: 路由前缀
        health_check_url: 健康检查地址
        system_client: System 客户端（如果为 None，则尝试从环境变量创建）

    Returns:
        ModuleRegistry 实例，如果注册失败则返回 None
    """
    if system_client is None:
        from clients.system_client import create_system_client_from_env
        system_client = create_system_client_from_env()

    if system_client is None:
        print("⚠️  无法创建 System 客户端，跳过模块注册")
        return None

    registry = ModuleRegistry(
        system_client=system_client,
        module_name=module_name,
        module_url=module_url,
        route_prefix=route_prefix,
        health_check_url=health_check_url
    )

    # 注册模块
    success = registry.register()
    if not success:
        print("⚠️  模块注册失败，但应用将继续运行")
        return None

    # 启动心跳任务（后台运行）
    asyncio.create_task(registry.start_heartbeat())

    return registry

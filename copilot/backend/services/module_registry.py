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
        self.system_client = system_client
        self.module_name = module_name
        self.module_url = module_url
        self.route_prefix = route_prefix
        self.health_check_url = health_check_url
        self.heartbeat_interval = heartbeat_interval
        self._heartbeat_task: Optional[asyncio.Task] = None
        self._running = False
        self._registered = False

    def register(self) -> bool:
        """注册模块到 System，最多重试 3 次"""
        for attempt in range(1, 4):
            print(f"📝 注册模块到 System: {self.module_name} (尝试 {attempt}/3)")
            success = self.system_client.register_module(
                module_name=self.module_name,
                module_url=self.module_url,
                route_prefix=self.route_prefix,
                health_check_url=self.health_check_url,
                metadata={"module": self.module_name}
            )
            if success:
                self._registered = True
                return True
            if attempt < 3:
                import time
                time.sleep(attempt * 5)
        return False

    def _try_register_sync(self) -> bool:
        """同步尝试注册一次"""
        success = self.system_client.register_module(
            module_name=self.module_name,
            module_url=self.module_url,
            route_prefix=self.route_prefix,
            health_check_url=self.health_check_url,
            metadata={"module": self.module_name}
        )
        if success:
            self._registered = True
            print(f"✅ {self.module_name} 模块注册成功")
        return success

    async def start_heartbeat(self):
        """启动心跳任务，连续失败时自动重新注册"""
        if self._running:
            print("⚠️  心跳任务已在运行")
            return

        self._running = True
        print(f"💓 启动心跳任务（间隔 {self.heartbeat_interval} 秒）")

        consecutive_failures = 0
        while self._running:
            try:
                await asyncio.sleep(self.heartbeat_interval)
                loop = asyncio.get_event_loop()
                success = await loop.run_in_executor(
                    None,
                    self.system_client.send_heartbeat,
                    self.module_name
                )
                if success:
                    if not self._registered:
                        print(f"✅ {self.module_name} 心跳恢复正常")
                        self._registered = True
                    consecutive_failures = 0
                else:
                    consecutive_failures += 1
            except asyncio.CancelledError:
                print("💓 心跳任务被取消")
                break
            except Exception as e:
                consecutive_failures += 1
                print(f"⚠️  心跳发送失败: {str(e)}")

            if consecutive_failures >= 3:
                print(f"⚠️  心跳连续失败 {consecutive_failures} 次，尝试重新注册...")
                success = await asyncio.get_event_loop().run_in_executor(
                    None, self._try_register_sync
                )
                if success:
                    consecutive_failures = 0
                else:
                    await asyncio.sleep(20)

    def stop_heartbeat(self):
        """停止心跳任务并关闭客户端"""
        self._running = False
        if self._heartbeat_task:
            self._heartbeat_task.cancel()
        self.system_client.close()
        print("✅ 模块注册心跳已停止")


async def register_module_on_startup(
    module_name: str,
    module_url: str,
    route_prefix: str,
    health_check_url: str,
    system_client: Optional[SystemClient] = None
) -> Optional[ModuleRegistry]:
    """在应用启动时注册模块，注册失败也会启动心跳（心跳中会重新注册）"""
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

    # 注册模块（失败也继续，心跳中会重试）
    registry.register()

    # 启动心跳任务（后台运行）
    asyncio.create_task(registry.start_heartbeat())

    return registry

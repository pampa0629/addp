"""
ADDP 基础客户端 - 向后兼容别名

实际实现已迁移到 common-python (addp_common.client.base)
"""
from addp_common.client.base import BaseClient

# 向后兼容别名
AddpBaseClient = BaseClient
ADDPBaseClient = BaseClient

__all__ = ["BaseClient", "AddpBaseClient", "ADDPBaseClient"]

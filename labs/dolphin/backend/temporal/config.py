"""
Temporal Configuration
配置管理
"""

import os
from dataclasses import dataclass


@dataclass
class TemporalConfig:
    """Temporal 服务配置"""

    # Temporal Server
    temporal_host: str = os.getenv("TEMPORAL_HOST", "localhost:7233")
    temporal_namespace: str = os.getenv("TEMPORAL_NAMESPACE", "default")

    # Task Queue
    task_queue: str = os.getenv("TEMPORAL_TASK_QUEUE", "spatial-analysis")

    # Worker Configuration
    max_concurrent_activities: int = int(os.getenv("TEMPORAL_MAX_ACTIVITIES", "10"))
    max_concurrent_workflows: int = int(os.getenv("TEMPORAL_MAX_WORKFLOWS", "5"))

    # Logging
    log_level: str = os.getenv("LOG_LEVEL", "INFO")


# 全局配置实例
config = TemporalConfig()

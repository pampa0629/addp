"""Transfer task definitions; creating a definition never starts execution."""
from .base import BaseClient


class TransferClient(BaseClient):
    async def create_task(self, *, name: str, config: dict,
                          description: str = "", batch_size: int = 1000) -> dict:
        response = await self.post("/api/v1/transfer/task-definitions", json={
            "name": name, "description": description, "task_type": "sync",
            "config": config, "batch_size": batch_size,
            "schedule": "", "enabled": False, "auto_scan_metadata": False,
        })
        if not isinstance(response, dict):
            raise ValueError("transfer task creation response must be an object")
        return {key: response[key] for key in (
            "id", "name", "task_type", "status", "desired_state", "enabled", "schedule",
        ) if key in response}

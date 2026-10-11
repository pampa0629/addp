"""Transfer task definitions; creating a definition never starts execution."""
from .base import BaseClient


class TransferClient(BaseClient):
    @staticmethod
    def _task_request(*, name: str, config: dict,
                      description: str, batch_size: int) -> dict:
        return {
            "name": name, "description": description, "task_type": "sync",
            "config": config, "batch_size": batch_size,
            "schedule": "", "enabled": False, "auto_scan_metadata": False,
        }

    async def check_task_config(self, *, name: str, config: dict,
                                description: str = "", batch_size: int = 1000) -> dict:
        """Check a stopped task definition without saving or executing it.

        Return the complete owner report, including unsupported/unknown results
        and unchecked runtime requirements. A report is not execution approval.
        """
        response = await self.post(
            "/api/v1/transfer/task-config-checks",
            json=self._task_request(name=name, config=config,
                                    description=description, batch_size=batch_size),
        )
        if not isinstance(response, dict):
            raise ValueError("transfer task config check response must be an object")
        return response

    async def create_task(self, *, name: str, config: dict,
                          description: str = "", batch_size: int = 1000) -> dict:
        response = await self.post(
            "/api/v1/transfer/task-definitions",
            json=self._task_request(name=name, config=config,
                                    description=description, batch_size=batch_size),
        )
        if not isinstance(response, dict):
            raise ValueError("transfer task creation response must be an object")
        return {key: response[key] for key in (
            "id", "name", "task_type", "status", "desired_state", "enabled", "schedule",
        ) if key in response}

"""Transfer definitions, owner-approved runs and bounded execution observations."""
from .base import BaseClient


class TransferClient(BaseClient):
    @staticmethod
    def _execution_projection(response: dict) -> dict:
        result = {key: response[key] for key in (
            "execution_id", "task_id", "status", "records_read", "records_written",
            "bytes_read", "bytes_written", "start_time", "end_time",
        ) if key in response}
        details = response.get("error_details")
        diagnostic = details.get("diagnostic") if isinstance(details, dict) else None
        if isinstance(diagnostic, dict):
            result["diagnostic"] = {key: diagnostic[key] for key in (
                "schema_version", "stage", "reason", "target_change_possible", "write",
            ) if key in diagnostic}
        return result

    async def start_task(self, task_id: int, *, approval_id: str | None = None,
                         request_fingerprint: str | None = None) -> dict:
        """Request owner approval, or consume a user-approved one exactly once.

        This method never retries, approves, creates a task or enables schedules.
        An uncertain write must be reconciled rather than submitted again.
        """
        body = {} if approval_id is None and request_fingerprint is None else {
            "approval_id": approval_id, "request_fingerprint": request_fingerprint,
        }
        response = await self.post(f"/api/v1/transfer/task-definitions/{task_id}/start", json=body)
        if not isinstance(response, dict):
            raise ValueError("transfer start response must be an object")
        if response.get("status") == "approval_required":
            return {key: response[key] for key in (
                "status", "interaction_id", "request_fingerprint", "request_summary", "expires_at",
            ) if key in response}
        return self._execution_projection(response)

    async def get_execution(self, execution_id: str) -> dict:
        response = await self.get(f"/api/v1/transfer/executions/{execution_id}")
        if not isinstance(response, dict):
            raise ValueError("transfer execution response must be an object")
        return self._execution_projection(response)

    async def get_run_approval(self, approval_id: str) -> dict:
        """User-only approval status; a read does not approve or start a run."""
        response = await self.get(f"/api/v1/transfer/run-approvals/{approval_id}")
        if not isinstance(response, dict):
            raise ValueError("transfer approval response must be an object")
        return response

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

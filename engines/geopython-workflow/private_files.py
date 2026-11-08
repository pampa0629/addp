"""Execution-owned local bytes; no business storage connection or publication."""
from __future__ import annotations

import hashlib
import json
import re
import shutil
import tempfile
import threading
import time
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from uuid import UUID

from private_execution import PrivateWorkflowProcess


class PrivateFileError(ValueError):
    def __init__(self, code, status=409):
        super().__init__(code)
        self.code, self.status = code, status


@dataclass(frozen=True)
class InputFile:
    node_id: str
    size: int
    sha256: str


@dataclass(frozen=True)
class FileManifest:
    tenant_id: str
    execution_id: str
    attempt: int
    authorization_id: str
    scope_digest: str
    deadline: float
    workflow_json: str
    inputs: tuple[InputFile, ...]
    outputs: tuple[str, ...]

    @classmethod
    def parse(cls, value: dict) -> FileManifest:
        """The HTTP boundary converts the external manifest exactly once."""
        fields = {"schema_version", "tenant_id", "execution_id", "attempt", "authorization_id",
                  "scope_digest", "deadline", "workflow_json", "inputs", "outputs"}
        if not isinstance(value, dict) or set(value) != fields or value["schema_version"] != "addp.workflow.private-files/v1":
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        if any(not isinstance(value[k], str) or re.fullmatch(r"[1-9][0-9]*", value[k]) is None
               for k in ["tenant_id", "authorization_id"]):
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        try:
            identifier = UUID(value["execution_id"])
            deadline = datetime.fromisoformat(value["deadline"].replace("Z", "+00:00"))
        except (ValueError, TypeError, AttributeError):
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400) from None
        if str(identifier) != value["execution_id"] or identifier.int == 0 or deadline.utcoffset() is None:
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        if type(value["attempt"]) is not int or value["attempt"] < 1:
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        if not isinstance(value["scope_digest"], str) or re.fullmatch(r"[0-9a-f]{64}", value["scope_digest"]) is None:
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        if not isinstance(value["workflow_json"], str):
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        try:
            workflow = json.loads(value["workflow_json"])
            json.dumps(workflow, allow_nan=False)
        except ValueError:
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400) from None
        if not isinstance(workflow, dict) or not isinstance(workflow.get("tasks"), list) or not workflow["tasks"]:
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        if not isinstance(value["inputs"], list) or not value["inputs"]:
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        inputs = []
        for item in value["inputs"]:
            if not isinstance(item, dict) or set(item) != {"node_id", "size_bytes", "sha256"}:
                raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
            if (not isinstance(item["node_id"], str) or not item["node_id"] or
                    not isinstance(item["size_bytes"], str) or re.fullmatch(r"0|[1-9][0-9]*", item["size_bytes"]) is None or
                    not isinstance(item["sha256"], str) or re.fullmatch(r"[0-9a-f]{64}", item["sha256"]) is None):
                raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
            inputs.append(InputFile(item["node_id"], int(item["size_bytes"]), item["sha256"]))
        outputs = value["outputs"]
        if (not isinstance(outputs, list) or len(outputs) > 1 or
                any(not isinstance(node, str) or not node for node in outputs)):
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        ids = [part.node_id for part in inputs] + outputs
        if len(set(ids)) != len(ids):
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        from operators import OPERATORS

        input_ids, output_ids = {item.node_id for item in inputs}, set(outputs)
        seen = set()
        for task in workflow["tasks"]:
            if not isinstance(task, dict):
                raise PrivateFileError("PRIVATE_FILES_WORKFLOW_MISMATCH", 400)
            node, operator, params = task.get("id"), task.get("operator"), task.get("params", {})
            if (not isinstance(node, str) or not node or node in seen or
                    not isinstance(operator, str) or operator not in OPERATORS or not isinstance(params, dict)):
                raise PrivateFileError("PRIVATE_FILES_WORKFLOW_MISMATCH", 400)
            metadata = OPERATORS[operator]
            if "workflow" not in metadata["execution_modes"]:
                raise PrivateFileError("PRIVATE_FILES_WORKFLOW_MISMATCH", 400)
            seen.add(node)
            if operator == "load":
                allowed = node in input_ids and set(params) <= {"geom_column"}
            elif operator == "save":
                allowed = node in output_ids and set(params) <= {"input_df", "mode"} and params.get("mode") == "fail"
            else:
                # read also covers external readers. They are not admitted by
                # this private in-memory step merely because they lack writes.
                storage_params = {"connection_info", "path", "access_plan"}
                allowed = (set(metadata["effects"]) == {"read"} and
                           "raster" not in (metadata.get("attributes") or {}).get("resource_groups", []) and
                           not storage_params.intersection(p["name"] for p in metadata["param_schema"]) and
                           not storage_params.intersection(params))
            if not allowed or (node in input_ids and operator != "load") or (node in output_ids and operator != "save"):
                raise PrivateFileError("PRIVATE_FILES_WORKFLOW_MISMATCH", 400)
        if not input_ids.union(output_ids) <= seen:
            raise PrivateFileError("PRIVATE_FILES_WORKFLOW_MISMATCH", 400)
        return cls(value["tenant_id"], value["execution_id"], value["attempt"], value["authorization_id"],
                   value["scope_digest"], deadline.timestamp(), value["workflow_json"], tuple(inputs), tuple(outputs))


class PrivateFiles:
    """One creator's execution files, addressed by ordinal, never caller paths."""

    def __init__(self, manifest: FileManifest, creator: tuple[str, str], *, parent=None):
        if creator[1] != manifest.tenant_id:
            raise PrivateFileError("PRIVATE_FILES_NOT_FOUND", 404)
        if time.time() >= manifest.deadline:
            raise PrivateFileError("PRIVATE_FILES_CLOSED")
        self.manifest = manifest
        self.creator = creator
        self.directory = Path(tempfile.mkdtemp(prefix="addp-workflow-", dir=parent))
        self._lock = threading.RLock()
        self._inputs = ["missing"] * len(manifest.inputs)
        self._active_transfers = 0
        self._closing = False
        self._cleaned = False
        self.worker = None
        self._outputs = []

    def _admit(self, creator, *, active=True):
        if creator != self.creator:
            raise PrivateFileError("PRIVATE_FILES_NOT_FOUND", 404)
        if active and (self._closing or time.time() >= self.manifest.deadline):
            raise PrivateFileError("PRIVATE_FILES_CLOSED")

    def put_input(self, creator, index: int, stream):
        with self._lock:
            self._admit(creator)
            if type(index) is not int or not 0 <= index < len(self.manifest.inputs):
                raise PrivateFileError("PRIVATE_FILES_NOT_FOUND", 404)
            if self.worker is not None or self._inputs[index] != "missing":
                raise PrivateFileError("PRIVATE_FILES_INPUT_ALREADY_CLAIMED")
            self._inputs[index] = "receiving"
            self._active_transfers += 1
        part = self.manifest.inputs[index]
        pending = self.directory / f"input-{index}.pending"
        digest, remaining = hashlib.sha256(), part.size
        try:
            with pending.open("xb") as target:
                while remaining:
                    if time.time() >= self.manifest.deadline or self._closing:
                        raise PrivateFileError("PRIVATE_FILES_CLOSED")
                    data = stream.read(min(remaining, 64 * 1024))
                    if not data:
                        raise PrivateFileError("PRIVATE_FILES_INPUT_MISMATCH", 400)
                    target.write(data)
                    digest.update(data)
                    remaining -= len(data)
                with self._lock:
                    self._admit(creator)
                if stream.read(1) or digest.hexdigest() != part.sha256:
                    raise PrivateFileError("PRIVATE_FILES_INPUT_MISMATCH", 400)
            with self._lock:
                self._admit(creator)
                pending.rename(self.directory / f"input-{index}.gpkg")
                self._inputs[index] = "ready"
        except Exception:
            with self._lock:
                self._inputs[index] = "failed"
            raise
        finally:
            with self._lock:
                self._active_transfers -= 1

    def start(self, creator, language: str):
        with self._lock:
            self._admit(creator)
            if self.worker is not None:
                raise PrivateFileError("PRIVATE_FILES_ALREADY_DISPATCHED")
            if any(state != "ready" for state in self._inputs):
                raise PrivateFileError("PRIVATE_FILES_INPUTS_INCOMPLETE")
            workflow = json.loads(self.manifest.workflow_json)
            inputs = {part.node_id: i for i, part in enumerate(self.manifest.inputs)}
            outputs = {node: i for i, node in enumerate(self.manifest.outputs)}
            for task in workflow["tasks"]:
                node, operator, params = task["id"], task["operator"], task.get("params", {})
                if operator == "load":
                    params = {**params, "path": f"input-{inputs[node]}.gpkg"}
                elif operator == "save":
                    params = {**params, "path": f"output-{outputs[node]}.gpkg"}
                else:
                    continue
                task["params"] = {**params, "connection_info": {"mount_path": str(self.directory)}}
            self.worker = PrivateWorkflowProcess(self.directory, workflow, language, self.manifest.deadline)
            self.worker.start()

    def status(self, creator):
        with self._lock:
            self._admit(creator, active=False)
            report = self.worker.snapshot() if self.worker else {"status": "pending"}
            if report["status"] == "success" and not self._outputs and self.manifest.outputs and not self._cleaned:
                for index, node in enumerate(self.manifest.outputs):
                    digest = hashlib.sha256()
                    with (self.directory / f"output-{index}.gpkg").open("rb") as stream:
                        for data in iter(lambda: stream.read(64 * 1024), b""):
                            digest.update(data)
                        size = stream.tell()
                    self._outputs.append({"node_id": node, "size_bytes": str(size), "sha256": digest.hexdigest()})
            return {"execution_id": self.manifest.execution_id, "attempt": self.manifest.attempt,
                    "scope_digest": self.manifest.scope_digest, "inputs": list(self._inputs),
                    "execution": report, "outputs": [dict(x) for x in self._outputs],
                    "cleanup_pending": self._closing and not self._cleaned}

    @contextmanager
    def open_output(self, creator, index: int):
        with self._lock:
            self._admit(creator)
            if type(index) is not int or not 0 <= index < len(self.manifest.outputs):
                raise PrivateFileError("PRIVATE_FILES_NOT_FOUND", 404)
            if self.status(creator)["execution"]["status"] != "success":
                raise PrivateFileError("PRIVATE_FILES_OUTPUT_UNAVAILABLE")
            stream = (self.directory / f"output-{index}.gpkg").open("rb")
            self._active_transfers += 1
        try:
            yield stream
        finally:
            stream.close()
            with self._lock:
                self._active_transfers -= 1

    def cleanup(self, creator):
        with self._lock:
            self._admit(creator, active=False)
            self._closing = True
            if self.worker:
                self.worker.cancel()
                if not self.worker.stopped():
                    return {"cleanup": "pending"}
            if self._active_transfers:
                return {"cleanup": "pending"}
            if not self._cleaned:
                try:
                    shutil.rmtree(self.directory)
                except OSError:
                    return {"cleanup": "pending", "error_code": "PRIVATE_FILES_CLEANUP_FAILED"}
                self._cleaned = True
            return {"cleanup": "passed"}

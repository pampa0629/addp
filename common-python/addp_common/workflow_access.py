from __future__ import annotations

import mimetypes
import os
import shutil
import tempfile
import uuid
from pathlib import Path
from typing import Any


SCHEMA_VERSION = "addp.workflow.access-plan/v1"


class WorkflowAccessError(ValueError):
    pass


def require_access_plan(params: dict[str, Any]) -> dict[str, Any]:
    plan = params.get("access_plan")
    if not isinstance(plan, dict):
        raise WorkflowAccessError("access_plan is required")
    if plan.get("schema_version") != SCHEMA_VERSION:
        raise WorkflowAccessError(f"access_plan.schema_version must be {SCHEMA_VERSION}")
    source = _required_object(plan, "source")
    target = _required_object(plan, "target")
    _validate_resource(source, "source")
    _validate_resource(target, "target")
    if target.get("write_mode") not in {"create", "replace"}:
        raise WorkflowAccessError("access_plan.target.write_mode must be create or replace")
    if not _text(target.get("name")):
        raise WorkflowAccessError("access_plan.target.name is required")
    return plan


def require_source_plan(params: dict[str, Any]) -> dict[str, Any]:
    plan = params.get("access_plan")
    if not isinstance(plan, dict):
        raise WorkflowAccessError("access_plan is required")
    if plan.get("schema_version") != SCHEMA_VERSION:
        raise WorkflowAccessError(f"access_plan.schema_version must be {SCHEMA_VERSION}")
    source = _required_object(plan, "source")
    _validate_resource(source, "source")
    if "target" in plan:
        raise WorkflowAccessError("source-only access plan must not contain target")
    return plan


def require_target_plan(params: dict[str, Any]) -> dict[str, Any]:
    plan = params.get("access_plan")
    if not isinstance(plan, dict):
        raise WorkflowAccessError("access_plan is required")
    if plan.get("schema_version") != SCHEMA_VERSION:
        raise WorkflowAccessError(f"access_plan.schema_version must be {SCHEMA_VERSION}")
    target = _required_object(plan, "target")
    _validate_resource(target, "target")
    if target.get("write_mode") not in {"create", "replace"}:
        raise WorkflowAccessError("access_plan.target.write_mode must be create or replace")
    if not _text(target.get("name")):
        raise WorkflowAccessError("access_plan.target.name is required")
    if "source" in plan:
        raise WorkflowAccessError("target-only access plan must not contain source")
    return plan


def source_format(plan: dict[str, Any]) -> str:
    return _text(_required_object(plan, "source").get("format")).lower()


def target_name(plan: dict[str, Any]) -> str:
    return _text(_required_object(plan, "target").get("name"))


def stage_source_file(plan: dict[str, Any], work_dir: Path) -> Path:
    source = _required_object(plan, "source")
    if source.get("kind") != "file":
        raise WorkflowAccessError("access_plan.source.kind must be file")
    access = _required_object(source, "access")
    if access.get("method") == "mounted_path":
        path = Path(_required_text(access, "path"))
        if not path.is_file():
            raise WorkflowAccessError(f"source file not found: {path}")
        return path
    client = _object_store_client(access)
    bucket = _required_text(access, "bucket")
    object_name = _required_text(access, "object").strip("/")
    entrypoint = _text(source.get("entrypoint")) or Path(object_name).name
    path = work_dir / "source" / Path(entrypoint).name
    path.parent.mkdir(parents=True, exist_ok=True)
    try:
        client.fget_object(bucket, object_name, str(path))
    except Exception as exc:
        raise WorkflowAccessError(f"download source object failed: {bucket}/{object_name}: {exc}") from exc
    return path


def stage_source_directory(plan: dict[str, Any], work_dir: Path) -> Path:
    source = _required_object(plan, "source")
    if source.get("kind") != "directory":
        raise WorkflowAccessError("access_plan.source.kind must be directory")
    access = _required_object(source, "access")
    if access.get("method") == "mounted_path":
        path = Path(_required_text(access, "path"))
        if not path.is_dir():
            raise WorkflowAccessError(f"source directory not found: {path}")
        return path

    client = _object_store_client(access)
    bucket = _required_text(access, "bucket")
    prefix = _required_text(access, "prefix").strip("/")
    root = work_dir / "source"
    root.mkdir(parents=True, exist_ok=True)
    objects = [item for item in client.list_objects(bucket, prefix=prefix + "/", recursive=True) if not getattr(item, "is_dir", False)]
    if not objects:
        raise WorkflowAccessError(f"source prefix is empty: {bucket}/{prefix}")
    prefix_with_slash = prefix + "/"
    for item in objects:
        object_name = _text(getattr(item, "object_name", ""))
        if not object_name:
            continue
        relative = object_name[len(prefix_with_slash):] if object_name.startswith(prefix_with_slash) else Path(object_name).name
        target = root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        client.fget_object(bucket, object_name, str(target))
    return root


def publish_target_file(path: Path, plan: dict[str, Any]) -> dict[str, Any]:
    if not path.is_file():
        raise WorkflowAccessError(f"output file not found: {path}")
    target = _required_object(plan, "target")
    if target.get("kind") != "file":
        raise WorkflowAccessError("access_plan.target.kind must be file")
    access = _required_object(target, "access")
    write_mode = _required_text(target, "write_mode")
    content_type = _text(target.get("content_type")) or _content_type(path.name)
    if access.get("method") == "mounted_path":
        destination = Path(_required_text(access, "path"))
        _publish_local_file(path, destination, write_mode)
        return {
            "method": "mounted_path",
            "path": str(destination),
            "locator": str(destination),
            "uploaded_files": 1,
            "uploaded_bytes": destination.stat().st_size,
            "content_type": content_type,
        }

    client = _object_store_client(access)
    bucket = _required_text(access, "bucket")
    object_name = _required_text(access, "object").strip("/")
    _ensure_bucket(client, bucket)
    if write_mode == "create" and _object_exists(client, bucket, object_name):
        raise WorkflowAccessError(f"target object already exists: {bucket}/{object_name}")
    client.fput_object(bucket, object_name, str(path), content_type=content_type)
    return {
        "method": "object_store",
        "bucket": bucket,
        "object_name": object_name,
        "object_uri": f"s3://{bucket}/{object_name}",
        "uploaded_files": 1,
        "uploaded_bytes": path.stat().st_size,
        "content_type": content_type,
    }


def publish_target_directory(root: Path, plan: dict[str, Any], *, completion_marker: str) -> dict[str, Any]:
    if not root.is_dir():
        raise WorkflowAccessError(f"output directory not found: {root}")
    marker = root / completion_marker
    if not marker.is_file():
        raise WorkflowAccessError(f"output completion marker not found: {marker}")
    target = _required_object(plan, "target")
    if target.get("kind") != "directory":
        raise WorkflowAccessError("access_plan.target.kind must be directory")
    access = _required_object(target, "access")
    write_mode = _required_text(target, "write_mode")
    if access.get("method") == "mounted_path":
        destination = Path(_required_text(access, "path"))
        _publish_local_directory(root, destination, write_mode)
        return {
            "method": "mounted_path",
            "path": str(destination),
            "locator": str(destination),
            "uploaded_files": sum(1 for item in destination.rglob("*") if item.is_file()),
            "uploaded_bytes": sum(item.stat().st_size for item in destination.rglob("*") if item.is_file()),
            "completion_marker": completion_marker,
        }

    client = _object_store_client(access)
    bucket = _required_text(access, "bucket")
    prefix = _required_text(access, "prefix").strip("/")
    _ensure_bucket(client, bucket)
    existing = {
        _text(getattr(item, "object_name", ""))
        for item in client.list_objects(bucket, prefix=prefix + "/", recursive=True)
        if not getattr(item, "is_dir", False)
    }
    existing.discard("")
    if write_mode == "create" and existing:
        raise WorkflowAccessError(f"target prefix already exists: {bucket}/{prefix}")
    marker_object = f"{prefix}/{completion_marker}"
    if write_mode == "replace" and marker_object in existing:
        client.remove_object(bucket, marker_object)

    files = sorted(item for item in root.rglob("*") if item.is_file())
    ordered = [item for item in files if item != marker] + [marker]
    written: set[str] = set()
    uploaded_bytes = 0
    for item in ordered:
        relative = item.relative_to(root).as_posix()
        object_name = f"{prefix}/{relative}"
        client.fput_object(bucket, object_name, str(item), content_type=_content_type(object_name))
        written.add(object_name)
        uploaded_bytes += item.stat().st_size
    if write_mode == "replace":
        for stale in sorted(existing - written - {marker_object}):
            client.remove_object(bucket, stale)
    return {
        "method": "object_store",
        "bucket": bucket,
        "prefix": prefix,
        "target_root_uri": f"s3://{bucket}/{prefix}",
        "uploaded_files": len(written),
        "uploaded_bytes": uploaded_bytes,
        "completion_marker": completion_marker,
    }


def _validate_resource(resource: dict[str, Any], label: str) -> None:
    if resource.get("kind") not in {"file", "directory"}:
        raise WorkflowAccessError(f"access_plan.{label}.kind must be file or directory")
    if not _text(resource.get("format")):
        raise WorkflowAccessError(f"access_plan.{label}.format is required")
    access = _required_object(resource, "access")
    method = access.get("method")
    if method == "mounted_path":
        _required_text(access, "path")
        return
    if method != "object_store":
        raise WorkflowAccessError(f"access_plan.{label}.access.method must be mounted_path or object_store")
    for key in ("endpoint", "access_key", "secret_key", "bucket"):
        _required_text(access, key)
    _required_text(access, "object" if resource.get("kind") == "file" else "prefix")


def _publish_local_file(source: Path, destination: Path, write_mode: str) -> None:
    if write_mode == "create" and destination.exists():
        raise WorkflowAccessError(f"target file already exists: {destination}")
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.parent / f".{destination.name}.addp-{uuid.uuid4().hex}.tmp"
    try:
        shutil.copy2(source, temporary)
        os.replace(temporary, destination)
    finally:
        temporary.unlink(missing_ok=True)


def _publish_local_directory(source: Path, destination: Path, write_mode: str) -> None:
    if write_mode == "create" and destination.exists():
        raise WorkflowAccessError(f"target directory already exists: {destination}")
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.parent / f".{destination.name}.addp-{uuid.uuid4().hex}.tmp"
    backup = destination.parent / f".{destination.name}.addp-{uuid.uuid4().hex}.bak"
    shutil.copytree(source, temporary)
    try:
        if destination.exists():
            os.replace(destination, backup)
        os.replace(temporary, destination)
        if backup.exists():
            shutil.rmtree(backup)
    except Exception:
        if backup.exists() and not destination.exists():
            os.replace(backup, destination)
        raise
    finally:
        shutil.rmtree(temporary, ignore_errors=True)


def _object_store_client(access: dict[str, Any]):
    from minio import Minio

    endpoint = _required_text(access, "endpoint")
    if endpoint.startswith("http://"):
        endpoint = endpoint[len("http://"):]
    elif endpoint.startswith("https://"):
        endpoint = endpoint[len("https://"):]
    return Minio(
        endpoint.strip("/"),
        access_key=_required_text(access, "access_key"),
        secret_key=_required_text(access, "secret_key"),
        secure=bool(access.get("use_ssl")),
    )


def _ensure_bucket(client, bucket: str) -> None:
    if not client.bucket_exists(bucket):
        client.make_bucket(bucket)


def _object_exists(client, bucket: str, object_name: str) -> bool:
    try:
        client.stat_object(bucket, object_name)
        return True
    except Exception as exc:
        code = _text(getattr(exc, "code", ""))
        if code in {"NoSuchKey", "NoSuchObject", "NoSuchBucket", "XMinioInvalidObjectName"}:
            return False
        status = getattr(exc, "status", None)
        if status == 404:
            return False
        raise


def _required_object(parent: dict[str, Any], key: str) -> dict[str, Any]:
    value = parent.get(key)
    if not isinstance(value, dict):
        raise WorkflowAccessError(f"{key} must be an object")
    return value


def _required_text(parent: dict[str, Any], key: str) -> str:
    value = _text(parent.get(key))
    if not value:
        raise WorkflowAccessError(f"{key} is required")
    return value


def _text(value: Any) -> str:
    return value.strip() if isinstance(value, str) else ""


def _content_type(name: str) -> str:
    return mimetypes.guess_type(name)[0] or "application/octet-stream"

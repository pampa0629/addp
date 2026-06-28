from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable
from urllib.parse import urlparse

from minio import Minio


ENGINE_TYPE = "model3d_workflow"
ENGINE_ROOT = Path(__file__).resolve().parent
DEFAULT_CONVERTER_BIN = str(ENGINE_ROOT / "bin" / "_3dtile")
CONVERTER_ENV = "MODEL3D_CONVERTER_BIN"
TILE_EXTENSIONS = {".b3dm", ".i3dm", ".pnts", ".cmpt", ".glb", ".gltf"}
TILESET_REF = "tileset.json"


class ConverterError(Exception):
    def __init__(
        self,
        error_code: str,
        message: str,
        *,
        details: str | None = None,
        http_status: int = 400,
    ) -> None:
        super().__init__(message)
        self.error_code = error_code
        self.message = message
        self.details = details
        self.http_status = http_status


@dataclass
class CommandResult:
    returncode: int
    stdout: str = ""
    stderr: str = ""


CommandRunner = Callable[[list[str], int | None], CommandResult]


def converter_status(env: dict[str, str] | None = None) -> dict[str, Any]:
    converter = _converter_bin(env)
    available = _converter_available(converter, env)
    detail = "" if available else _converter_unavailable_detail(converter)
    return {
        "name": "_3dtile",
        "env": CONVERTER_ENV,
        "path": converter,
        "available": available,
        "binding": "model3d_workflow",
        "details": detail,
    }


def list_operators() -> list[dict[str, Any]]:
    return [
        {
            "id": "osgb_to_glb",
            "name": "osgb_to_glb",
            "display_name": "OSGB 转 GLB",
            "engine_type": ENGINE_TYPE,
            "category": "三维模型转换",
            "category_path": ["三维模型转换", "快显"],
            "description": "将单个 OSGB 文件转换为前端可快速预览的 GLB artifact。",
            "execution_modes": ["direct"],
            "parameters": [
                {
                    "name": "access_plan",
                    "type": "object",
                    "required": True,
                    "description": "源 OSGB 文件访问计划和 GLB artifact 对象存储发布计划。",
                },
                {
                    "name": "options",
                    "type": "object",
                    "required": False,
                    "description": "转换器私有选项，第一版透传给运行时审计，不拼接为命令参数。",
                },
            ],
            "output_ports": [
                {
                    "name": "result",
                    "type": "object",
                    "description": "GLB artifact 的对象引用、大小、发布结果和转换器信息。",
                    "is_default": True,
                }
            ],
        },
        {
            "id": "osgb_scene_to_3dtiles",
            "name": "osgb_scene_to_3dtiles",
            "display_name": "OSGB Scene 转 3D Tiles",
            "engine_type": ENGINE_TYPE,
            "category": "三维模型转换",
            "category_path": ["三维模型转换", "倾斜摄影"],
            "description": "将一套 OSGB 倾斜摄影数据集转换为前端高效预览的 3D Tiles 数据集。",
            "execution_modes": ["direct"],
            "parameters": [
                {
                    "name": "access_plan",
                    "type": "object",
                    "required": True,
                    "description": "OSGB Scene 源目录和 3D Tiles 目标目录的本地访问计划。",
                },
                {
                    "name": "tiles",
                    "type": "object",
                    "required": False,
                    "description": "目标瓦片格式信息。",
                },
                {
                    "name": "options",
                    "type": "object",
                    "required": False,
                    "description": "转换器私有选项，第一版透传给运行时审计，不拼接为命令参数。",
                },
            ],
            "output_ports": [
                {
                    "name": "result",
                    "type": "object",
                    "description": "3D Tiles 数据集的 tileset 引用、瓦片数量和转换器信息。",
                    "is_default": True,
                }
            ],
        },
    ]


def get_operator(name: str) -> dict[str, Any] | None:
    return next((operator for operator in list_operators() if operator["name"] == name), None)


def invoke_operator(
    name: str,
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    if get_operator(name) is None:
        raise ConverterError("OPERATOR_NOT_FOUND", f"Operator not found: {name}", http_status=404)
    if name == "osgb_to_glb":
        return osgb_to_glb(params, runner=runner, env=env, timeout_seconds=timeout_seconds)
    if name == "osgb_scene_to_3dtiles":
        return osgb_scene_to_3dtiles(params, runner=runner, env=env, timeout_seconds=timeout_seconds)
    raise ConverterError("OPERATOR_NOT_FOUND", f"Operator not found: {name}", http_status=404)


def osgb_to_glb(
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    access_plan = _required_object(params, "access_plan")
    source = _required_object(access_plan, "source")
    target = _required_object(access_plan, "target")
    source_path = _first_text(source, "local_path", "root_uri")
    publish = _required_object(target, "publish")
    file_name = _required_text(target, "file_name")

    if not source_path:
        raise ConverterError("INVALID_PARAMS", "access_plan.source.local_path or root_uri is required")
    if _text(publish.get("method")) != "object_store":
        raise ConverterError("INVALID_PARAMS", "access_plan.target.publish.method must be object_store")

    temp_dir = Path(tempfile.mkdtemp(prefix="addp-model3d-glb-"))
    target_file = temp_dir / file_name

    try:
        converter = _converter_bin(env)
        command = [converter, "-f", "gltf", "-i", source_path, "-o", str(target_file)]
        result = _run_converter(command, runner=runner, env=env, timeout_seconds=timeout_seconds)
        if not target_file.exists():
            raise ConverterError(
                "OUTPUT_NOT_FOUND",
                "GLB output file was not generated",
                details=str(target_file),
                http_status=500,
            )

        publish_result = publish_object_store_file(target_file, publish)
        return {
            "glb_uri": publish_result["object_uri"],
            "glb_ref": publish_result["object_name"],
            "size_bytes": publish_result["uploaded_bytes"],
            "publish": publish_result,
            "converter": converter,
            "command": _redact_command(command),
            "stdout": result.stdout,
            "stderr": result.stderr,
        }
    finally:
        shutil.rmtree(temp_dir, ignore_errors=True)


def osgb_scene_to_3dtiles(
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    access_plan = _required_object(params, "access_plan")
    source = _required_object(access_plan, "source")
    target = _required_object(access_plan, "target")
    source_root = _text(source.get("root_uri"))
    stage = _optional_object(source, "stage")
    target_root = _text(target.get("dataset_root_uri"))
    publish = _optional_object(target, "publish")

    if not source_root:
        if _text(stage.get("method")) != "object_store":
            raise ConverterError("INVALID_PARAMS", "access_plan.source.root_uri is required")
        source_root = tempfile.mkdtemp(prefix="addp-model3d-source-")
    if not target_root:
        if _text(publish.get("method")) != "object_store":
            raise ConverterError("INVALID_PARAMS", "access_plan.target.dataset_root_uri is required")
        target_root = tempfile.mkdtemp(prefix="addp-model3d-tiles-")

    source_dir = Path(source_root)
    target_dir = Path(target_root)
    cleanup_source = _text(stage.get("method")) == "object_store"
    cleanup_target = _text(publish.get("method")) == "object_store"
    try:
        stage_result: dict[str, Any] = {}
        if _text(stage.get("method")) == "object_store":
            stage_result = stage_object_store_directory(source_dir, stage)
        target_dir.mkdir(parents=True, exist_ok=True)

        converter = _converter_bin(env)
        command = [converter, "-f", "osgb", "-i", source_root, "-o", str(target_dir)]
        result = _run_converter(command, runner=runner, env=env, timeout_seconds=timeout_seconds)

        tileset_path = target_dir / TILESET_REF
        if not tileset_path.exists():
            raise ConverterError(
                "OUTPUT_NOT_FOUND",
                "3D Tiles tileset.json was not generated",
                details=str(tileset_path),
                http_status=500,
            )

        publish_result: dict[str, Any] = {}
        if _text(publish.get("method")) == "object_store":
            publish_result = publish_object_store_directory(target_dir, publish)

        tile_count = _count_tiles(target_dir)
        return {
            "tileset_locator": publish_result.get("tileset_locator") or str(target_dir),
            "tileset_ref": publish_result.get("tileset_ref") or TILESET_REF,
            "tile_count": tile_count,
            "target_root_uri": publish_result.get("target_root_uri") or str(target_dir),
            "stage": stage_result or None,
            "publish": publish_result or None,
            "converter": converter,
            "command": _redact_command(command),
            "stdout": result.stdout,
            "stderr": result.stderr,
        }
    finally:
        if cleanup_source:
            shutil.rmtree(source_dir, ignore_errors=True)
        if cleanup_target:
            shutil.rmtree(target_dir, ignore_errors=True)


def stage_object_store_directory(root: Path, stage: dict[str, Any]) -> dict[str, Any]:
    endpoint = _normal_endpoint(_required_text(stage, "endpoint"))
    bucket = _required_text(stage, "bucket")
    prefix = _required_text(stage, "prefix").strip("/")
    access_key = _required_text(stage, "access_key")
    secret_key = _required_text(stage, "secret_key")
    secure = bool(stage.get("use_ssl"))

    client = Minio(endpoint, access_key=access_key, secret_key=secret_key, secure=secure)
    objects = list(client.list_objects(bucket, prefix=prefix, recursive=True))
    files = [obj for obj in objects if not getattr(obj, "is_dir", False)]
    if not files:
        raise ConverterError(
            "SOURCE_NOT_FOUND",
            "object store source prefix is empty",
            details=f"{bucket}/{prefix}",
            http_status=404,
        )

    downloaded_files = 0
    downloaded_bytes = 0
    prefix_with_slash = prefix.rstrip("/") + "/"
    for obj in files:
        key = getattr(obj, "object_name", "")
        if not key or key.rstrip("/") == prefix.rstrip("/"):
            continue
        rel = key[len(prefix_with_slash) :] if key.startswith(prefix_with_slash) else Path(key).name
        target_path = root / rel
        target_path.parent.mkdir(parents=True, exist_ok=True)
        client.fget_object(bucket, key, str(target_path))
        downloaded_files += 1
        downloaded_bytes += int(getattr(obj, "size", 0) or 0)

    if downloaded_files == 0:
        raise ConverterError(
            "SOURCE_NOT_FOUND",
            "object store source prefix has no downloadable files",
            details=f"{bucket}/{prefix}",
            http_status=404,
        )
    return {
        "method": "object_store",
        "bucket": bucket,
        "prefix": prefix,
        "downloaded_files": downloaded_files,
        "downloaded_bytes": downloaded_bytes,
    }


def publish_object_store_directory(root: Path, publish: dict[str, Any]) -> dict[str, Any]:
    endpoint = _normal_endpoint(_required_text(publish, "endpoint"))
    bucket = _required_text(publish, "bucket")
    prefix = _required_text(publish, "prefix").strip("/")
    access_key = _required_text(publish, "access_key")
    secret_key = _required_text(publish, "secret_key")
    secure = bool(publish.get("use_ssl"))

    client = Minio(endpoint, access_key=access_key, secret_key=secret_key, secure=secure)
    if not client.bucket_exists(bucket):
        client.make_bucket(bucket)

    files = sorted(path for path in root.rglob("*") if path.is_file())
    tileset = root / TILESET_REF
    ordered = [path for path in files if path != tileset]
    if tileset.exists():
        ordered.append(tileset)

    uploaded_files = 0
    uploaded_bytes = 0
    for path in ordered:
        rel = path.relative_to(root).as_posix()
        object_name = f"{prefix}/{rel}" if prefix else rel
        size = path.stat().st_size
        client.fput_object(bucket, object_name, str(path), content_type=_content_type_for_object(object_name))
        uploaded_files += 1
        uploaded_bytes += size

    tileset_ref = f"{prefix}/{TILESET_REF}" if prefix else TILESET_REF
    return {
        "method": "object_store",
        "bucket": bucket,
        "prefix": prefix,
        "tileset_ref": TILESET_REF,
        "tileset_object": tileset_ref,
        "tileset_locator": _text(publish.get("locator")) or f"s3://{bucket}/{prefix}",
        "target_root_uri": f"s3://{bucket}/{prefix}",
        "uploaded_files": uploaded_files,
        "uploaded_bytes": uploaded_bytes,
    }


def publish_object_store_file(path: Path, publish: dict[str, Any]) -> dict[str, Any]:
    endpoint = _normal_endpoint(_required_text(publish, "endpoint"))
    bucket = _required_text(publish, "bucket")
    object_name = _required_text(publish, "object").strip("/")
    access_key = _required_text(publish, "access_key")
    secret_key = _required_text(publish, "secret_key")
    secure = bool(publish.get("use_ssl"))
    content_type = _text(publish.get("content_type")) or _content_type_for_object(object_name)

    if not path.exists():
        raise ConverterError(
            "OUTPUT_NOT_FOUND",
            "GLB output file was not generated",
            details=str(path),
            http_status=500,
        )

    client = Minio(endpoint, access_key=access_key, secret_key=secret_key, secure=secure)
    if not client.bucket_exists(bucket):
        client.make_bucket(bucket)

    size = path.stat().st_size
    client.fput_object(bucket, object_name, str(path), content_type=content_type)
    return {
        "method": "object_store",
        "bucket": bucket,
        "object_name": object_name,
        "object_uri": _text(publish.get("locator")) or f"s3://{bucket}/{object_name}",
        "content_type": content_type,
        "uploaded_files": 1,
        "uploaded_bytes": size,
    }


def run_command(command: list[str], timeout_seconds: int | None) -> CommandResult:
    try:
        completed = subprocess.run(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired as exc:
        raise ConverterError(
            "EXECUTION_TIMEOUT",
            "model3d converter timed out",
            details=str(exc),
            http_status=504,
        ) from exc
    return CommandResult(
        returncode=completed.returncode,
        stdout=completed.stdout,
        stderr=completed.stderr,
    )


def _run_converter(
    command: list[str],
    *,
    runner: CommandRunner | None,
    env: dict[str, str] | None,
    timeout_seconds: int | None,
) -> CommandResult:
    if runner is None:
        _ensure_converter_available(command[0], env)
        runner = run_command

    result = runner(command, timeout_seconds)
    if result.returncode != 0:
        raise ConverterError(
            "EXECUTION_FAILED",
            "model3d converter failed",
            details=(result.stderr or result.stdout or "").strip() or f"exit code {result.returncode}",
            http_status=500,
        )
    return result


def _converter_bin(env: dict[str, str] | None) -> str:
    values = env if env is not None else os.environ
    return _text(values.get(CONVERTER_ENV)) or DEFAULT_CONVERTER_BIN


def _converter_available(converter: str, env: dict[str, str] | None) -> bool:
    if not _is_explicit_file_path(converter):
        return False
    return Path(converter).is_file() and os.access(converter, os.X_OK)


def _ensure_converter_available(converter: str, env: dict[str, str] | None) -> None:
    if _converter_available(converter, env):
        return
    raise ConverterError(
        "CONVERTER_UNAVAILABLE",
        "model3d converter executable was not found",
        details=_converter_unavailable_detail(converter),
        http_status=503,
    )


def _is_explicit_file_path(converter: str) -> bool:
    return bool(
        converter
        and (
            os.path.isabs(converter)
            or os.path.sep in converter
            or (os.path.altsep is not None and os.path.altsep in converter)
        )
    )


def _converter_unavailable_detail(converter: str) -> str:
    if not _is_explicit_file_path(converter):
        return f"{CONVERTER_ENV} must point to the engine-bound _3dtile executable file, not a PATH command name: {converter}"
    return f"{converter} was not found or is not executable"


def _count_tiles(root: Path) -> int:
    return sum(1 for path in root.rglob("*") if path.is_file() and path.suffix.lower() in TILE_EXTENSIONS)


def _required_object(payload: dict[str, Any], key: str) -> dict[str, Any]:
    value = payload.get(key)
    if not isinstance(value, dict):
        raise ConverterError("INVALID_PARAMS", f"{key} must be an object")
    return value


def _optional_object(payload: dict[str, Any], key: str) -> dict[str, Any]:
    value = payload.get(key)
    return value if isinstance(value, dict) else {}


def _required_text(payload: dict[str, Any], key: str) -> str:
    value = _text(payload.get(key))
    if not value:
        raise ConverterError("INVALID_PARAMS", f"{key} is required")
    return value


def _first_text(payload: dict[str, Any], *keys: str) -> str:
    for key in keys:
        value = _text(payload.get(key))
        if value:
            return value
    return ""


def _text(value: Any) -> str:
    return value.strip() if isinstance(value, str) else ""


def _redact_command(command: list[str]) -> list[str]:
    return list(command)


def _normal_endpoint(endpoint: str) -> str:
    parsed = urlparse(endpoint)
    if parsed.scheme in {"http", "https"} and parsed.netloc:
        return parsed.netloc
    return endpoint.strip().strip("/")


def _content_type_for_object(object_name: str) -> str:
    ext = Path(object_name).suffix.lower()
    if object_name.endswith(TILESET_REF):
        return "application/vnd.ogc.3dtiles+json"
    if ext == ".json":
        return "application/json"
    if ext == ".glb":
        return "model/gltf-binary"
    if ext == ".gltf":
        return "model/gltf+json"
    return "application/octet-stream"

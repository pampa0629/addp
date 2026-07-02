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


ENGINE_TYPE = "pointcloud_workflow"
ENGINE_ROOT = Path(__file__).resolve().parent
DEFAULT_PDAL_BIN = str(ENGINE_ROOT / "bin" / "pdal")
PDAL_ENV = "POINTCLOUD_PDAL_BIN"
COPC_CONTENT_TYPE = "application/vnd.laszip+copc"


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
    pdal = _pdal_bin(env)
    available = _executable_available(pdal)
    details = "" if available else _executable_unavailable_detail(PDAL_ENV, pdal)
    return {
        "name": "pdal",
        "env": PDAL_ENV,
        "path": pdal,
        "available": available,
        "binding": ENGINE_TYPE,
        "details": details,
    }


def list_operators() -> list[dict[str, Any]]:
    return [
        _copc_operator("las_to_copc", "LAS 转 COPC", "将 LAS 点云转换为 Manager 受管 COPC 快显 artifact。", "las"),
        _copc_operator("laz_to_copc", "LAZ 转 COPC", "将 LAZ 点云转换为 Manager 受管 COPC 快显 artifact。", "laz"),
        _copc_operator("e57_to_copc", "E57 转 COPC", "将 E57 扫描点云转换为 Manager 受管 COPC 快显 artifact。", "e57"),
    ]


def _copc_operator(operator_id: str, display_name: str, description: str, source_format: str) -> dict[str, Any]:
    return {
        "id": operator_id,
        "name": operator_id,
        "display_name": display_name,
        "engine_type": ENGINE_TYPE,
        "category": "点云转换",
        "category_path": ["点云转换", "快显"],
        "description": description,
        "execution_modes": ["direct"],
        "parameters": [
            {
                "name": "access_plan",
                "type": "object",
                "required": True,
                "description": f"源 {source_format.upper()} 文件访问计划和 COPC artifact 对象存储发布计划。",
            },
            {
                "name": "options",
                "type": "object",
                "required": False,
                "description": "PDAL writers.copc 私有选项，第一版只接受受控白名单。",
            },
        ],
        "output_ports": [
            {
                "name": "result",
                "type": "object",
                "description": "COPC artifact 的对象引用、大小、发布结果和转换器信息。",
                "is_default": True,
            }
        ],
    }


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
    source_format = {
        "las_to_copc": "las",
        "laz_to_copc": "laz",
        "e57_to_copc": "e57",
    }[name]
    return point_cloud_to_copc(params, source_format=source_format, runner=runner, env=env, timeout_seconds=timeout_seconds)


def point_cloud_to_copc(
    params: dict[str, Any],
    *,
    source_format: str,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    access_plan = _required_object(params, "access_plan")
    source = _required_object(access_plan, "source")
    target = _required_object(access_plan, "target")
    options = _optional_object(params, "options")
    source_path = _first_text(source, "local_path", "root_uri")
    source_plan_format = _text(source.get("format")).lower()
    publish = _required_object(target, "publish")
    file_name = _required_text(target, "file_name")

    if not source_path:
        raise ConverterError("INVALID_PARAMS", "access_plan.source.local_path or root_uri is required")
    if source_plan_format and source_plan_format != source_format:
        raise ConverterError(
            "INVALID_PARAMS",
            f"access_plan.source.format must be {source_format}",
            details=f"source format: {source_plan_format}",
        )
    if _text(publish.get("method")) != "object_store":
        raise ConverterError("INVALID_PARAMS", "access_plan.target.publish.method must be object_store")
    if not file_name.lower().endswith(".copc.laz"):
        raise ConverterError("INVALID_PARAMS", "access_plan.target.file_name must end with .copc.laz")

    source_file = Path(source_path)
    if not source_file.is_file():
        raise ConverterError("SOURCE_NOT_FOUND", "Point cloud source file was not found", details=str(source_file), http_status=404)

    temp_dir = Path(tempfile.mkdtemp(prefix="addp-pointcloud-copc-"))
    target_file = temp_dir / file_name
    try:
        pdal = _pdal_bin(env)
        command = [pdal, "translate", str(source_file), str(target_file), "--writers.copc.forward=all"]
        command.extend(_pdal_copc_option_args(options))
        result = _run_executable(command, runner=runner, env_name=PDAL_ENV, timeout_seconds=timeout_seconds)
        if not target_file.is_file():
            raise ConverterError(
                "OUTPUT_NOT_FOUND",
                "COPC output file was not generated",
                details=str(target_file),
                http_status=500,
            )
        publish_result = publish_object_store_file(target_file, publish)
        return {
            "copc_uri": publish_result["object_uri"],
            "copc_ref": publish_result["object_name"],
            "size_bytes": publish_result["uploaded_bytes"],
            "publish": publish_result,
            "source_format": source_format,
            "target_format": "copc",
            "converter": pdal,
            "command": _redact_command(command),
            "stdout": result.stdout,
            "stderr": result.stderr,
        }
    finally:
        shutil.rmtree(temp_dir, ignore_errors=True)


def publish_object_store_file(path: Path, publish: dict[str, Any]) -> dict[str, Any]:
    endpoint = _normal_endpoint(_required_text(publish, "endpoint"))
    bucket = _required_text(publish, "bucket")
    object_name = _required_text(publish, "object").strip("/")
    access_key = _required_text(publish, "access_key")
    secret_key = _required_text(publish, "secret_key")
    secure = bool(publish.get("use_ssl"))
    content_type = _text(publish.get("content_type")) or COPC_CONTENT_TYPE

    if not path.is_file():
        raise ConverterError("OUTPUT_NOT_FOUND", "COPC output file was not generated", details=str(path), http_status=500)

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
        "uploaded_files": 1,
        "uploaded_bytes": size,
        "content_type": content_type,
    }


def _pdal_copc_option_args(options: dict[str, Any]) -> list[str]:
    args: list[str] = []
    for key in ["compression", "chunk_size", "scale_x", "scale_y", "scale_z", "offset_x", "offset_y", "offset_z"]:
        value = options.get(key)
        if value is None or value == "":
            continue
        args.append(f"--writers.copc.{key}={value}")
    return args


def _pdal_bin(env: dict[str, str] | None = None) -> str:
    values = env if env is not None else os.environ
    return _text(values.get(PDAL_ENV)) or DEFAULT_PDAL_BIN


def _executable_available(path: str) -> bool:
    path = _text(path)
    if not path:
        return False
    if Path(path).name == path:
        return False
    return os.path.isfile(path) and os.access(path, os.X_OK)


def _executable_unavailable_detail(env_name: str, path: str) -> str:
    if Path(path).name == path:
        return f"{env_name}={path!r} is not allowed; bind an absolute or engine-local executable path, not a PATH command name"
    return f"{env_name} executable is not available: {path}"


def _run_executable(
    command: list[str],
    *,
    runner: CommandRunner | None,
    env_name: str,
    timeout_seconds: int | None,
) -> CommandResult:
    executable = command[0] if command else ""
    if not _executable_available(executable):
        raise ConverterError(
            "CONVERTER_UNAVAILABLE",
            "pointcloud converter executable is not available",
            details=_executable_unavailable_detail(env_name, executable),
            http_status=503,
        )
    if runner is not None:
        result = runner(command, timeout_seconds)
    else:
        completed = subprocess.run(command, capture_output=True, text=True, timeout=timeout_seconds, check=False)
        result = CommandResult(returncode=completed.returncode, stdout=completed.stdout, stderr=completed.stderr)
    if result.returncode != 0:
        raise ConverterError(
            "CONVERSION_FAILED",
            "pointcloud conversion command failed",
            details=(result.stderr or result.stdout or "").strip(),
            http_status=500,
        )
    return result


def _required_object(data: dict[str, Any], key: str) -> dict[str, Any]:
    value = data.get(key)
    if not isinstance(value, dict):
        raise ConverterError("INVALID_PARAMS", f"{key} must be an object")
    return value


def _optional_object(data: dict[str, Any], key: str) -> dict[str, Any]:
    value = data.get(key)
    return value if isinstance(value, dict) else {}


def _required_text(data: dict[str, Any], key: str) -> str:
    value = _text(data.get(key))
    if value == "":
        raise ConverterError("INVALID_PARAMS", f"{key} is required")
    return value


def _first_text(data: dict[str, Any], *keys: str) -> str:
    for key in keys:
        value = _text(data.get(key))
        if value:
            return value
    return ""


def _text(value: Any) -> str:
    if value is None:
        return ""
    return str(value).strip()


def _normal_endpoint(endpoint: str) -> str:
    parsed = urlparse(endpoint)
    if parsed.scheme and parsed.netloc:
        return parsed.netloc
    return endpoint


def _redact_command(command: list[str]) -> list[str]:
    redacted: list[str] = []
    for part in command:
        text = str(part)
        lowered = text.lower()
        if "secret" in lowered or "access_key" in lowered or "password" in lowered:
            redacted.append("<redacted>")
        else:
            redacted.append(text)
    return redacted

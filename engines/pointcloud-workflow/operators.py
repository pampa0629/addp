from __future__ import annotations

import os
import copy
import json
import shutil
import subprocess
import tempfile
import re
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable
from urllib import error as urlerror
from urllib.parse import urlparse
from urllib import request as urlrequest

from addp_common.workflow_access import publish_target_file, require_access_plan, source_format as plan_source_format, stage_source_file, target_name
from addp_common.client import SyncOAuthServiceTokenSource


ENGINE_TYPE = "pointcloud_workflow"
ENGINE_ROOT = Path(__file__).resolve().parent
DEFAULT_PDAL_BIN = str(ENGINE_ROOT / "bin" / "pdal")
PDAL_ENV = "POINTCLOUD_PDAL_BIN"
COPC_CONTENT_TYPE = "application/vnd.laszip+copc"
OBJECT_STORE_LOOPBACK_HOST_ENV = "POINTCLOUD_OBJECT_STORE_LOOPBACK_HOST"
WORK_DIR_ENV = "POINTCLOUD_WORK_DIR"
PROGRESS_INTERVAL_ENV = "POINTCLOUD_PROGRESS_INTERVAL_SECONDS"
COPC_THREADS_ENV = "POINTCLOUD_COPC_THREADS"
DEFAULT_COPC_THREADS = 4
MAX_COPC_THREADS = 8


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
        _copc_operator("las_to_copc", "LAS 转 COPC", "将 LAS 点云转换并持久化为 COPC。", "las"),
        _copc_operator("laz_to_copc", "LAZ 转 COPC", "将 LAZ 点云转换并持久化为 COPC。", "laz"),
        _copc_operator("e57_to_copc", "E57 转 COPC", "将 E57 扫描点云转换并持久化为 COPC。", "e57"),
        _copc_operator("pcd_to_copc", "PCD 转 COPC", "将 PCD 点云转换并持久化为 COPC。", "pcd"),
        _copc_operator("xyz_to_copc", "XYZ 转 COPC", "将简单文本 XYZ 点云转换并持久化为 COPC。", "xyz"),
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
        "execution_modes": ["workflow", "direct"],
        "effects": ["read", "write"],
        "parameters": [
            {
                "name": "access_plan",
                "type": "object",
                "required": True,
                "description": f"源 {source_format.upper()} 与目标 COPC 的 addp.workflow.access-plan/v1 访问计划。",
            },
            {
                "name": "options",
                "type": "object",
                "required": False,
                "description": "PDAL writers.copc 私有选项，第一版只接受受控白名单。",
            },
            {
                "name": "progress_callback",
                "type": "object",
                "required": False,
                "description": "调用方受控的执行进度回调，必须包含当前 attempt 和 lease_token；不属于公开算子参数。",
            },
        ],
        "output_ports": [
            {
                "name": "result",
                "type": "object",
                "description": "持久化 COPC 的引用、大小、发布结果和转换器信息。",
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
        "pcd_to_copc": "pcd",
        "xyz_to_copc": "xyz",
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
    access_plan = require_access_plan(params)
    runtime_access_plan = _runtime_access_plan(access_plan, env)
    source = _required_object(access_plan, "source")
    options = _optional_object(params, "options")
    source_plan_format = plan_source_format(access_plan)
    file_name = target_name(access_plan)

    if source_plan_format and source_plan_format != source_format:
        raise ConverterError(
            "INVALID_PARAMS",
            f"access_plan.source.format must be {source_format}",
            details=f"source format: {source_plan_format}",
        )
    if not file_name.lower().endswith(".copc.laz"):
        raise ConverterError("INVALID_PARAMS", "access_plan.target.name must end with .copc.laz")
    temp_dir = _make_work_dir(env)
    target_file = temp_dir / file_name
    started_at = time.time()
    reporter = _ProgressReporter(params.get("progress_callback"), env)
    try:
        reporter.emit("prepare", "started", "准备点云 COPC 转换", overall_progress=1, force=True)
        pdal = _pdal_bin(env)
        source_file = stage_source_file(runtime_access_plan, temp_dir)
        source_uri = str(source_file)
        pdal_source_uri = _prepare_pdal_source_uri(source_uri, source_format, temp_dir)
        command = [pdal, "translate", pdal_source_uri, str(target_file)]
        command.extend(_pdal_reader_args(source_format))
        command.append("--writers.copc.forward=all")
        command.extend(_pdal_copc_option_args(options, env))
        runtime_env = _runtime_env(runtime_access_plan, temp_dir, env)
        reporter.emit("convert", "started", "生成点云 COPC 文件", overall_progress=5, force=True)
        result = _run_executable(
            command,
            runner=runner,
            env_name=PDAL_ENV,
            timeout_seconds=timeout_seconds,
            extra_env=runtime_env,
            work_dir=temp_dir,
            progress_callback=lambda: reporter.emit(
                "convert",
                "progress",
                "生成点云 COPC 文件",
                overall_progress=_estimate_convert_progress(target_file, source.get("metadata") or {}),
                metadata={"output_size_bytes": _file_size(target_file)},
            ),
        )
        if not target_file.is_file():
            raise ConverterError(
                "OUTPUT_NOT_FOUND",
                "COPC output file was not generated",
                details=str(target_file),
                http_status=500,
            )
        reporter.emit(
            "publish",
            "started",
            "发布点云 COPC artifact",
            overall_progress=90,
            metadata={"output_size_bytes": target_file.stat().st_size},
            force=True,
        )
        publish_result = publish_target_file(target_file, runtime_access_plan)
        reporter.emit(
            "publish",
            "completed",
            "点云 COPC artifact 发布完成",
            overall_progress=95,
            metadata={"uploaded_bytes": publish_result["uploaded_bytes"]},
            force=True,
        )
        elapsed_ms = int((time.time() - started_at) * 1000)
        return {
            "copc_uri": publish_result.get("object_uri") or publish_result.get("locator") or publish_result.get("path"),
            "copc_ref": publish_result.get("object_name") or publish_result.get("path"),
            "size_bytes": publish_result["uploaded_bytes"],
            "publish": publish_result,
            "source_format": source_format,
            "target_format": "copc",
            "converter": pdal,
            "command": _redact_command(command),
            "stdout": result.stdout,
            "stderr": result.stderr,
            "work_dir": str(temp_dir),
            "elapsed_ms": elapsed_ms,
        }
    finally:
        shutil.rmtree(temp_dir, ignore_errors=True)


def _pdal_copc_option_args(options: dict[str, Any], env: dict[str, str] | None = None) -> list[str]:
    args: list[str] = []
    normalized = dict(options)
    if normalized.get("threads") in (None, ""):
        normalized["threads"] = _default_copc_threads(env)
    for key in [
        "scale_x",
        "scale_y",
        "scale_z",
        "offset_x",
        "offset_y",
        "offset_z",
        "a_srs",
        "threads",
        "extra_dims",
        "fixed_seed",
        "enhanced_srs_vlrs",
    ]:
        value = normalized.get(key)
        if value is None or value == "":
            continue
        args.append(f"--writers.copc.{key}={value}")
    return args


def _default_copc_threads(env: dict[str, str] | None = None) -> int:
    values = env if env is not None else os.environ
    raw = _text(values.get(COPC_THREADS_ENV))
    try:
        threads = int(raw) if raw else DEFAULT_COPC_THREADS
    except ValueError:
        threads = DEFAULT_COPC_THREADS
    if threads < 1:
        return 1
    if threads > MAX_COPC_THREADS:
        return MAX_COPC_THREADS
    return threads


def _pdal_reader_args(source_format: str) -> list[str]:
    if source_format == "xyz":
        return ["--reader", "readers.text", "--readers.text.header=X Y Z", "--readers.text.skip=0"]
    return []


def _prepare_pdal_source_uri(source_uri: str, source_format: str, temp_dir: Path) -> str:
    if source_format != "pcd":
        return source_uri
    if _is_virtual_path(source_uri):
        return source_uri
    source_file = Path(source_uri)
    normalized = _normalize_legacy_pcd_header(source_file)
    if normalized is None:
        return source_uri
    normalized_path = temp_dir / source_file.name
    normalized_path.write_bytes(normalized)
    return str(normalized_path)


def _normalize_legacy_pcd_header(source_file: Path) -> bytes | None:
    data = source_file.read_bytes()
    marker = re.search(rb"(?m)^VERSION\s+(?:\.\d+|0\.[0-6])\s*$", data)
    if marker is None:
        return None
    line_end = data.find(b"\n", marker.start())
    if line_end < 0:
        line_end = len(data)
    newline = b"\n" if line_end < len(data) else b""
    return data[: marker.start()] + b"VERSION 0.7" + newline + data[line_end + len(newline) :]


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
    extra_env: dict[str, str] | None = None,
    work_dir: Path | None = None,
    progress_callback: Callable[[], None] | None = None,
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
        if progress_callback is not None:
            progress_callback()
        result = runner(command, timeout_seconds)
        if progress_callback is not None:
            progress_callback()
    else:
        merged_env = os.environ.copy()
        if extra_env:
            merged_env.update({str(key): str(value) for key, value in extra_env.items() if key and value is not None})
        process = subprocess.Popen(
            command,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env=merged_env,
            cwd=str(work_dir) if work_dir else None,
        )
        deadline = time.monotonic() + timeout_seconds if timeout_seconds and timeout_seconds > 0 else None
        while process.poll() is None:
            if deadline is not None and time.monotonic() > deadline:
                process.kill()
                stdout, stderr = process.communicate()
                raise ConverterError(
                    "CONVERSION_TIMEOUT",
                    "pointcloud conversion command timed out",
                    details=_command_failure_details(command, CommandResult(returncode=-1, stdout=stdout, stderr=stderr), work_dir),
                    http_status=504,
                )
            if progress_callback is not None:
                progress_callback()
            time.sleep(1)
        stdout, stderr = process.communicate()
        result = CommandResult(returncode=process.returncode or 0, stdout=stdout, stderr=stderr)
    if result.returncode != 0:
        raise ConverterError(
            "CONVERSION_FAILED",
            "pointcloud conversion command failed",
            details=_command_failure_details(command, result, work_dir),
            http_status=500,
        )
    return result


class _ProgressReporter:
    def __init__(self, callback: Any, env: dict[str, str] | None = None) -> None:
        self.callback = callback if isinstance(callback, dict) else {}
        self.interval_seconds = _progress_interval_seconds(env)
        self.last_emit_at = 0.0
        self.last_progress = 0

    def emit(
        self,
        phase: str,
        event: str,
        message: str = "",
        *,
        overall_progress: int | None = None,
        metadata: dict[str, Any] | None = None,
        force: bool = False,
    ) -> None:
        if not self.callback:
            return
        now = time.monotonic()
        if not force and now - self.last_emit_at < self.interval_seconds:
            return
        progress = self.last_progress if overall_progress is None else _clamp_progress(overall_progress)
        if progress < self.last_progress:
            progress = self.last_progress
        self.last_progress = progress
        self.last_emit_at = now
        payload: dict[str, Any] = {
            "phase": phase,
            "event": event,
            "message": message,
            "overall_progress": progress,
        }
        if metadata:
            payload["metadata"] = metadata
        _post_progress(self.callback, payload)


def _post_progress(callback: dict[str, Any], payload: dict[str, Any]) -> None:
    endpoint = _text(callback.get("endpoint"))
    tenant_id = callback.get("tenant_id")
    attempt = callback.get("attempt")
    lease_token = _text(callback.get("lease_token"))
    if (
        not endpoint
        or not isinstance(tenant_id, int)
        or isinstance(tenant_id, bool)
        or tenant_id <= 0
        or not isinstance(attempt, int)
        or isinstance(attempt, bool)
        or attempt <= 0
        or not lease_token
    ):
        return
    token_source = SyncOAuthServiceTokenSource(
        os.environ.get("SYSTEM_URL", "http://localhost:8180"),
        "addp-pointcloud",
        os.environ.get("POINTCLOUD_WORKFLOW_SERVICE_CLIENT_SECRET", ""),
    )
    body_payload = dict(payload)
    body_payload["attempt"] = attempt
    body_payload["lease_token"] = lease_token
    body = json.dumps(body_payload).encode("utf-8")
    req = urlrequest.Request(endpoint, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Authorization", f"Bearer {token_source.token(tenant_id)}")
    try:
        with urlrequest.urlopen(req, timeout=5) as response:
            response.read()
    except (OSError, urlerror.URLError, urlerror.HTTPError):
        return


def _progress_interval_seconds(env: dict[str, str] | None = None) -> float:
    values = env if env is not None else os.environ
    raw = _text(values.get(PROGRESS_INTERVAL_ENV))
    try:
        seconds = float(raw) if raw else 5.0
    except ValueError:
        seconds = 5.0
    if seconds < 1:
        return 1.0
    if seconds > 60:
        return 60.0
    return seconds


def _estimate_convert_progress(target_file: Path, source: dict[str, Any]) -> int:
    output_size = _file_size(target_file)
    source_size = _source_size_bytes(source)
    if source_size <= 0 or output_size <= 0:
        return 10
    estimated = 10 + int((min(output_size, source_size) * 75) / source_size)
    if estimated > 85:
        return 85
    return estimated


def _source_size_bytes(source: dict[str, Any]) -> int:
    metadata = source.get("metadata")
    if isinstance(metadata, dict):
        value = metadata.get("source_size_bytes")
        try:
            return int(value)
        except (TypeError, ValueError):
            return 0
    return 0


def _file_size(path: Path) -> int:
    try:
        return path.stat().st_size
    except OSError:
        return 0


def _clamp_progress(value: int) -> int:
    if value < 0:
        return 0
    if value > 99:
        return 99
    return value


def _runtime_env(access_plan: dict[str, Any], temp_dir: Path, env: dict[str, str] | None) -> dict[str, str]:
    values: dict[str, str] = {}
    source = _required_object(access_plan, "source")
    plan_env = source.get("env")
    if isinstance(plan_env, dict):
        values.update(_clean_env(plan_env))
    if "CPL_TMPDIR" not in values:
        values["CPL_TMPDIR"] = str(temp_dir)
    if "TMPDIR" not in values:
        values["TMPDIR"] = str(temp_dir)
    _rewrite_localhost_endpoint(values, env)
    return values


def _clean_env(data: dict[str, Any]) -> dict[str, str]:
    return {
        str(key): str(value)
        for key, value in data.items()
        if key and value is not None and str(value).strip() != ""
    }


def _rewrite_localhost_endpoint(values: dict[str, str], env: dict[str, str] | None = None) -> None:
    endpoint = values.get("AWS_S3_ENDPOINT")
    if not endpoint:
        return
    rewritten = _runtime_endpoint(endpoint, env)
    if rewritten:
        values["AWS_S3_ENDPOINT"] = rewritten


def _runtime_access_plan(access_plan: dict[str, Any], env: dict[str, str] | None = None) -> dict[str, Any]:
    runtime_plan = copy.deepcopy(access_plan)
    changed = False
    for role in ("source", "target"):
        access = _required_object(runtime_plan, role).get("access")
        if not isinstance(access, dict) or access.get("method") != "object_store":
            continue
        endpoint = _text(access.get("endpoint"))
        rewritten = _runtime_endpoint(endpoint, env)
        if rewritten and rewritten != endpoint:
            access["endpoint"] = rewritten
            changed = True
    return runtime_plan if changed else access_plan


def _command_failure_details(command: list[str], result: CommandResult, work_dir: Path | None) -> str:
    parts = [
        f"exit_code={result.returncode}",
        "command=" + " ".join(_redact_command(command)),
    ]
    if work_dir is not None:
        parts.append(f"work_dir={work_dir}")
    stderr = _tail(result.stderr)
    stdout = _tail(result.stdout)
    if stderr:
        parts.append(f"stderr_tail={stderr}")
    if stdout:
        parts.append(f"stdout_tail={stdout}")
    return "; ".join(parts)


def _tail(value: str, limit: int = 4000) -> str:
    text = str(value or "").strip()
    if len(text) <= limit:
        return text
    return text[-limit:]


def _make_work_dir(env: dict[str, str] | None = None) -> Path:
    values = env if env is not None else os.environ
    base_dir = _text(values.get(WORK_DIR_ENV))
    if base_dir:
        Path(base_dir).mkdir(parents=True, exist_ok=True)
        return Path(tempfile.mkdtemp(prefix="addp-pointcloud-copc-", dir=base_dir))
    return Path(tempfile.mkdtemp(prefix="addp-pointcloud-copc-"))


def _is_virtual_path(path: str) -> bool:
    return str(path or "").startswith("/vsi")


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


def _runtime_endpoint(endpoint: str, env: dict[str, str] | None = None) -> str:
    normalized = _normal_endpoint(endpoint)
    values = env if env is not None else os.environ
    loopback_host = _endpoint_host(_text(values.get(OBJECT_STORE_LOOPBACK_HOST_ENV)))
    if not loopback_host or _endpoint_host(normalized) not in {"localhost", "127.0.0.1", "::1"}:
        return normalized
    parsed = urlparse(normalized if "://" in normalized else f"//{normalized}")
    port = f":{parsed.port}" if parsed.port is not None else ""
    return f"{loopback_host}{port}"


def _endpoint_host(endpoint: str) -> str:
    parsed = urlparse(endpoint if "://" in endpoint else f"//{endpoint}")
    return parsed.hostname or endpoint.split(":", 1)[0]


def _redact_command(command: list[str]) -> list[str]:
    redacted: list[str] = []
    for part in command:
        text = str(part)
        lowered = text.lower()
        if text.startswith("/vsicurl/"):
            redacted.append(_redact_vsicurl(text))
        elif "secret" in lowered or "access_key" in lowered or "password" in lowered:
            redacted.append("<redacted>")
        else:
            redacted.append(text)
    return redacted


def _redact_vsicurl(value: str) -> str:
    prefix = "/vsicurl/"
    raw = value[len(prefix) :]
    parsed = urlparse(raw)
    if not parsed.scheme or not parsed.netloc:
        return prefix + "<redacted-url>"
    return prefix + parsed._replace(query="<redacted>", fragment="").geturl()

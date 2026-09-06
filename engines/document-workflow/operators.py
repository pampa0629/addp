from __future__ import annotations

import copy
import os
import shutil
import signal
import subprocess
import tempfile
import threading
import time
import zipfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable
from urllib.parse import urlparse

from addp_common.workflow_access import (
    publish_target_file,
    require_access_plan,
    source_format as plan_source_format,
    stage_source_file,
    target_name,
)
from pypdf import PdfReader


ENGINE_TYPE = "document_workflow"
ENGINE_ROOT = Path(__file__).resolve().parent
DEFAULT_LIBREOFFICE_BIN = str(ENGINE_ROOT / "bin" / "soffice")
LIBREOFFICE_ENV = "DOCUMENT_LIBREOFFICE_BIN"
WORK_DIR_ENV = "DOCUMENT_WORK_DIR"
TIMEOUT_ENV = "DOCUMENT_CONVERSION_TIMEOUT_SECONDS"
CONCURRENCY_ENV = "DOCUMENT_CONVERSION_CONCURRENCY"
OBJECT_STORE_LOOPBACK_HOST_ENV = "DOCUMENT_OBJECT_STORE_LOOPBACK_HOST"
PDF_CONTENT_TYPE = "application/pdf"
SUPPORTED_SOURCE_FORMATS = {"pptx"}
MEDIA_EXTENSIONS = {
    ".3g2", ".3gp", ".aac", ".avi", ".m4a", ".m4v", ".mid", ".midi",
    ".mov", ".mp3", ".mp4", ".mpeg", ".mpg", ".ogg", ".wav", ".wma", ".wmv",
}


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
_conversion_slots_lock = threading.Lock()
_conversion_slots: dict[int, threading.BoundedSemaphore] = {}


def converter_status(env: dict[str, str] | None = None) -> dict[str, Any]:
    executable = _libreoffice_bin(env)
    available = _executable_available(executable)
    version = ""
    details = ""
    if available:
        try:
            probe = subprocess.run(
                [executable, "--version"],
                capture_output=True,
                text=True,
                timeout=5,
                check=False,
            )
            available = probe.returncode == 0
            version = (probe.stdout or probe.stderr).strip()
            if not available:
                details = f"LibreOffice version probe exited with {probe.returncode}"
        except (OSError, subprocess.SubprocessError) as exc:
            available = False
            details = str(exc)
    else:
        details = _executable_unavailable_detail(LIBREOFFICE_ENV, executable)
    return {
        "name": "libreoffice",
        "env": LIBREOFFICE_ENV,
        "path": executable,
        "available": available,
        "version": version,
        "binding": ENGINE_TYPE,
        "supported_source_formats": sorted(SUPPORTED_SOURCE_FORMATS),
        "details": details,
    }


def list_operators() -> list[dict[str, Any]]:
    return [
        {
            "id": "document_to_pdf",
            "name": "document_to_pdf",
            "display_name": "文档转 PDF",
            "engine_type": ENGINE_TYPE,
            "category": "文档转换",
            "category_path": ["文档转换", "静态预览"],
            "description": "使用运行时内绑定的 LibreOffice 将受支持文档转换并持久化为 PDF；第一阶段只支持 PPTX。",
            "brief_description": "将 PPTX 转换为静态 PDF。",
            "execution_modes": ["workflow", "direct"],
            "effects": ["read", "write"],
            "parameters": [
                {
                    "name": "access_plan",
                    "type": "object",
                    "required": True,
                    "description": "源文档与目标 PDF 的 addp.workflow.access-plan/v1 访问计划。",
                },
                {
                    "name": "options",
                    "type": "object",
                    "required": False,
                    "description": "文档转换选项；第一阶段只接受 strip_embedded_media。",
                },
            ],
            "output_ports": [
                {
                    "name": "result",
                    "type": "object",
                    "description": "持久化 PDF 的引用、大小、页数、发布结果和转换器信息。",
                    "is_default": True,
                }
            ],
        }
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
    return document_to_pdf(params, runner=runner, env=env, timeout_seconds=timeout_seconds)


def document_to_pdf(
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    access_plan = require_access_plan(params)
    runtime_plan = _runtime_access_plan(access_plan, env)
    source_format = plan_source_format(access_plan)
    target_file_name = target_name(access_plan)
    options = _optional_object(params, "options")
    unknown_options = sorted(set(options) - {"strip_embedded_media"})
    if unknown_options:
        raise ConverterError("INVALID_PARAMS", f"unsupported document conversion options: {', '.join(unknown_options)}")
    if source_format not in SUPPORTED_SOURCE_FORMATS:
        raise ConverterError(
            "UNSUPPORTED_SOURCE_FORMAT",
            f"document_to_pdf does not support source format: {source_format or '<empty>'}",
            details="supported source formats: pptx",
            http_status=422,
        )
    if not target_file_name.lower().endswith(".pdf"):
        raise ConverterError("INVALID_PARAMS", "access_plan.target.name must end with .pdf")

    wait_timeout = timeout_seconds or _int_env(TIMEOUT_ENV, 600, maximum=3600, env=env)
    conversion_slots = _conversion_semaphore(env)
    acquired = conversion_slots.acquire(timeout=max(1, wait_timeout))
    if not acquired:
        raise ConverterError("RUNTIME_BUSY", "document conversion capacity is busy", http_status=503)

    work_dir = _make_work_dir(env)
    started_at = time.time()
    try:
        staged_source = stage_source_file(runtime_plan, work_dir)
        conversion_source = staged_source
        media_facts = {"removed_files": 0, "removed_bytes": 0}
        if source_format == "pptx" and options.get("strip_embedded_media", True) is not False:
            conversion_source, media_facts = _strip_pptx_media(staged_source, work_dir / "prepared.pptx")

        output_dir = work_dir / "output"
        profile_dir = work_dir / "profile"
        output_dir.mkdir(parents=True, exist_ok=True)
        profile_dir.mkdir(parents=True, exist_ok=True)
        executable = _libreoffice_bin(env)
        command = [
            executable,
            "--headless",
            "--nologo",
            "--nolockcheck",
            "--nodefault",
            "--nofirststartwizard",
            f"-env:UserInstallation={profile_dir.resolve().as_uri()}",
            "--convert-to",
            "pdf",
            "--outdir",
            str(output_dir),
            str(conversion_source),
        ]
        result = _run_executable(
            command,
            runner=runner,
            timeout_seconds=wait_timeout,
            env=env,
            work_dir=work_dir,
        )
        generated = output_dir / f"{conversion_source.stem}.pdf"
        page_count = _validate_pdf(generated)
        publish_result = publish_target_file(generated, runtime_plan)
        status = converter_status(env)
        return {
            "pdf_uri": publish_result.get("object_uri") or publish_result.get("locator") or publish_result.get("path"),
            "pdf_ref": publish_result.get("object_name") or publish_result.get("path"),
            "size_bytes": publish_result["uploaded_bytes"],
            "page_count": page_count,
            "publish": publish_result,
            "source_format": source_format,
            "target_format": "pdf",
            "converter": {"name": "libreoffice", "version": status.get("version", ""), "path": executable},
            "media_preprocessing": media_facts,
            "stdout": _tail(result.stdout),
            "stderr": _tail(result.stderr),
            "elapsed_ms": int((time.time() - started_at) * 1000),
        }
    finally:
        conversion_slots.release()
        shutil.rmtree(work_dir, ignore_errors=True)


def _strip_pptx_media(source: Path, destination: Path) -> tuple[Path, dict[str, int]]:
    try:
        with zipfile.ZipFile(source, "r") as archive:
            removable = [
                item for item in archive.infolist()
                if item.filename.startswith("ppt/media/") and Path(item.filename).suffix.lower() in MEDIA_EXTENSIONS
            ]
            if not removable:
                return source, {"removed_files": 0, "removed_bytes": 0}
            removed_names = {item.filename for item in removable}
            destination.parent.mkdir(parents=True, exist_ok=True)
            with zipfile.ZipFile(destination, "w") as rewritten:
                for item in archive.infolist():
                    if item.filename in removed_names:
                        continue
                    with archive.open(item, "r") as source_stream, rewritten.open(item, "w", force_zip64=True) as target_stream:
                        shutil.copyfileobj(source_stream, target_stream, length=1024 * 1024)
            return destination, {
                "removed_files": len(removable),
                "removed_bytes": sum(item.file_size for item in removable),
            }
    except (OSError, zipfile.BadZipFile) as exc:
        raise ConverterError("INVALID_DOCUMENT", "PPTX source is not a valid Open XML package", details=str(exc), http_status=422) from exc


def _validate_pdf(path: Path) -> int:
    if not path.is_file() or path.stat().st_size <= 0:
        raise ConverterError("OUTPUT_NOT_FOUND", "PDF output file was not generated", details=str(path), http_status=500)
    with path.open("rb") as stream:
        if stream.read(5) != b"%PDF-":
            raise ConverterError("OUTPUT_INVALID", "document conversion output is not a PDF", http_status=500)
    try:
        page_count = len(PdfReader(str(path)).pages)
    except Exception as exc:
        raise ConverterError("OUTPUT_INVALID", "document conversion output PDF cannot be parsed", details=str(exc), http_status=500) from exc
    if page_count <= 0:
        raise ConverterError("OUTPUT_INVALID", "document conversion output PDF has no pages", http_status=500)
    return page_count


def _run_executable(
    command: list[str],
    *,
    runner: CommandRunner | None,
    timeout_seconds: int,
    env: dict[str, str] | None,
    work_dir: Path,
) -> CommandResult:
    executable = command[0] if command else ""
    if not _executable_available(executable):
        raise ConverterError(
            "CONVERTER_UNAVAILABLE",
            "LibreOffice executable is not available",
            details=_executable_unavailable_detail(LIBREOFFICE_ENV, executable),
            http_status=503,
        )
    if runner is not None:
        result = runner(command, timeout_seconds)
    else:
        merged_env = os.environ.copy()
        if env:
            merged_env.update({str(key): str(value) for key, value in env.items() if key and value is not None})
        merged_env.update({"HOME": str(work_dir / "home"), "TMPDIR": str(work_dir)})
        process = subprocess.Popen(
            command,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env=merged_env,
            cwd=str(work_dir),
            start_new_session=True,
        )
        try:
            stdout, stderr = process.communicate(timeout=timeout_seconds)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            stdout, stderr = process.communicate()
            raise ConverterError(
                "CONVERSION_TIMEOUT",
                "document conversion timed out",
                details=f"timeout_seconds={timeout_seconds}; stderr_tail={_tail(stderr)}",
                http_status=504,
            )
        result = CommandResult(process.returncode or 0, stdout, stderr)
    if result.returncode != 0:
        raise ConverterError(
            "CONVERSION_FAILED",
            "LibreOffice document conversion failed",
            details=f"exit_code={result.returncode}; stderr_tail={_tail(result.stderr)}; stdout_tail={_tail(result.stdout)}",
            http_status=500,
        )
    return result


def _libreoffice_bin(env: dict[str, str] | None = None) -> str:
    values = env if env is not None else os.environ
    return _text(values.get(LIBREOFFICE_ENV)) or DEFAULT_LIBREOFFICE_BIN


def _executable_available(path: str) -> bool:
    path = _text(path)
    return bool(path and Path(path).name != path and os.path.isfile(path) and os.access(path, os.X_OK))


def _executable_unavailable_detail(env_name: str, path: str) -> str:
    if Path(path).name == path:
        return f"{env_name}={path!r} is not allowed; bind an absolute or engine-local executable path"
    return f"{env_name} executable is not available: {path}"


def _make_work_dir(env: dict[str, str] | None = None) -> Path:
    values = env if env is not None else os.environ
    base_dir = _text(values.get(WORK_DIR_ENV))
    if base_dir:
        Path(base_dir).mkdir(parents=True, exist_ok=True)
        return Path(tempfile.mkdtemp(prefix="addp-document-", dir=base_dir))
    return Path(tempfile.mkdtemp(prefix="addp-document-"))


def _runtime_access_plan(access_plan: dict[str, Any], env: dict[str, str] | None = None) -> dict[str, Any]:
    runtime_plan = copy.deepcopy(access_plan)
    changed = False
    for role in ("source", "target"):
        resource = runtime_plan.get(role)
        access = resource.get("access") if isinstance(resource, dict) else None
        if not isinstance(access, dict) or access.get("method") != "object_store":
            continue
        endpoint = _text(access.get("endpoint"))
        rewritten = _runtime_endpoint(endpoint, env)
        if rewritten and rewritten != endpoint:
            access["endpoint"] = rewritten
            changed = True
    return runtime_plan if changed else access_plan


def _runtime_endpoint(endpoint: str, env: dict[str, str] | None = None) -> str:
    parsed = urlparse(endpoint if "://" in endpoint else f"//{endpoint}")
    normalized = parsed.netloc or endpoint
    values = env if env is not None else os.environ
    replacement = _text(values.get(OBJECT_STORE_LOOPBACK_HOST_ENV))
    if not replacement or (parsed.hostname or "") not in {"localhost", "127.0.0.1", "::1"}:
        return normalized
    replacement_host = urlparse(replacement if "://" in replacement else f"//{replacement}").hostname or replacement
    port = f":{parsed.port}" if parsed.port is not None else ""
    return f"{replacement_host}{port}"


def _optional_object(data: dict[str, Any], key: str) -> dict[str, Any]:
    value = data.get(key)
    return value if isinstance(value, dict) else {}


def _int_env(name: str, default: int, *, maximum: int, env: dict[str, str] | None = None) -> int:
    values = env if env is not None else os.environ
    try:
        value = int(_text(values.get(name)) or default)
    except ValueError:
        value = default
    return max(1, min(maximum, value))


def _conversion_semaphore(env: dict[str, str] | None = None) -> threading.BoundedSemaphore:
    capacity = _int_env(CONCURRENCY_ENV, 1, maximum=16, env=env)
    with _conversion_slots_lock:
        semaphore = _conversion_slots.get(capacity)
        if semaphore is None:
            semaphore = threading.BoundedSemaphore(capacity)
            _conversion_slots[capacity] = semaphore
        return semaphore


def _tail(value: str, limit: int = 4000) -> str:
    text = str(value or "").strip()
    return text if len(text) <= limit else text[-limit:]


def _text(value: Any) -> str:
    return "" if value is None else str(value).strip()

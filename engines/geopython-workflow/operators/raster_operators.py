"""
栅格格式算子模块

提供栅格格式优化算子。Manager 负责准备 GDAL source_uri / target_uri /
access_plan、目标 artifact 或业务数据集生命周期和任务状态；Python workflow
只负责执行 GDAL 栅格处理。
"""

import json
import logging
import math
import os
import posixpath
from pathlib import Path
import re
import fnmatch
from contextlib import contextmanager
from concurrent.futures import ThreadPoolExecutor, as_completed
import subprocess
import uuid
from datetime import datetime, timezone
from time import perf_counter
from typing import Any, Callable, Dict

import requests
from addp_common.client import SyncOAuthServiceTokenSource

from addp_common.raster_mosaic import (
    MANIFEST_FILE_NAME,
    SOURCE_INDEX_REF,
    RasterMosaicRefs,
    RasterMosaicSummary,
    build_manifest,
    build_source_index,
)

from .base import (
    OperatorType,
    OperatorMetadata,
    OperatorParam,
    OperatorCategory,
    register_operator,
)

logger = logging.getLogger(__name__)


def _import_gdal():
    try:
        from osgeo import gdal
    except Exception as exc:
        raise RuntimeError("GDAL Python bindings are required for raster mosaic generation") from exc
    gdal.UseExceptions()
    return gdal


def _run_command(args: list[str], extra_env: Dict[str, Any] | None = None) -> subprocess.CompletedProcess:
    logger.info("执行栅格命令: %s", " ".join(args))
    env = os.environ.copy()
    if extra_env:
        for key, value in extra_env.items():
            if key and value is not None:
                env[str(key)] = str(value)
    completed = subprocess.run(args, check=False, capture_output=True, text=True, env=env)
    if completed.returncode != 0:
        stderr = (completed.stderr or "").strip()
        stdout = (completed.stdout or "").strip()
        message = f"command failed with exit code {completed.returncode}: {' '.join(args)}"
        if stderr:
            message = f"{message}; stderr: {stderr}"
        if stdout:
            message = f"{message}; stdout: {stdout}"
        raise RuntimeError(message)
    return completed


def _gdalinfo_json(path: str, gdal_env: Dict[str, Any] | None = None) -> Dict[str, Any]:
    try:
        completed = _run_command(["gdalinfo", "-json", path], gdal_env)
        return json.loads(completed.stdout or "{}")
    except Exception as exc:
        logger.warning("读取 GeoTIFF 元数据失败: %s", exc)
        return {}


def _authority_code_from_wkt(wkt: str) -> str:
    text = str(wkt or "")
    for pattern in (r'AUTHORITY\["EPSG","(\d+)"\]', r'ID\["EPSG",\s*(\d+)'):
        for match in re.finditer(pattern, text):
            if _wkt_depth_at(text, match.start()) == 1:
                return f"EPSG:{match.group(1)}"
    return ""


def _wkt_depth_at(text: str, offset: int) -> int:
    depth = 0
    in_quote = False
    escaped = False
    for char in text[:offset]:
        if escaped:
            escaped = False
            continue
        if char == "\\":
            escaped = True
            continue
        if char == '"':
            in_quote = not in_quote
            continue
        if in_quote:
            continue
        if char == "[":
            depth += 1
        elif char == "]" and depth > 0:
            depth -= 1
    return depth


def _raster_facts(path: str, gdal_env: Dict[str, Any] | None = None) -> Dict[str, Any]:
    info = _gdalinfo_json(path, gdal_env)
    size = info.get("size") if isinstance(info.get("size"), list) else []
    width = int(size[0]) if len(size) > 0 and isinstance(size[0], (int, float)) else 0
    height = int(size[1]) if len(size) > 1 and isinstance(size[1], (int, float)) else 0
    bands = info.get("bands") if isinstance(info.get("bands"), list) else []
    extent = []
    extent_srid = 0
    corner_coordinates = info.get("cornerCoordinates")
    if isinstance(corner_coordinates, dict):
        lower_left = corner_coordinates.get("lowerLeft")
        upper_right = corner_coordinates.get("upperRight")
        if isinstance(lower_left, list) and isinstance(upper_right, list) and len(lower_left) >= 2 and len(upper_right) >= 2:
            extent = [float(lower_left[0]), float(lower_left[1]), float(upper_right[0]), float(upper_right[1])]
    coordinate_system = info.get("coordinateSystem")
    source_crs = ""
    source_crs_definition = ""
    if isinstance(coordinate_system, dict):
        source_crs_definition = str(coordinate_system.get("wkt") or "").strip()
        source_crs = _authority_code_from_wkt(source_crs_definition)
        if source_crs.startswith("EPSG:"):
            try:
                extent_srid = int(source_crs.split(":", 1)[1])
            except ValueError:
                extent_srid = 0
    return {
        "width": width,
        "height": height,
        "band_count": len(bands),
        "size_bytes": os.path.getsize(path) if os.path.exists(path) else 0,
        "extent": extent,
        "extent_srid": extent_srid,
        "source_crs": source_crs,
        "source_crs_definition": source_crs_definition,
    }


def _is_gdal_virtual_path(path: str) -> bool:
    return str(path or "").startswith("/vsi")


@contextmanager
def _gdal_config_env(gdal_env: Dict[str, Any] | None):
    gdal = _import_gdal()
    env = {
        str(key): str(value)
        for key, value in (gdal_env or {}).items()
        if key and value is not None
    }
    previous_env = {key: os.environ.get(key) for key in env}
    previous_config = {key: gdal.GetConfigOption(key) for key in env}
    try:
        for key, value in env.items():
            os.environ[key] = value
            gdal.SetConfigOption(key, value)
        yield gdal
    finally:
        for key, value in previous_env.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
        for key, value in previous_config.items():
            gdal.SetConfigOption(key, value)


def _set_path_specific_gdal_options(path_envs: list[tuple[str, Dict[str, Any]]]) -> Callable[[], None]:
    gdal = _import_gdal()
    configured_paths: list[str] = []
    for path, env in path_envs:
        clean_path = str(path or "").rstrip("/")
        if not clean_path or not _is_gdal_virtual_path(clean_path):
            continue
        for key, value in (env or {}).items():
            if key and value is not None:
                gdal.SetPathSpecificOption(clean_path, str(key), str(value))
        configured_paths.append(clean_path)

    def cleanup() -> None:
        for path in configured_paths:
            try:
                gdal.ClearPathSpecificOptions(path)
            except Exception:
                pass

    return cleanup


def _join_gdal_path(root: str, *parts: str) -> str:
    base = str(root or "").rstrip("/")
    clean_parts = [str(part).strip("/") for part in parts if str(part or "").strip("/")]
    if not clean_parts:
        return base
    return base + "/" + "/".join(clean_parts)


def _relative_gdal_path(root: str, child: str) -> str:
    root_clean = str(root or "").rstrip("/")
    child_clean = str(child or "")
    if root_clean and child_clean == root_clean:
        return ""
    prefix = root_clean + "/"
    if root_clean and child_clean.startswith(prefix):
        return child_clean[len(prefix):]
    return child_clean


def _path_parent(path: str) -> str:
    text = str(path or "").rstrip("/")
    if "/" not in text:
        return ""
    return text.rsplit("/", 1)[0]


def _ensure_gdal_dir(path: str, gdal_env: Dict[str, Any] | None = None) -> None:
    directory = str(path or "").strip()
    if not directory:
        return
    if _is_gdal_virtual_path(directory):
        with _gdal_config_env(gdal_env) as gdal:
            gdal.MkdirRecursive(directory, 0o755)
        return
    Path(directory).mkdir(parents=True, exist_ok=True)


def _write_json(path: str, payload: Dict[str, Any], gdal_env: Dict[str, Any] | None = None) -> None:
    data = json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True).encode("utf-8")
    _ensure_gdal_dir(_path_parent(path), gdal_env)
    if _is_gdal_virtual_path(path):
        with _gdal_config_env(gdal_env) as gdal:
            handle = gdal.VSIFOpenL(path, "wb")
            if handle is None:
                raise RuntimeError(f"GDAL cannot open JSON target for writing: {path}")
            try:
                written = gdal.VSIFWriteL(data, 1, len(data), handle)
            finally:
                close_result = gdal.VSIFCloseL(handle)
            if written != len(data):
                raise RuntimeError(f"GDAL wrote incomplete JSON payload to {path}: {written}/{len(data)} bytes")
            if close_result != 0:
                raise RuntimeError(f"GDAL failed to close JSON target after writing: {path}")
            stat = gdal.VSIStatL(path)
            if stat is None or int(getattr(stat, "size", 0) or 0) <= 0:
                raise RuntimeError(f"GDAL JSON target was not created: {path}")
        return
    Path(path).write_bytes(data)


def _safe_component(value: str) -> str:
    text = re.sub(r"[^A-Za-z0-9._/-]+", "_", str(value or "").strip().replace("\\", "/"))
    text = re.sub(r"/+", "/", text).strip("/")
    return text or f"raster_{uuid.uuid4().hex[:8]}"


def _source_matches(path: str, rel_path: str, include_patterns: list[str], exclude_patterns: list[str]) -> bool:
    name = posixpath.basename(path)
    candidates = [name, rel_path]
    if include_patterns and not any(fnmatch.fnmatch(candidate, pattern) for pattern in include_patterns for candidate in candidates):
        return False
    if exclude_patterns and any(fnmatch.fnmatch(candidate, pattern) for pattern in exclude_patterns for candidate in candidates):
        return False
    return True


def _list_source_files(source_plan: Dict[str, Any]) -> list[str]:
    root = str(source_plan.get("root_uri") or "").rstrip("/")
    if not root:
        raise ValueError("access_plan.source.root_uri is required")
    recursive = bool(source_plan.get("recursive", True))
    include_patterns = [str(item) for item in source_plan.get("include_patterns") or ["*.tif", "*.tiff"]]
    exclude_patterns = [str(item) for item in source_plan.get("exclude_patterns") or []]
    gdal_env = source_plan.get("gdal_env") if isinstance(source_plan.get("gdal_env"), dict) else {}

    discovered: list[str] = []
    if _is_gdal_virtual_path(root):
        with _gdal_config_env(gdal_env) as gdal:
            def walk(prefix: str):
                entries = gdal.ReadDir(prefix) or []
                for entry in entries:
                    if entry in (".", ".."):
                        continue
                    child = _join_gdal_path(prefix, entry)
                    stat = gdal.VSIStatL(child)
                    is_dir = bool(stat and (stat.mode & 0o040000))
                    if is_dir:
                        if recursive:
                            walk(child)
                        continue
                    rel = _relative_gdal_path(root, child)
                    if _source_matches(child, rel, include_patterns, exclude_patterns):
                        discovered.append(child)
            walk(root)
    else:
        root_path = Path(root)
        if not root_path.exists():
            raise FileNotFoundError(f"source root does not exist: {root}")
        iterator = root_path.rglob("*") if recursive else root_path.glob("*")
        for candidate in iterator:
            if not candidate.is_file():
                continue
            rel = candidate.relative_to(root_path).as_posix()
            path = candidate.as_posix()
            if _source_matches(path, rel, include_patterns, exclude_patterns):
                discovered.append(path)

    return sorted(discovered)


def _first_band_dtype(ds: Any) -> str:
    gdal = _import_gdal()
    if ds.RasterCount <= 0:
        return ""
    band = ds.GetRasterBand(1)
    return gdal.GetDataTypeName(band.DataType) if band else ""


def _dataset_extent(ds: Any) -> list[float]:
    transform = ds.GetGeoTransform(can_return_null=True)
    if not transform:
        return []
    width = float(ds.RasterXSize)
    height = float(ds.RasterYSize)
    points = [
        (0.0, 0.0),
        (width, 0.0),
        (0.0, height),
        (width, height),
    ]
    xs = [transform[0] + x * transform[1] + y * transform[2] for x, y in points]
    ys = [transform[3] + x * transform[4] + y * transform[5] for x, y in points]
    return [min(xs), min(ys), max(xs), max(ys)]


def _extent_looks_geographic(extent: list[float]) -> bool:
    if not isinstance(extent, list) or len(extent) != 4:
        return False
    min_x, min_y, max_x, max_y = [float(value) for value in extent]
    return (
        -180.0 <= min_x <= 180.0
        and -180.0 <= max_x <= 180.0
        and -90.0 <= min_y <= 90.0
        and -90.0 <= max_y <= 90.0
        and max_x > min_x
        and max_y > min_y
    )


def _inspect_raster(path: str, gdal_env: Dict[str, Any] | None = None) -> Dict[str, Any]:
    with _gdal_config_env(gdal_env) as gdal:
        ds = gdal.OpenEx(path, gdal.OF_RASTER)
        if ds is None:
            raise ValueError(f"GDAL cannot open raster: {path}")
        driver_name = ds.GetDriver().ShortName if ds.GetDriver() else ""
        band = ds.GetRasterBand(1) if ds.RasterCount else None
        block_size = list(band.GetBlockSize()) if band else []
        overview_count = band.GetOverviewCount() if band else 0
        image_structure = ds.GetMetadata("IMAGE_STRUCTURE") or {}
        layout = str(image_structure.get("LAYOUT") or "").upper()
        compression = str(image_structure.get("COMPRESSION") or "")
        is_tiled = bool(block_size and block_size[0] > 1 and block_size[1] > 1 and block_size[0] < ds.RasterXSize + 1)
        is_cog = driver_name.upper() == "GTIFF" and (layout == "COG" or (is_tiled and (overview_count > 0 or max(ds.RasterXSize, ds.RasterYSize) <= max(block_size or [0]))))
        projection = ds.GetProjection() or ""
        extent = _dataset_extent(ds)
        source_crs = _authority_code_from_wkt(projection)
        if not source_crs and _extent_looks_geographic(extent):
            source_crs = "EPSG:4326"
        return {
            "path": path,
            "driver": driver_name,
            "width": int(ds.RasterXSize),
            "height": int(ds.RasterYSize),
            "band_count": int(ds.RasterCount),
            "dtype": _first_band_dtype(ds),
            "block_size": block_size,
            "overview_count": int(overview_count),
            "compression": compression,
            "layout": layout,
            "is_cog": bool(is_cog),
            "extent": extent,
            "source_crs": source_crs,
        }


def _translate_to_cog(
    source: str,
    target: str,
    gdal_env: Dict[str, Any] | None,
    cog_config: Dict[str, Any],
    width: int = 0,
    height: int = 0,
    resampling: str = "",
    callback: Callable[[float, str, Any], int] | None = None,
) -> None:
    compression = str(cog_config.get("compression") or "DEFLATE").upper()
    blocksize = int(cog_config.get("blocksize") or 512)
    overview_resampling = str(cog_config.get("overview_resampling") or "NEAREST").upper()
    num_threads = str(cog_config.get("num_threads") or "2").strip()
    output_srs = str(cog_config.get("assign_srs") or "").strip()
    if not output_srs:
        try:
            output_srs = str(_inspect_raster(source, gdal_env).get("source_crs") or "").strip()
        except Exception:
            output_srs = ""
    creation_options = [
        f"COMPRESS={compression}",
        f"BLOCKSIZE={blocksize}",
        f"OVERVIEW_RESAMPLING={overview_resampling}",
    ]
    if num_threads:
        creation_options.append(f"NUM_THREADS={num_threads}")
    _ensure_gdal_dir(_path_parent(target), gdal_env)
    with _gdal_config_env(gdal_env) as gdal:
        if _is_gdal_virtual_path(target):
            try:
                gdal.Unlink(target)
            except Exception:
                pass
        elif Path(target).exists():
            Path(target).unlink()
        options = gdal.TranslateOptions(
            format="COG",
            creationOptions=creation_options,
            width=int(width or 0),
            height=int(height or 0),
            resampleAlg=str(resampling or "") or None,
            outputSRS=output_srs or None,
            callback=callback,
        )
        result = gdal.Translate(target, source, options=options)
        if result is None:
            raise RuntimeError(f"GDAL failed to create COG: {target}")
        result = None


def _leaf_retry_attempts(cog_config: Dict[str, Any]) -> int:
    try:
        attempts = int(cog_config.get("leaf_retry_attempts") or 2)
    except (TypeError, ValueError):
        attempts = 2
    if attempts < 1:
        return 1
    if attempts > 5:
        return 5
    return attempts


def _inspect_existing_cog(path: str, gdal_env: Dict[str, Any] | None = None) -> Dict[str, Any] | None:
    try:
        facts = _inspect_raster(path, gdal_env)
    except Exception:
        return None
    if not facts.get("is_cog"):
        return None
    return facts


def _replace_raster_with_temp(source: str, temp_path: str, gdal_env: Dict[str, Any] | None) -> None:
    if _is_gdal_virtual_path(source):
        backup_path = source + f".addp-backup-{uuid.uuid4().hex}"
        with _gdal_config_env(gdal_env) as gdal:
            try:
                gdal.Rename(source, backup_path)
                gdal.Rename(temp_path, source)
                gdal.Unlink(backup_path)
            except Exception:
                try:
                    if gdal.VSIStatL(source) is None and gdal.VSIStatL(backup_path) is not None:
                        gdal.Rename(backup_path, source)
                finally:
                    raise
        return
    os.replace(temp_path, source)


def _progress_reporter(progress_plan: Dict[str, Any] | None) -> Callable[[Dict[str, Any]], None]:
    if not isinstance(progress_plan, dict):
        return lambda payload: None
    endpoint = str(progress_plan.get("endpoint") or "").strip()
    tenant_id = progress_plan.get("tenant_id")
    if not endpoint or not isinstance(tenant_id, int) or isinstance(tenant_id, bool) or tenant_id <= 0:
        return lambda payload: None
    token_source = SyncOAuthServiceTokenSource(
        os.environ.get("SYSTEM_URL", "http://localhost:8180"),
        "addp-geopython",
        os.environ.get("GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET", ""),
    )

    def emit(payload: Dict[str, Any]) -> None:
        try:
            token = token_source.token(tenant_id)
            response = requests.post(
                endpoint,
                json=payload,
                headers={"Authorization": f"Bearer {token}"},
                timeout=5,
            )
            if response.status_code == 401:
                token_source.invalidate(tenant_id, token)
                token = token_source.token(tenant_id)
                requests.post(
                    endpoint,
                    json=payload,
                    headers={"Authorization": f"Bearer {token}"},
                    timeout=5,
                )
        except Exception as exc:
            logger.warning("上报 raster mosaic 进度失败: %s", exc)

    return emit


def _gdal_progress_callback(
    emit: Callable[[Dict[str, Any]], None],
    phase: str,
    message: str,
    total_files: int,
    processed_files: int,
    failed_files: int,
    current_file: str,
    base_progress: float,
    span_progress: float,
) -> Callable[[float, str, Any], int]:
    state = {"last_percent": -1}

    def callback(complete: float, _message: str, _data: Any) -> int:
        file_progress = int(max(0, min(100, round(float(complete or 0) * 100))))
        if file_progress == state["last_percent"] or (file_progress - state["last_percent"] < 5 and file_progress < 100):
            return 1
        state["last_percent"] = file_progress
        emit({
            "phase": phase,
            "event": "file_progress",
            "message": message,
            "total_files": total_files,
            "processed_files": processed_files,
            "failed_files": failed_files,
            "current_file": current_file,
            "file_progress": file_progress,
            "overall_progress": int(max(0, min(100, round(base_progress + span_progress * float(complete or 0))))),
        })
        return 1

    return callback


def _union_extent(items: list[Dict[str, Any]]) -> list[float]:
    extents = [item.get("extent") for item in items if isinstance(item.get("extent"), list) and len(item.get("extent")) == 4]
    if not extents:
        return []
    return [
        min(float(extent[0]) for extent in extents),
        min(float(extent[1]) for extent in extents),
        max(float(extent[2]) for extent in extents),
        max(float(extent[3]) for extent in extents),
    ]


def _elapsed_ms(started_at: float) -> int:
    return int(max(0, round((perf_counter() - started_at) * 1000)))


def _prepare_target_path(target: str, overwrite: bool) -> None:
    if _is_gdal_virtual_path(target):
        return
    target_path = Path(target)
    if not target_path.exists():
        return
    if not overwrite:
        raise FileExistsError(f"target already exists: {target}")
    if target_path.is_dir():
        raise IsADirectoryError(f"target is a directory: {target}")
    target_path.unlink()


def tiff_to_cog(
	source_uri: str,
	target_uri: str,
	gdal_env: Dict[str, Any] | None = None,
    assign_srs: str = "",
    compression: str = "DEFLATE",
    blocksize: int = 512,
    overview_resampling: str = "NEAREST",
    overwrite: bool = True,
    **kwargs,
) -> Dict[str, Any]:
    """
    将 TIFF / GeoTIFF 转换为 Cloud Optimized GeoTIFF。

    参数:
    - source_uri: Manager 预处理后的 GDAL 可读 URI，例如本地挂载路径或 /vsicurl/ URL。
    - target_uri: Manager 预处理后的 GDAL 可写 URI，例如 infra MinIO /vsis3/bucket/object。
    - gdal_env: Manager 预处理后的 GDAL 子进程环境变量。
    - assign_srs: Manager 基于源 Meta facts 派生的 CRS 定义，只写入目标 COG，不重投影。
    """
    source = str(source_uri or "").strip()
    target = str(target_uri or "").strip()
    if not source:
        raise ValueError("source_uri is required")
    if not target:
        raise ValueError("target_uri is required")
    _prepare_target_path(target, overwrite)

    cmd = [
        "gdal_translate",
        "-of",
        "COG",
        "-co",
        f"COMPRESS={compression}",
        "-co",
        f"BLOCKSIZE={int(blocksize)}",
        "-co",
        f"OVERVIEW_RESAMPLING={overview_resampling}",
    ]
    if str(assign_srs or "").strip():
        cmd.extend(["-a_srs", str(assign_srs).strip()])
    cmd.extend([source, target])
    _run_command(cmd, gdal_env)

    facts = _raster_facts(target, gdal_env)
    return {
        "status": "success",
        "format": "tiff",
        "profile": "cog",
        "source_uri": source,
        "target_uri": target,
        "compression": compression,
        "blocksize": int(blocksize),
        "overview_resampling": overview_resampling,
        "assign_srs": str(assign_srs or "").strip(),
		**facts,
	}


def build_raster_mosaic(
	access_plan: Dict[str, Any],
	placement: Dict[str, Any],
	cog: Dict[str, Any] | None = None,
	overview: Dict[str, Any] | None = None,
	tiles: Dict[str, Any] | None = None,
	**kwargs,
) -> Dict[str, Any]:
	"""
	生成栅格 mosaic 数据集。

	Manager 负责把 ADDP locator、engine 和目标业务存储解析为 GDAL 可访问
	的 access_plan；本算子只处理栅格技术流程，不识别 ADDP locator。
	"""
	if not isinstance(access_plan, dict):
		raise ValueError("access_plan is required and must be an object")
	if not isinstance(placement, dict):
		raise ValueError("placement is required and must be an object")
	source_plan = access_plan.get("source")
	target_plan = access_plan.get("target")
	if not isinstance(source_plan, dict):
		raise ValueError("access_plan.source is required and must be an object")
	if not isinstance(target_plan, dict):
		raise ValueError("access_plan.target is required and must be an object")
	if not str(source_plan.get("root_uri") or "").strip():
		raise ValueError("access_plan.source.root_uri is required")
	if not str(target_plan.get("dataset_root_uri") or "").strip():
		raise ValueError("access_plan.target.dataset_root_uri is required")
	mode = str(placement.get("mode") or "").strip()
	if mode not in ("in_place", "detached"):
		raise ValueError("placement.mode must be in_place or detached")

	cog_config = dict(cog or {})
	overview_config = dict(overview or {})
	source_env = source_plan.get("gdal_env") if isinstance(source_plan.get("gdal_env"), dict) else {}
	target_env = target_plan.get("gdal_env") if isinstance(target_plan.get("gdal_env"), dict) else {}
	source_root = str(source_plan.get("root_uri") or "").rstrip("/")
	target_root = str(target_plan.get("dataset_root_uri") or "").rstrip("/")
	dataset_name = str(target_plan.get("dataset_name") or posixpath.basename(target_root) or "raster_mosaic")
	total_started_at = perf_counter()
	stage_timings: Dict[str, Any] = {}
	progress_plan = access_plan.get("progress_callback") if isinstance(access_plan.get("progress_callback"), dict) else {}
	emit = _progress_reporter(progress_plan)
	cleanup_path_options = _set_path_specific_gdal_options([
		(source_root, source_env),
		(target_root, target_env),
	])

	discover_started_at = perf_counter()
	_emit_payload = {
		"phase": "discover",
		"event": "started",
		"message": "发现源 TIFF/COG",
		"overall_progress": 0,
	}
	emit(_emit_payload)
	try:
		source_files = _list_source_files(source_plan)
	except Exception:
		cleanup_path_options()
		raise
	if not source_files:
		cleanup_path_options()
		raise ValueError("no source TIFF files were discovered")
	stage_timings["discover"] = {
		"duration_ms": _elapsed_ms(discover_started_at),
		"source_count": len(source_files),
	}

	_emit_payload = {
		"phase": "leaf_cog",
		"event": "started",
		"message": "生成 leaf COG",
		"total_files": len(source_files),
		"processed_files": 0,
		"failed_files": 0,
		"overall_progress": 1,
	}
	emit(_emit_payload)

	leaf_started_at = perf_counter()
	leaf_concurrency = int(cog_config.get("leaf_concurrency") or 1)
	if leaf_concurrency < 1:
		leaf_concurrency = 1
	if leaf_concurrency > 8:
		leaf_concurrency = 8
	if mode != "detached":
		leaf_concurrency = 1
	if mode == "detached":
		_ensure_gdal_dir(_join_gdal_path(target_root, "leaf"), target_env)
	worker_target_env = {} if mode == "detached" and leaf_concurrency > 1 else target_env
	leaf_items_by_index: list[Dict[str, Any] | None] = [None] * len(source_files)
	failed_files = 0
	leaf_retry_attempts = _leaf_retry_attempts(cog_config)
	leaf_stats = {
		"generated_count": 0,
		"reused_count": 0,
		"retry_count": 0,
	}

	def translate_leaf_with_retries(
		index: int,
		source: str,
		target: str,
		gdal_env: Dict[str, Any] | None,
		source_rel: str,
		base_progress: float,
		span_progress: float,
		message: str,
	) -> tuple[Dict[str, Any], int]:
		last_error: Exception | None = None
		for attempt in range(1, leaf_retry_attempts + 1):
			try:
				_translate_to_cog(
					source,
					target,
					gdal_env,
					cog_config,
					callback=_gdal_progress_callback(
						emit,
						"leaf_cog",
						message,
						len(source_files),
						index,
						0,
						source_rel,
						base_progress,
						span_progress,
					),
				)
				facts = _inspect_raster(target, gdal_env)
				if not facts.get("is_cog"):
					raise RuntimeError(f"generated leaf COG did not pass content validation: {source_rel}")
				return facts, attempt - 1
			except Exception as exc:
				last_error = exc
				if attempt >= leaf_retry_attempts:
					break
				emit({
					"phase": "leaf_cog",
					"event": "file_retry",
					"message": "leaf COG 生成重试",
					"total_files": len(source_files),
					"processed_files": sum(1 for item in leaf_items_by_index if item is not None),
					"failed_files": failed_files,
					"current_file": source_rel,
					"overall_progress": int(round(base_progress)),
					"metadata": {
						"attempt": attempt,
						"max_attempts": leaf_retry_attempts,
						"error": str(exc),
					},
				})
		raise RuntimeError(f"leaf COG generation failed after {leaf_retry_attempts} attempts: {source_rel}") from last_error

	def process_leaf(index: int, source: str) -> tuple[int, str, float, float, Dict[str, Any], Dict[str, int]]:
		source_rel = _relative_gdal_path(source_root, source)
		leaf_rel = _safe_component(source_rel)
		leaf_uri = source
		leaf_ref = _relative_gdal_path(target_root, source)
		leaf_kind = "source_cog"
		generation_status = "source"
		retry_count = 0
		base_progress = 1 + (float(index) / float(len(source_files))) * 79
		span_progress = 79 / float(len(source_files))

		try:
			if mode == "detached":
				stem = re.sub(r"\.[Tt][Ii][Ff]{1,2}$", "", leaf_rel)
				leaf_ref = _safe_component(stem) + ".cog.tif"
				leaf_uri = _join_gdal_path(target_root, "leaf", leaf_ref)
				leaf_facts = _inspect_existing_cog(leaf_uri, worker_target_env)
				if leaf_facts:
					leaf_kind = "reused_cog"
					generation_status = "reused"
				else:
					leaf_facts, retry_count = translate_leaf_with_retries(
						index,
						source,
						leaf_uri,
						worker_target_env,
						source_rel,
						base_progress,
						span_progress,
						"生成 leaf COG",
					)
					leaf_kind = "generated_cog"
					generation_status = "generated"
				leaf_ref = _relative_gdal_path(target_root, leaf_uri)
			else:
				source_facts = _inspect_raster(source, source_env)
				if not source_facts.get("is_cog"):
					temp_uri = source + f".addp-tmp-{uuid.uuid4().hex}.cog.tif"
					_, retry_count = translate_leaf_with_retries(
						index,
						source,
						temp_uri,
						source_env,
						source_rel,
						base_progress,
						span_progress,
						"原地规范化 leaf COG",
					)
					_replace_raster_with_temp(source, temp_uri, source_env)
					leaf_uri = source
					leaf_ref = _relative_gdal_path(target_root, leaf_uri)
					leaf_kind = "normalized_in_place_cog"
					generation_status = "generated"
					leaf_facts = _inspect_raster(leaf_uri, source_env)
				else:
					leaf_facts = source_facts

			if not leaf_facts.get("is_cog"):
				raise RuntimeError(f"leaf raster is not a content-valid COG: {source_rel}")
			leaf_item = {
				"id": f"leaf-{index + 1:06d}",
				"leaf_ref": leaf_ref,
				"leaf_uri": leaf_uri if not leaf_ref or leaf_ref == leaf_uri else "",
				"leaf_kind": leaf_kind,
				"generation_status": generation_status,
				"source_ref": source_rel,
				"width": leaf_facts.get("width"),
				"height": leaf_facts.get("height"),
				"band_count": leaf_facts.get("band_count"),
				"dtype": leaf_facts.get("dtype"),
				"extent": leaf_facts.get("extent"),
				"source_crs": leaf_facts.get("source_crs"),
				"cog_validation": {
					"method": "gdal_metadata",
					"status": "valid",
					"layout": leaf_facts.get("layout"),
					"block_size": leaf_facts.get("block_size"),
					"overview_count": leaf_facts.get("overview_count"),
					"compression": leaf_facts.get("compression"),
				},
			}
			stats = {
				"generated_count": 1 if generation_status == "generated" else 0,
				"reused_count": 1 if generation_status == "reused" else 0,
				"retry_count": retry_count,
			}
			return index, source_rel, base_progress, span_progress, {key: value for key, value in leaf_item.items() if value not in ("", None)}, stats
		except Exception:
			raise

	def record_leaf_completed(index: int, source_rel: str, base_progress: float, span_progress: float, leaf_item: Dict[str, Any], stats: Dict[str, int]) -> None:
		leaf_items_by_index[index] = leaf_item
		for key in leaf_stats:
			leaf_stats[key] += int(stats.get(key) or 0)
		processed_files = sum(1 for item in leaf_items_by_index if item is not None)
		emit({
			"phase": "leaf_cog",
			"event": "file_completed",
			"message": "leaf COG 完成",
			"total_files": len(source_files),
			"processed_files": processed_files,
			"failed_files": failed_files,
			"current_file": source_rel,
			"file_progress": 100,
			"overall_progress": int(round(base_progress + span_progress)),
		})

	def record_leaf_failed(source_rel: str, base_progress: float) -> None:
		emit({
			"phase": "leaf_cog",
			"event": "file_failed",
			"message": "leaf COG 生成失败",
			"total_files": len(source_files),
			"processed_files": sum(1 for item in leaf_items_by_index if item is not None),
			"failed_files": failed_files,
			"current_file": source_rel,
			"overall_progress": int(round(base_progress)),
		})

	if leaf_concurrency > 1:
		try:
			with _gdal_config_env(target_env):
				with ThreadPoolExecutor(max_workers=leaf_concurrency) as executor:
					futures = {executor.submit(process_leaf, index, source): (index, source) for index, source in enumerate(source_files)}
					for future in as_completed(futures):
						index, source = futures[future]
						source_rel = _relative_gdal_path(source_root, source)
						base_progress = 1 + (float(index) / float(len(source_files))) * 79
						try:
							result_index, result_source_rel, result_base_progress, result_span_progress, leaf_item, stats = future.result()
							record_leaf_completed(result_index, result_source_rel, result_base_progress, result_span_progress, leaf_item, stats)
						except Exception:
							failed_files += 1
							record_leaf_failed(source_rel, base_progress)
							for pending in futures:
								if pending is not future:
									pending.cancel()
							raise
		except Exception:
			cleanup_path_options()
			raise
	else:
		for index, source in enumerate(source_files):
			source_rel = _relative_gdal_path(source_root, source)
			base_progress = 1 + (float(index) / float(len(source_files))) * 79
			try:
				result_index, result_source_rel, result_base_progress, result_span_progress, leaf_item, stats = process_leaf(index, source)
				record_leaf_completed(result_index, result_source_rel, result_base_progress, result_span_progress, leaf_item, stats)
			except Exception:
				failed_files += 1
				record_leaf_failed(source_rel, base_progress)
				cleanup_path_options()
				raise

	leaf_items = [item for item in leaf_items_by_index if item is not None]
	stage_timings["leaf_cog"] = {
		"duration_ms": _elapsed_ms(leaf_started_at),
		"leaf_count": len(leaf_items),
		"concurrency": leaf_concurrency,
		"retry_attempts": leaf_retry_attempts,
		"generated_count": leaf_stats["generated_count"],
		"reused_count": leaf_stats["reused_count"],
		"retry_count": leaf_stats["retry_count"],
	}

	leaf_uris = [
		_join_gdal_path(target_root, item["leaf_ref"]) if "leaf_ref" in item and not str(item["leaf_ref"]).startswith("/") else str(item.get("leaf_uri") or item.get("leaf_ref"))
		for item in leaf_items
	]
	vrt_uri = f"/vsimem/addp-raster-mosaic-{uuid.uuid4().hex}.vrt"
	overview_ref = "overviews/overview.cog.tif"
	overview_uri = _join_gdal_path(target_root, overview_ref)
	overview_enabled = bool(overview_config.get("enabled", True))
	overview_started_at = perf_counter()
	try:
		with _gdal_config_env(target_env) as gdal:
			emit({
				"phase": "overview",
				"event": "started",
				"message": "构建全局 overview COG",
				"total_files": len(source_files),
				"processed_files": len(leaf_items),
				"failed_files": failed_files,
				"overall_progress": 80,
			})
			vrt_options = gdal.BuildVRTOptions(
				resampleAlg=str(overview_config.get("resampling") or "AVERAGE"),
				allowProjectionDifference=False,
			)
			vrt_ds = gdal.BuildVRT(vrt_uri, leaf_uris, options=vrt_options)
			if vrt_ds is None:
				raise RuntimeError("GDAL failed to build raster mosaic VRT")
			vrt_width = int(vrt_ds.RasterXSize)
			vrt_height = int(vrt_ds.RasterYSize)
			vrt_ds = None

		overview_width = 0
		overview_height = 0
		if overview_enabled:
			max_pixels = int(overview_config.get("max_pixels") or 64000000)
			total_pixels = max(1, vrt_width * vrt_height)
			if total_pixels > max_pixels:
				scale = math.sqrt(float(max_pixels) / float(total_pixels))
				overview_width = max(1, int(vrt_width * scale))
				overview_height = max(1, int(vrt_height * scale))
			_translate_to_cog(
				vrt_uri,
				overview_uri,
				target_env,
				{
					**cog_config,
					"overview_resampling": str(overview_config.get("resampling") or cog_config.get("overview_resampling") or "AVERAGE"),
				},
				width=overview_width,
				height=overview_height,
				resampling=str(overview_config.get("resampling") or "AVERAGE"),
				callback=_gdal_progress_callback(
					emit,
					"overview",
					"构建全局 overview COG",
					len(source_files),
					len(leaf_items),
					failed_files,
					overview_ref,
					80,
					15,
				),
			)
			overview_facts = _inspect_raster(overview_uri, target_env)
		else:
			overview_facts = {}
		stage_timings["overview"] = {
			"duration_ms": _elapsed_ms(overview_started_at),
			"enabled": overview_enabled,
			"width": int(overview_facts.get("width") or 0),
			"height": int(overview_facts.get("height") or 0),
		}

		manifest_started_at = perf_counter()
		index_ref = SOURCE_INDEX_REF
		manifest_ref = MANIFEST_FILE_NAME
		index_uri = _join_gdal_path(target_root, index_ref)
		manifest_uri = _join_gdal_path(target_root, manifest_ref)
		now = datetime.now(timezone.utc).isoformat()
		union_extent = _union_extent(leaf_items)
		source_crs_values = sorted({str(item.get("source_crs") or "") for item in leaf_items if str(item.get("source_crs") or "")})
		index_payload = build_source_index(now, leaf_items)
		manifest_payload = build_manifest(
			dataset_name=dataset_name,
			generated_at=now,
			refs=RasterMosaicRefs(index=index_ref, overview=overview_ref if overview_enabled else ""),
			summary=RasterMosaicSummary(
				leaf_count=len(leaf_items),
				source_count=len(source_files),
				failed_count=failed_files,
				extent=union_extent,
				source_crs=source_crs_values[0] if len(source_crs_values) == 1 else "",
				vrt_width=vrt_width,
				vrt_height=vrt_height,
				overview_width=int(overview_facts.get("width") or 0),
				overview_height=int(overview_facts.get("height") or 0),
			),
			capabilities={
				"leaf_cog": True,
				"global_overview_cog": bool(overview_enabled),
				"backend_tile_preview": True,
			},
		)
		_write_json(index_uri, index_payload, target_env)
		_write_json(manifest_uri, manifest_payload, target_env)
		stage_timings["manifest"] = {
			"duration_ms": _elapsed_ms(manifest_started_at),
		}
		stage_timings["total"] = {
			"duration_ms": _elapsed_ms(total_started_at),
		}
		emit({
			"phase": "manifest",
			"event": "completed",
			"message": "mosaic manifest 已写入",
			"total_files": len(source_files),
			"processed_files": len(leaf_items),
			"failed_files": failed_files,
			"overall_progress": 100,
			"metadata": {
				"manifest_ref": manifest_ref,
				"index_ref": index_ref,
				"overview_ref": overview_ref if overview_enabled else "",
				"stage_timings": stage_timings,
			},
		})
		return {
			"status": "success",
			"format": "raster_mosaic",
			"data_type": "media",
			"layout": "whole",
			"manifest_locator": manifest_uri,
			"manifest_ref": manifest_ref,
			"index_ref": index_ref,
			"overview_ref": overview_ref if overview_enabled else "",
			"leaf_count": len(leaf_items),
			"source_count": len(source_files),
			"failed_count": failed_files,
			"stage_timings": stage_timings,
			"extent": union_extent,
			"overview": {
				"width": overview_facts.get("width"),
				"height": overview_facts.get("height"),
				"is_cog": overview_facts.get("is_cog"),
			} if overview_enabled else {},
		}
	finally:
		try:
			with _gdal_config_env(target_env) as gdal:
				try:
					gdal.Unlink(vrt_uri)
				except Exception:
					pass
		finally:
			cleanup_path_options()


TIFF_TO_COG_METADATA = OperatorMetadata(
	name="tiff_to_cog",
	type=OperatorType.GENERAL,
    category=OperatorCategory.FORMAT_CONVERSION,
    description="TIFF 转 COG",
    brief_description="将 TIFF / GeoTIFF 转换为 Cloud Optimized GeoTIFF",
    execution_modes=["workflow", "direct"],
    effects=["read", "write"],
    overview="面向 Manager 栅格快显派生产物的窄口径转换算子。Manager 负责将 source locator 和 infra artifact 目标预处理为 GDAL URI / 环境变量；本算子只负责执行 TIFF 到 COG 的格式转换。",
    params=[
        OperatorParam(
            name="source_uri",
            type="param",
            data_type="string",
            required=True,
            description="源 TIFF GDAL URI",
            notes="由 Manager 在执行前派生，可以是本地挂载路径、/vsicurl/ URL 等 GDAL 可读 URI。",
        ),
        OperatorParam(
            name="target_uri",
            type="param",
            data_type="string",
            required=True,
            description="目标 COG GDAL URI",
            notes="由 Manager 在执行前派生，第一阶段为 infra MinIO /vsis3/bucket/object。",
        ),
        OperatorParam(
            name="gdal_env",
            type="param",
            data_type="object",
            required=False,
            description="GDAL 子进程环境变量",
            notes="由 Manager 派生，只作用于 gdal_translate / gdalinfo 子进程。",
        ),
        OperatorParam(
            name="assign_srs",
            type="param",
            data_type="string",
            required=False,
            description="写入目标 COG 的 CRS 定义",
            notes="由 Manager 根据源 Meta 空间事实派生；只用于 -a_srs 保留 GeoTIFF CRS authority，不执行坐标转换。",
        ),
        OperatorParam(
            name="compression",
            type="param",
            data_type="string",
            required=False,
            description="压缩方式",
            enum=["DEFLATE", "LZW", "ZSTD", "JPEG", "NONE"],
            default="DEFLATE",
        ),
        OperatorParam(
            name="blocksize",
            type="param",
            data_type="int",
            required=False,
            description="COG block size",
            default=512,
        ),
        OperatorParam(
            name="overview_resampling",
            type="param",
            data_type="string",
            required=False,
            description="概览重采样方法",
            enum=["NEAREST", "BILINEAR", "CUBIC", "AVERAGE"],
            default="NEAREST",
        ),
        OperatorParam(
            name="overwrite",
            type="param",
            data_type="bool",
            required=False,
            description="目标已存在时是否覆盖",
            default=True,
        ),
    ],
    use_cases=[
        "Manager 将源 item locator 派生为 GDAL source_uri 后生成 COG 快显 artifact。",
        "已是 COG 的源文件复制到受管 artifact 位置前，可用该算子重写为规范 COG。",
        "小 TIFF 自动优化为 COG，提升后续前端 Range 渲染效率。",
        "大 TIFF 用户手动触发 COG 快显产物生成。",
    ],
    notes=[
        "该算子不是瓦片生成，不输出 PNG/JPEG/WebP tile。",
        "该算子不直接访问 Manager 数据库，也不登记 artifact state。",
        "输入输出 GDAL URI 和访问环境由 Manager 在执行前保证。",
        "第一阶段不承诺所有源格式都能转换；失败应由 Manager execution 记录。",
    ],
    workflow_example={
        "id": "build_cog",
        "operator": "tiff_to_cog",
        "params": {
            "source_uri": "/vsicurl/http://manager/source-presigned.tif",
            "target_uri": "/vsis3/manager/tenant_7/cog/fp/source.cog.tif",
            "assign_srs": "+proj=longlat +datum=WGS84 +no_defs",
            "gdal_env": {
                "AWS_S3_ENDPOINT": "minio:9000",
                "AWS_ACCESS_KEY_ID": "minioadmin",
                "AWS_SECRET_ACCESS_KEY": "minioadmin",
                "AWS_VIRTUAL_HOSTING": "FALSE",
                "AWS_HTTPS": "NO"
            },
            "compression": "DEFLATE",
            "blocksize": 512,
        },
        "depends_on": [],
    },
)


BUILD_RASTER_MOSAIC_METADATA = OperatorMetadata(
	name="build_raster_mosaic",
	type=OperatorType.GENERAL,
	category=OperatorCategory.FORMAT_CONVERSION,
	description="栅格 mosaic 数据集生成",
	brief_description="从资源树 node 批量生成业务存储中的 raster_mosaic 数据集",
	execution_modes=["workflow", "direct"],
	effects=["read", "write"],
	overview="面向 Manager raster_mosaic_generation 任务的栅格 mosaic 生成算子。Manager 负责任务定义、源 node 和目标业务存储选择；GeoPython Workflow 负责内容级 COG 校验、必要的 leaf COG 转换、全局 overview COG 和 manifest/index 生成。",
	params=[
		OperatorParam(
			name="access_plan",
			type="param",
			data_type="object",
			required=True,
			description="Manager 解析后的 GDAL 访问计划",
			notes="包含 source.root_uri/source.gdal_env/source.include_patterns、target.dataset_root_uri/target.gdal_env、progress_callback 等技术访问参数。原始 ADDP locator 只能作为诊断 metadata，不由 Python 解析。",
		),
		OperatorParam(
			name="placement",
			type="param",
			data_type="object",
			required=True,
			description="生成位置模式",
			notes="包含 mode=in_place|detached。该模式只影响生成流程，生成完成后不进入 mosaic item 语义。",
		),
		OperatorParam(
			name="cog",
			type="param",
			data_type="object",
			required=False,
			description="leaf COG 生成与校验配置",
			notes="包含 compression、blocksize、overview_resampling、validate_source_cog、leaf_concurrency、num_threads、leaf_retry_attempts。COG 判断必须使用 GDAL/rio-cogeo 等内容级校验，不能依赖后缀。",
		),
		OperatorParam(
			name="overview",
			type="param",
			data_type="object",
			required=False,
			description="全局 overview COG 配置",
			notes="全局 overview 是低分辨率 COG，不是全分辨率单文件 mosaic COG。",
		),
		OperatorParam(
			name="tiles",
			type="param",
			data_type="object",
			required=False,
			description="可选低层级瓦片缓存配置",
			notes="默认关闭；只在确认全局 overview COG 无法满足低层级预览性能时启用。",
		),
	],
	use_cases=[
		"用户从资源树 node 选择几千个 TIFF/GeoTIFF，生成一个 raster_mosaic 业务 item。",
		"源文件已是内容级 COG 时直接引用，源文件不是 COG 时在目标业务存储生成 leaf COG。",
		"为大规模栅格集合生成全局 overview COG，支撑地图低分辨率快速预览。",
	],
	notes=[
		"mosaic 不是单文件全分辨率 COG，而是一套业务数据集。",
		"mosaic 数据集内部 leaf 读取对象必须是 COG。",
		"该算子使用 GDAL Python API 落地，Manager 只传入 GDAL access_plan，不要求 Python 识别 ADDP locator。",
		"全局 overview 是低分辨率 COG，不是全分辨率单文件 mosaic COG。",
	],
	workflow_example={
		"id": "build_raster_mosaic",
		"operator": "build_raster_mosaic",
		"params": {
			"access_plan": {
				"source": {
					"root_uri": "/mnt/business/rasters/source",
					"gdal_env": {},
					"recursive": True,
					"include_patterns": ["*.tif", "*.tiff"],
					"exclude_patterns": [],
					"metadata": {
						"locator": "addp://engine/2/path/raster/source?type=node"
					},
				},
				"target": {
					"dataset_root_uri": "/vsis3/business/mosaic/output/raster_mosaic",
					"gdal_env": {
						"AWS_S3_ENDPOINT": "business-minio:9000",
						"AWS_ACCESS_KEY_ID": "access-key",
						"AWS_SECRET_ACCESS_KEY": "secret-key",
						"AWS_VIRTUAL_HOSTING": "FALSE",
						"AWS_HTTPS": "NO",
					},
					"dataset_name": "raster_mosaic",
				},
				"progress_callback": {
					"endpoint": "http://manager:8081/api/v1/manager/executions/{execution_id}/events",
					"tenant_id": 7,
					"execution_id": "execution-uuid",
				},
			},
			"placement": {
				"mode": "detached",
			},
			"cog": {
				"compression": "DEFLATE",
				"blocksize": 512,
				"overview_resampling": "NEAREST",
				"validate_source_cog": True,
				"leaf_concurrency": 4,
				"num_threads": 2,
			},
			"overview": {
				"enabled": True,
				"max_pixels": 64000000,
				"resampling": "AVERAGE",
			},
			"tiles": {
				"enabled": False,
				"format": "webp",
			},
		},
		"depends_on": [],
	},
)


OPERATORS = dict([
	register_operator(TIFF_TO_COG_METADATA, tiff_to_cog),
	register_operator(BUILD_RASTER_MOSAIC_METADATA, build_raster_mosaic),
])

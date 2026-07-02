from __future__ import annotations

import os
import json
import shutil
import struct
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
DEFAULT_MESH_CONVERTER_BIN = str(ENGINE_ROOT / "bin" / "assimp")
MESH_CONVERTER_ENV = "MODEL3D_MESH_CONVERTER_BIN"
DEFAULT_IFC_CONVERTER_BIN = str(ENGINE_ROOT / "bin" / "IfcConvert")
IFC_CONVERTER_ENV = "MODEL3D_IFC_CONVERTER_BIN"
GAUSSIAN_SPLAT_CONVERTER_SCRIPT = str(ENGINE_ROOT / "create_ksplat.mjs")
GAUSSIAN_SPLAT_NODE_ENV = "MODEL3D_GAUSSIAN_SPLAT_NODE_BIN"
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
    mesh_converter = _mesh_converter_bin(env)
    ifc_converter = _ifc_converter_bin(env)
    gaussian_splat_node = _gaussian_splat_node_bin(env)
    converter_available = _executable_available(converter)
    mesh_converter_available = _executable_available(mesh_converter)
    ifc_converter_available = _executable_available(ifc_converter)
    gaussian_splat_converter_available = _gaussian_splat_converter_available(gaussian_splat_node)
    available = converter_available and mesh_converter_available and ifc_converter_available and gaussian_splat_converter_available
    details = [
        detail
        for detail in [
            "" if converter_available else _executable_unavailable_detail(CONVERTER_ENV, converter),
            "" if mesh_converter_available else _executable_unavailable_detail(MESH_CONVERTER_ENV, mesh_converter),
            "" if ifc_converter_available else _executable_unavailable_detail(IFC_CONVERTER_ENV, ifc_converter),
            "" if gaussian_splat_converter_available else _gaussian_splat_converter_unavailable_detail(gaussian_splat_node),
        ]
        if detail
    ]
    return {
        "name": "_3dtile",
        "env": CONVERTER_ENV,
        "path": converter,
        "available": available,
        "binding": "model3d_workflow",
        "details": "; ".join(details),
        "mesh_converter": {
            "name": "assimp",
            "env": MESH_CONVERTER_ENV,
            "path": mesh_converter,
            "available": mesh_converter_available,
            "details": "" if mesh_converter_available else _executable_unavailable_detail(MESH_CONVERTER_ENV, mesh_converter),
        },
        "ifc_converter": {
            "name": "IfcConvert",
            "env": IFC_CONVERTER_ENV,
            "path": ifc_converter,
            "available": ifc_converter_available,
            "details": "" if ifc_converter_available else _executable_unavailable_detail(IFC_CONVERTER_ENV, ifc_converter),
        },
        "gaussian_splat_converter": {
            "name": "create_ksplat",
            "env": GAUSSIAN_SPLAT_NODE_ENV,
            "node_path": gaussian_splat_node,
            "script": GAUSSIAN_SPLAT_CONVERTER_SCRIPT,
            "available": gaussian_splat_converter_available,
            "details": "" if gaussian_splat_converter_available else _gaussian_splat_converter_unavailable_detail(gaussian_splat_node),
        },
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
            "id": "gltf_to_glb",
            "name": "gltf_to_glb",
            "display_name": "glTF 转 GLB",
            "engine_type": ENGINE_TYPE,
            "category": "三维模型转换",
            "category_path": ["三维模型转换", "快显"],
            "description": "将 glTF 多资源模型打包为前端可快速预览的 GLB artifact。",
            "execution_modes": ["direct"],
            "parameters": [
                {
                    "name": "access_plan",
                    "type": "object",
                    "required": True,
                    "description": "源 glTF manifest 访问计划和 GLB artifact 对象存储发布计划。",
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
            "id": "fbx_to_glb",
            "name": "fbx_to_glb",
            "display_name": "FBX 转 GLB",
            "engine_type": ENGINE_TYPE,
            "category": "三维模型转换",
            "category_path": ["三维模型转换", "快显"],
            "description": "将 FBX 单体网格模型转换为前端可快速预览的 GLB artifact。",
            "execution_modes": ["direct"],
            "parameters": [
                {
                    "name": "access_plan",
                    "type": "object",
                    "required": True,
                    "description": "源 FBX 文件访问计划和 GLB artifact 对象存储发布计划。",
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
            "id": "obj_to_glb",
            "name": "obj_to_glb",
            "display_name": "OBJ 转 GLB",
            "engine_type": ENGINE_TYPE,
            "category": "三维模型转换",
            "category_path": ["三维模型转换", "快显"],
            "description": "将 OBJ 单体网格模型转换为前端可快速预览的 GLB artifact。",
            "execution_modes": ["direct"],
            "parameters": [
                {
                    "name": "access_plan",
                    "type": "object",
                    "required": True,
                    "description": "源 OBJ 文件访问计划和 GLB artifact 对象存储发布计划。",
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
            "id": "stl_to_glb",
            "name": "stl_to_glb",
            "display_name": "STL 转 GLB",
            "engine_type": ENGINE_TYPE,
            "category": "三维模型转换",
            "category_path": ["三维模型转换", "快显"],
            "description": "将 STL 单体网格模型转换为前端可快速预览的 GLB artifact。",
            "execution_modes": ["direct"],
            "parameters": [
                {
                    "name": "access_plan",
                    "type": "object",
                    "required": True,
                    "description": "源 STL 文件访问计划和 GLB artifact 对象存储发布计划。",
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
            "id": "ifc_to_glb",
            "name": "ifc_to_glb",
            "display_name": "IFC 转 GLB",
            "engine_type": ENGINE_TYPE,
            "category": "三维模型转换",
            "category_path": ["三维模型转换", "快显"],
            "description": "将 IFC BIM 模型通过 IfcConvert 转换为前端可快速预览的 GLB artifact。",
            "execution_modes": ["direct"],
            "parameters": [
                {
                    "name": "access_plan",
                    "type": "object",
                    "required": True,
                    "description": "源 IFC 文件访问计划和 GLB artifact 对象存储发布计划。",
                },
                {
                    "name": "options",
                    "type": "object",
                    "required": False,
                    "description": "转换选项，第一版支持 center_model 控制是否将模型居中。",
                },
            ],
            "output_ports": [
                {
                    "name": "result",
                    "type": "object",
                    "description": "GLB artifact 的对象引用、大小、发布结果和 IfcConvert 转换摘要。",
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
        {
            "id": "gaussian_splat_to_ksplat",
            "name": "gaussian_splat_to_ksplat",
            "display_name": "Gaussian Splat 转 KSplat",
            "engine_type": ENGINE_TYPE,
            "category": "三维模型转换",
            "category_path": ["三维模型转换", "高斯泼溅"],
            "description": "将 PLY / SPLAT 高斯泼溅源转换为 Manager 受管 KSplat 快显 artifact；源已经是 KSplat 时直接发布登记。",
            "execution_modes": ["direct"],
            "parameters": [
                {
                    "name": "access_plan",
                    "type": "object",
                    "required": True,
                    "description": "高斯泼溅源文件访问计划和 KSplat artifact 对象存储发布计划。",
                },
                {
                    "name": "options",
                    "type": "object",
                    "required": False,
                    "description": "转换器私有选项，支持 compression_level、alpha_threshold 和 spherical_harmonics_degree。",
                },
            ],
            "output_ports": [
                {
                    "name": "result",
                    "type": "object",
                    "description": "KSplat artifact 的对象引用、大小、发布结果和转换器信息。",
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
    if name == "gltf_to_glb":
        return gltf_to_glb(params, runner=runner, env=env, timeout_seconds=timeout_seconds)
    if name == "fbx_to_glb":
        return fbx_to_glb(params, runner=runner, env=env, timeout_seconds=timeout_seconds)
    if name == "obj_to_glb":
        return obj_to_glb(params, runner=runner, env=env, timeout_seconds=timeout_seconds)
    if name == "stl_to_glb":
        return stl_to_glb(params, runner=runner, env=env, timeout_seconds=timeout_seconds)
    if name == "ifc_to_glb":
        return ifc_to_glb(params, runner=runner, env=env, timeout_seconds=timeout_seconds)
    if name == "osgb_scene_to_3dtiles":
        return osgb_scene_to_3dtiles(params, runner=runner, env=env, timeout_seconds=timeout_seconds)
    if name == "gaussian_splat_to_ksplat":
        return gaussian_splat_to_ksplat(params, runner=runner, env=env, timeout_seconds=timeout_seconds)
    raise ConverterError("OPERATOR_NOT_FOUND", f"Operator not found: {name}", http_status=404)


def gaussian_splat_to_ksplat(
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    access_plan = _required_object(params, "access_plan")
    source = _required_object(access_plan, "source")
    target = _required_object(access_plan, "target")
    options = _optional_object(params, "options")
    source_path = _first_text(source, "local_path", "root_uri")
    source_format = _text(source.get("format")).lower()
    publish = _required_object(target, "publish")
    file_name = _required_text(target, "file_name")

    if not source_path:
        raise ConverterError("INVALID_PARAMS", "access_plan.source.local_path or root_uri is required")
    if source_format not in {"ply", "splat", "ksplat"}:
        raise ConverterError(
            "UNSUPPORTED_SOURCE_FORMAT",
            "gaussian_splat_to_ksplat supports only PLY, SPLAT or KSplat sources",
            details=f"source format: {source_format or '<empty>'}",
        )
    if _text(publish.get("method")) != "object_store":
        raise ConverterError("INVALID_PARAMS", "access_plan.target.publish.method must be object_store")
    if not file_name.lower().endswith(".ksplat"):
        raise ConverterError("INVALID_PARAMS", "access_plan.target.file_name must end with .ksplat")

    source_file = Path(source_path)
    if not source_file.is_file():
        raise ConverterError("SOURCE_NOT_FOUND", "Gaussian splat source file was not found", details=str(source_file))

    if source_format == "ksplat":
        publish_result = publish_object_store_file(source_file, publish)
        return {
            "ksplat_uri": publish_result["object_uri"],
            "ksplat_ref": publish_result["object_name"],
            "size_bytes": publish_result["uploaded_bytes"],
            "publish": publish_result,
            "source_format": source_format,
            "target_format": "ksplat",
            "converter": "copy",
            "command": [],
            "stdout": "",
            "stderr": "",
        }

    temp_dir = Path(tempfile.mkdtemp(prefix="addp-gaussian-ksplat-"))
    target_file = temp_dir / file_name
    try:
        node_bin = _gaussian_splat_node_bin(env)
        _ensure_gaussian_splat_converter_available(node_bin)
        conversion_options = _gaussian_splat_conversion_options(options)
        command = [
            node_bin,
            GAUSSIAN_SPLAT_CONVERTER_SCRIPT,
            str(source_file),
            str(target_file),
            source_format,
            str(_int_option(options, "compression_level", 1, 0, 2)),
            str(_int_option(options, "alpha_threshold", 1, 0, 255)),
            str(_int_option(options, "spherical_harmonics_degree", 0, 0, 2)),
            str(conversion_options["section_size"]),
            _scene_center_arg(conversion_options["scene_center"]),
            _number_arg(conversion_options["block_size"]),
            str(conversion_options["bucket_size"]),
        ]
        result = _run_executable(command, runner=runner, env_name=GAUSSIAN_SPLAT_NODE_ENV, timeout_seconds=timeout_seconds)
        if not target_file.is_file():
            raise ConverterError(
                "OUTPUT_NOT_FOUND",
                "KSplat output file was not generated",
                details=str(target_file),
                http_status=500,
            )

        publish_result = publish_object_store_file(target_file, publish)
        return {
            "ksplat_uri": publish_result["object_uri"],
            "ksplat_ref": publish_result["object_name"],
            "size_bytes": publish_result["uploaded_bytes"],
            "publish": publish_result,
            "source_format": source_format,
            "target_format": "ksplat",
            "converter": GAUSSIAN_SPLAT_CONVERTER_SCRIPT,
            "section_size": conversion_options["section_size"],
            "scene_center": conversion_options["scene_center"],
            "scene_center_source": conversion_options["scene_center_source"],
            "block_size": conversion_options["block_size"],
            "bucket_size": conversion_options["bucket_size"],
            "converter_facts": _json_object_from_stdout(result.stdout),
            "command": _redact_command(command),
            "stdout": result.stdout,
            "stderr": result.stderr,
        }
    finally:
        shutil.rmtree(temp_dir, ignore_errors=True)


def osgb_to_glb(
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    return _single_model_to_glb(
        params,
        source_label="OSGB",
        converter_format="gltf",
        runner=runner,
        env=env,
        timeout_seconds=timeout_seconds,
    )


def _single_model_to_glb(
    params: dict[str, Any],
    *,
    source_label: str,
    converter_format: str,
    runner: CommandRunner | None,
    env: dict[str, str] | None,
    timeout_seconds: int | None,
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
        command = [converter, "-f", converter_format, "-i", source_path, "-o", str(target_file)]
        result = _run_converter(command, runner=runner, env=env, timeout_seconds=timeout_seconds)
        if not target_file.is_file():
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
            "source_format": source_label.lower(),
            "converter": converter,
            "command": _redact_command(command),
            "stdout": result.stdout,
            "stderr": result.stderr,
        }
    finally:
        shutil.rmtree(temp_dir, ignore_errors=True)


def gltf_to_glb(
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    return _mesh_model_to_glb(
        params,
        source_label="glTF",
        runner=runner,
        env=env,
        timeout_seconds=timeout_seconds,
    )


def fbx_to_glb(
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    return _mesh_model_to_glb(
        params,
        source_label="FBX",
        runner=runner,
        env=env,
        timeout_seconds=timeout_seconds,
    )


def obj_to_glb(
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    return _mesh_model_to_glb(
        params,
        source_label="OBJ",
        runner=runner,
        env=env,
        timeout_seconds=timeout_seconds,
    )


def stl_to_glb(
    params: dict[str, Any],
    *,
    runner: CommandRunner | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int | None = None,
) -> dict[str, Any]:
    return _mesh_model_to_glb(
        params,
        source_label="STL",
        runner=runner,
        env=env,
        timeout_seconds=timeout_seconds,
    )


def ifc_to_glb(
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

    temp_dir = Path(tempfile.mkdtemp(prefix="addp-ifc-glb-"))
    target_file = temp_dir / file_name
    try:
        converter = _ifc_converter_bin(env)
        command = [converter]
        options = params.get("options")
        options = options if isinstance(options, dict) else {}
        if bool(options.get("center_model", True)):
            command.append("--center-model")
        command.extend([source_path, str(target_file)])
        result = _run_executable(command, runner=runner, env_name=IFC_CONVERTER_ENV, timeout_seconds=timeout_seconds)
        if not target_file.is_file():
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
            "source_format": "ifc",
            "converter": converter,
            "command": _redact_command(command),
            "stdout": result.stdout,
            "stderr": result.stderr,
        }
    finally:
        shutil.rmtree(temp_dir, ignore_errors=True)


def _mesh_model_to_glb(
    params: dict[str, Any],
    *,
    source_label: str,
    runner: CommandRunner | None,
    env: dict[str, str] | None,
    timeout_seconds: int | None,
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

    if source_label.lower() == "obj":
        _validate_obj_material_libraries(Path(source_path))

    temp_dir = Path(tempfile.mkdtemp(prefix="addp-model3d-glb-"))
    target_file = temp_dir / file_name

    try:
        converter = _mesh_converter_bin(env)
        command = [converter, "export", source_path, str(target_file), "-embtex"]
        result = _run_executable(command, runner=runner, env_name=MESH_CONVERTER_ENV, timeout_seconds=timeout_seconds)
        if not target_file.is_file():
            raise ConverterError(
                "OUTPUT_NOT_FOUND",
                "GLB output file was not generated",
                details=str(target_file),
                http_status=500,
            )

        postprocess = {}
        if source_label.lower() == "obj":
            postprocess = _repair_obj_glb_fully_transparent_textured_materials(target_file)

        publish_result = publish_object_store_file(target_file, publish)
        facts = {
            "glb_uri": publish_result["object_uri"],
            "glb_ref": publish_result["object_name"],
            "size_bytes": publish_result["uploaded_bytes"],
            "publish": publish_result,
            "source_format": source_label.lower(),
            "converter": converter,
            "command": _redact_command(command),
            "stdout": result.stdout,
            "stderr": result.stderr,
        }
        if postprocess:
            facts["postprocess"] = postprocess
        return facts
    finally:
        shutil.rmtree(temp_dir, ignore_errors=True)


def _validate_obj_material_libraries(source_path: Path) -> None:
    if not source_path.is_file():
        return
    refs = _obj_material_library_refs(source_path)
    if not refs:
        return
    missing = []
    for ref in refs:
        ref_path = Path(ref.replace("\\", "/"))
        if ref_path.is_absolute():
            candidate = ref_path
        else:
            candidate = source_path.parent / ref_path
        if not candidate.is_file():
            missing.append(ref)
    if missing:
        raise ConverterError(
            "MISSING_OBJ_MATERIAL_LIBRARY",
            "OBJ declares material libraries that are missing",
            details=", ".join(missing),
        )


def _obj_material_library_refs(source_path: Path) -> list[str]:
    refs: list[str] = []
    seen: set[str] = set()
    with source_path.open("r", encoding="utf-8", errors="ignore") as handle:
        for line in handle:
            statement = line.split("#", 1)[0].strip()
            if not statement:
                continue
            parts = statement.split()
            if len(parts) < 2 or parts[0].lower() != "mtllib":
                continue
            for ref in parts[1:]:
                ref = ref.strip()
                if ref and ref not in seen:
                    refs.append(ref)
                    seen.add(ref)
    return refs


def _repair_obj_glb_fully_transparent_textured_materials(glb_path: Path) -> dict[str, Any]:
    doc, chunks = _read_glb(glb_path)
    materials = doc.get("materials")
    if not isinstance(materials, list):
        return {}

    textured_materials = [material for material in materials if _material_has_base_color_texture(material)]
    if not textured_materials:
        return {}
    if any(not _material_alpha_is_fully_transparent(material) for material in textured_materials):
        return {}

    repaired = 0
    for material in textured_materials:
        if _set_material_alpha(material, 1.0):
            repaired += 1
        if material.get("alphaMode") == "BLEND":
            material.pop("alphaMode", None)
    if repaired == 0:
        return {}

    _write_glb(glb_path, doc, chunks)
    return {
        "obj_textured_material_alpha": "normalized_to_opaque",
        "material_count": repaired,
    }


def _material_has_base_color_texture(material: Any) -> bool:
    if not isinstance(material, dict):
        return False
    pbr = material.get("pbrMetallicRoughness")
    if isinstance(pbr, dict) and isinstance(pbr.get("baseColorTexture"), dict):
        return True
    specular = _material_specular_glossiness_extension(material)
    return isinstance(specular, dict) and isinstance(specular.get("diffuseTexture"), dict)


def _material_alpha_is_fully_transparent(material: Any) -> bool:
    if not isinstance(material, dict):
        return False
    alpha_values: list[float] = []
    pbr = material.get("pbrMetallicRoughness")
    if isinstance(pbr, dict):
        factor = pbr.get("baseColorFactor")
        if isinstance(factor, list) and len(factor) >= 4:
            alpha_values.append(_float(factor[3], 1.0))
    specular = _material_specular_glossiness_extension(material)
    if isinstance(specular, dict):
        factor = specular.get("diffuseFactor")
        if isinstance(factor, list) and len(factor) >= 4:
            alpha_values.append(_float(factor[3], 1.0))
    return bool(alpha_values) and all(value <= 0.001 for value in alpha_values)


def _set_material_alpha(material: dict[str, Any], alpha: float) -> bool:
    changed = False
    pbr = material.get("pbrMetallicRoughness")
    if isinstance(pbr, dict):
        factor = pbr.get("baseColorFactor")
        if isinstance(factor, list) and len(factor) >= 4 and _float(factor[3], 1.0) <= 0.001:
            factor[3] = alpha
            changed = True
    specular = _material_specular_glossiness_extension(material)
    if isinstance(specular, dict):
        factor = specular.get("diffuseFactor")
        if isinstance(factor, list) and len(factor) >= 4 and _float(factor[3], 1.0) <= 0.001:
            factor[3] = alpha
            changed = True
    return changed


def _material_specular_glossiness_extension(material: dict[str, Any]) -> dict[str, Any] | None:
    extensions = material.get("extensions")
    if not isinstance(extensions, dict):
        return None
    specular = extensions.get("KHR_materials_pbrSpecularGlossiness")
    return specular if isinstance(specular, dict) else None


def _float(value: Any, default: float) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def _read_glb(glb_path: Path) -> tuple[dict[str, Any], list[tuple[bytes, bytes]]]:
    data = glb_path.read_bytes()
    if len(data) < 20:
        raise ConverterError("INVALID_GLB", "GLB output is too small", details=str(glb_path), http_status=500)
    magic, version, total_length = struct.unpack_from("<4sII", data, 0)
    if magic != b"glTF" or version != 2 or total_length != len(data):
        raise ConverterError("INVALID_GLB", "GLB output header is invalid", details=str(glb_path), http_status=500)

    offset = 12
    chunks: list[tuple[bytes, bytes]] = []
    json_doc: dict[str, Any] | None = None
    while offset + 8 <= len(data):
        chunk_length, chunk_type = struct.unpack_from("<I4s", data, offset)
        offset += 8
        chunk_data = data[offset : offset + chunk_length]
        offset += chunk_length
        chunks.append((chunk_type, chunk_data))
        if chunk_type == b"JSON":
            json_doc = json.loads(chunk_data.decode("utf-8").rstrip(" \t\r\n\0"))
    if json_doc is None:
        raise ConverterError("INVALID_GLB", "GLB output has no JSON chunk", details=str(glb_path), http_status=500)
    return json_doc, chunks


def _write_glb(glb_path: Path, doc: dict[str, Any], chunks: list[tuple[bytes, bytes]]) -> None:
    json_bytes = json.dumps(doc, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    json_bytes += b" " * ((4 - len(json_bytes) % 4) % 4)

    next_chunks: list[tuple[bytes, bytes]] = []
    json_written = False
    for chunk_type, chunk_data in chunks:
        if chunk_type == b"JSON":
            next_chunks.append((b"JSON", json_bytes))
            json_written = True
        else:
            padded = chunk_data + b"\x00" * ((4 - len(chunk_data) % 4) % 4)
            next_chunks.append((chunk_type, padded))
    if not json_written:
        next_chunks.insert(0, (b"JSON", json_bytes))

    total_length = 12 + sum(8 + len(chunk_data) for _, chunk_data in next_chunks)
    output = bytearray(struct.pack("<4sII", b"glTF", 2, total_length))
    for chunk_type, chunk_data in next_chunks:
        output += struct.pack("<I4s", len(chunk_data), chunk_type)
        output += chunk_data
    glb_path.write_bytes(output)


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

    if not path.is_file():
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
        _ensure_executable_available(command[0], CONVERTER_ENV)
        runner = run_command

    return _handle_command_result(runner(command, timeout_seconds))


def _run_executable(
    command: list[str],
    *,
    runner: CommandRunner | None,
    env_name: str,
    timeout_seconds: int | None,
) -> CommandResult:
    if runner is None:
        _ensure_executable_available(command[0], env_name)
        runner = run_command

    return _handle_command_result(runner(command, timeout_seconds))


def _handle_command_result(result: CommandResult) -> CommandResult:
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


def _mesh_converter_bin(env: dict[str, str] | None) -> str:
    values = env if env is not None else os.environ
    return _text(values.get(MESH_CONVERTER_ENV)) or DEFAULT_MESH_CONVERTER_BIN


def _ifc_converter_bin(env: dict[str, str] | None) -> str:
    values = env if env is not None else os.environ
    return _text(values.get(IFC_CONVERTER_ENV)) or DEFAULT_IFC_CONVERTER_BIN


def _gaussian_splat_node_bin(env: dict[str, str] | None) -> str:
    values = env if env is not None else os.environ
    configured = _text(values.get(GAUSSIAN_SPLAT_NODE_ENV))
    if configured:
        return configured
    return shutil.which("node") or "node"


def _executable_available(path: str) -> bool:
    if not _is_explicit_file_path(path):
        return False
    return Path(path).is_file() and os.access(path, os.X_OK)


def _ensure_executable_available(path: str, env_name: str) -> None:
    if _executable_available(path):
        return
    raise ConverterError(
        "CONVERTER_UNAVAILABLE",
        "model3d converter executable was not found",
        details=_executable_unavailable_detail(env_name, path),
        http_status=503,
    )


def _gaussian_splat_converter_available(node_bin: str) -> bool:
    if not _executable_available(node_bin):
        return False
    return Path(GAUSSIAN_SPLAT_CONVERTER_SCRIPT).is_file()


def _ensure_gaussian_splat_converter_available(node_bin: str) -> None:
    if _gaussian_splat_converter_available(node_bin):
        return
    raise ConverterError(
        "CONVERTER_UNAVAILABLE",
        "Gaussian splat KSplat converter is unavailable",
        details=_gaussian_splat_converter_unavailable_detail(node_bin),
        http_status=503,
    )


def _gaussian_splat_converter_unavailable_detail(node_bin: str) -> str:
    details = []
    if not _executable_available(node_bin):
        details.append(_executable_unavailable_detail(GAUSSIAN_SPLAT_NODE_ENV, node_bin))
    if not Path(GAUSSIAN_SPLAT_CONVERTER_SCRIPT).is_file():
        details.append(f"{GAUSSIAN_SPLAT_CONVERTER_SCRIPT} was not found")
    return "; ".join(details)


def _is_explicit_file_path(converter: str) -> bool:
    return bool(
        converter
        and (
            os.path.isabs(converter)
            or os.path.sep in converter
            or (os.path.altsep is not None and os.path.altsep in converter)
        )
    )


def _executable_unavailable_detail(env_name: str, path: str) -> str:
    if not _is_explicit_file_path(path):
        return f"{env_name} must point to the engine-bound executable file, not a PATH command name: {path}"
    return f"{path} was not found or is not executable"


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


def _int_option(options: dict[str, Any], key: str, default: int, min_value: int, max_value: int) -> int:
    value = options.get(key)
    if isinstance(value, bool):
        return default
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return default
    return max(min_value, min(max_value, parsed))


def _float_option(options: dict[str, Any], key: str, default: float, min_value: float, max_value: float) -> float:
    value = options.get(key)
    if isinstance(value, bool):
        return default
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        return default
    return max(min_value, min(max_value, parsed))


def _gaussian_splat_conversion_options(options: dict[str, Any]) -> dict[str, Any]:
    scene_center, source = _scene_center_option(options)
    if scene_center is None:
        scene_center = [0.0, 0.0, 0.0]
        source = "default"
    return {
        "section_size": _int_option(options, "section_size", 262144, 1, 2_147_483_647),
        "scene_center": scene_center,
        "scene_center_source": source,
        "block_size": _float_option(options, "block_size", 5.0, 0.000001, 1_000_000_000.0),
        "bucket_size": _int_option(options, "bucket_size", 256, 1, 2_147_483_647),
    }


def _scene_center_option(options: dict[str, Any]) -> tuple[list[float] | None, str]:
    for key in ("scene_center", "center"):
        center = _vector3_option(options.get(key))
        if center is not None:
            return center, key
    center = _bounds_center_option(options.get("bounds_3d"))
    if center is not None:
        return center, "bounds_3d"
    center = _bounds_center_option(options.get("sampled_bounds_3d"))
    if center is not None:
        return center, "sampled_bounds_3d"
    return None, ""


def _vector3_option(value: Any) -> list[float] | None:
    if isinstance(value, str):
        parts = [part.strip() for part in value.split(",")]
        if len(parts) != 3:
            return None
        return _float_vector(parts)
    if isinstance(value, (list, tuple)) and len(value) == 3:
        return _float_vector(value)
    if isinstance(value, dict):
        return _float_vector([value.get("x"), value.get("y"), value.get("z")])
    return None


def _bounds_center_option(value: Any) -> list[float] | None:
    if not isinstance(value, dict):
        return None
    min_values = _float_vector([value.get("min_x"), value.get("min_y"), value.get("min_z")])
    max_values = _float_vector([value.get("max_x"), value.get("max_y"), value.get("max_z")])
    if min_values is None or max_values is None:
        return None
    return [
        (min_values[0] + max_values[0]) / 2.0,
        (min_values[1] + max_values[1]) / 2.0,
        (min_values[2] + max_values[2]) / 2.0,
    ]


def _float_vector(values: list[Any] | tuple[Any, ...]) -> list[float] | None:
    vector: list[float] = []
    for value in values:
        if isinstance(value, bool):
            return None
        try:
            vector.append(float(value))
        except (TypeError, ValueError):
            return None
    return vector


def _scene_center_arg(values: list[float]) -> str:
    return ",".join(_number_arg(value) for value in values)


def _number_arg(value: float) -> str:
    return f"{value:.12g}"


def _json_object_from_stdout(stdout: str) -> dict[str, Any]:
    text = (stdout or "").strip()
    if not text:
        return {}
    try:
        parsed = json.loads(text)
    except json.JSONDecodeError:
        return {}
    return parsed if isinstance(parsed, dict) else {}


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
    if ext == ".ksplat":
        return "application/vnd.gaussian-ksplat"
    return "application/octet-stream"

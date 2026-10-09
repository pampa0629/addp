#!/usr/bin/env python3
"""Accept Business MinIO -> Manager infra artifact -> Monitor lineage through real services."""

from __future__ import annotations

import hashlib
import json
import math
import os
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable, Iterable, Mapping


TERMINAL_STATUSES = {"success", "failed", "timeout", "cancelled"}
POINTCLOUD_TASK_TYPE = "point_cloud_copc_generation"
PPTX_TASK_TYPE = "pptx_pdf_generation"
RASTER_TASK_TYPE = "raster_cog_generation"
MODEL_TASK_TYPE = "model_3d_glb_generation"
MAX_SOURCE_SHA256 = "916714595bf7a2953264b8afaafe339fa60bdb4944b4664f6802ccb288190653"
MAX_OPTIONS = {'source_unit': 'mm', 'texture_files': {'C:\\0ad\\binaries\\data\\mods\\official\\art\\textures\\skins\\skeletal\\horse_chestnut_a.dds': 'texture.png'}}
LINEAGE_SCHEMA = "addp.lineage-facts/v1"
REQUIRED_PERMISSIONS = {
    "manager.data_item.read",
    "manager.derived_artifact.create",
    "manager.derived_artifact.delete",
    "manager.derived_artifact.read",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "monitor.execution.read",
}
FORBIDDEN_ADMIN_ROLES = {
    "platform.audit_administrator",
    "platform.security_administrator",
    "platform.system_administrator",
    "tenant.administrator",
}


class SuiteError(RuntimeError):
    pass


@dataclass
class Response:
    status: int
    payload: Any
    headers: Mapping[str, str]
    raw: bytes = b""


@dataclass
class ArtifactFixture:
    format: str
    item: dict[str, object]
    locator: str
    task_id: int | None = None
    execution_id: str = ""
    result_id: int | None = None
    previous_mode: str = "basic_preview"
    mode_changed: bool = False
    artifact: dict[str, object] = field(default_factory=dict)
    cleanup: dict[str, object] = field(default_factory=lambda: {
        "result_deleted": False, "task_deleted": False,
        "content_unavailable": False, "preview_mode_restored": False,
        "residual_resources": -1,
    })



class GatewayClient:
    def __init__(self, base_url: str, token: str, timeout: float) -> None:
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise SuiteError("GATEWAY_URL must be an absolute HTTP(S) URL")
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout

    def request(
        self,
        method: str,
        path: str,
        expected: Iterable[int],
        body: dict[str, object] | None = None,
        headers: Mapping[str, str] | None = None,
    ) -> Response:
        data = None if body is None else json.dumps(body).encode()
        request_headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self.token}",
        }
        request_headers.update(headers or {})
        request = urllib.request.Request(
            self.base_url + path,
            data=data,
            method=method,
            headers=request_headers,
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                status = response.status
                raw = response.read()
                response_headers = dict(response.headers.items())
        except urllib.error.HTTPError as error:
            status = error.code
            raw = error.read()
            response_headers = dict(error.headers.items()) if error.headers else {}
        except (urllib.error.URLError, TimeoutError) as error:
            raise SuiteError(f"{method} {path} transport failed: {error}") from error
        content_type = next(
            (value for key, value in response_headers.items() if key.lower() == "content-type"),
            "",
        ).lower()
        payload: Any = {}
        if raw and ("json" in content_type or status >= 400):
            try:
                payload = json.loads(raw)
            except json.JSONDecodeError as error:
                raise SuiteError(f"{method} {path} returned invalid JSON") from error
        if status not in set(expected):
            code = payload.get("error_code", "unknown") if isinstance(payload, dict) else "unknown"
            raise SuiteError(f"{method} {path} returned HTTP {status} ({code})")
        return Response(status=status, payload=payload, headers=response_headers, raw=raw)


def _object(value: Any, resource: str) -> dict[str, object]:
    if not isinstance(value, dict):
        raise SuiteError(f"{resource} must be an object")
    return value


def _array(value: Any, resource: str) -> list[object]:
    if not isinstance(value, list):
        raise SuiteError(f"{resource} must be an array")
    return value


def positive_int(value: object, field: str) -> int:
    if isinstance(value, bool):
        raise SuiteError(f"{field} must be a positive integer")
    try:
        parsed = int(value)  # type: ignore[arg-type]
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a positive integer") from error
    if parsed <= 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical positive integer")
    return parsed


def non_negative_int(value: object, field: str) -> int:
    if isinstance(value, bool):
        raise SuiteError(f"{field} must be a non-negative integer")
    try:
        parsed = int(value)  # type: ignore[arg-type]
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a non-negative integer") from error
    if parsed < 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical non-negative integer")
    return parsed


def validate_user_identity(client: GatewayClient, tenant_id: int) -> dict[str, object]:
    context = _object(
        client.request("GET", "/api/v1/system/auth/context", (200,)).payload,
        "AuthContext",
    )
    principal = _object(context.get("principal"), "AuthContext principal")
    tenant = _object(context.get("context"), "AuthContext context")
    token = _object(context.get("token"), "AuthContext token")
    authorization = _object(context.get("authorization"), "AuthContext authorization")
    if principal.get("type") != "user":
        raise SuiteError("Online Manager lineage token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online Manager lineage token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online Manager lineage token must be a User Access Token")
    roles: set[str] = set()
    permissions: set[str] = set()
    for assignment in _array(authorization.get("role_assignments"), "AuthContext role_assignments"):
        item = _object(assignment, "AuthContext role assignment")
        role = item.get("role_key")
        granted = item.get("permissions")
        if not isinstance(role, str) or not isinstance(granted, list) or not all(isinstance(key, str) for key in granted):
            raise SuiteError("AuthContext role assignment is incomplete")
        roles.add(role)
        permissions.update(granted)
    forbidden = roles & FORBIDDEN_ADMIN_ROLES
    if forbidden:
        raise SuiteError("Online Manager lineage token must not use administrator roles: " + ", ".join(sorted(forbidden)))
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError("Online Manager lineage token is missing required permissions: " + ", ".join(sorted(missing)))
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def wait_for_meta_scan(client: GatewayClient, engine_id: int, deadline: float) -> str:
    run = _object(
        client.request(
            "POST",
            "/api/v1/meta/scan/run/manual",
            (201,),
            {"engine_id": engine_id, "scan_depth": "deep", "trigger_type": "manual", "force": True},
        ).payload,
        "Meta scan execution",
    )
    execution_id = run.get("execution_id")
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError("Meta scan execution_id is missing")
    while time.monotonic() < deadline:
        execution = _object(
            client.request("GET", f"/api/v1/meta/executions/{urllib.parse.quote(execution_id)}", (200,)).payload,
            "Meta execution",
        )
        status = execution.get("status")
        if status == "success":
            return execution_id
        if status in TERMINAL_STATUSES:
            raise SuiteError(f"Meta scan ended with status {status}")
        time.sleep(1)
    raise SuiteError("Meta scan did not finish before the convergence timeout")


def find_fixture_item(client: GatewayClient, engine_id: int, full_name: str, label: str) -> dict[str, object]:
    items = _array(
        client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload,
        "Meta engine items",
    )
    matches = [item for item in items if isinstance(item, dict) and item.get("full_name") == full_name]
    if len(matches) != 1:
        raise SuiteError(f"Meta must return exactly one {label} fixture {full_name}")
    item = matches[0]
    if item.get("item_type") not in {"object", "file"}:
        raise SuiteError(f"{label} fixture must be an object or file DataItem")
    fingerprint = item.get("fingerprint")
    if not isinstance(fingerprint, str) or not fingerprint:
        raise SuiteError(f"{label} fixture fingerprint is missing")
    positive_int(item.get("id"), f"{label} fixture id")
    size = item.get("size_bytes")
    if size is not None and positive_int(size, f"{label} fixture size_bytes") <= 0:
        raise SuiteError(f"{label} fixture size_bytes must be positive")
    return item


def build_item_locator(engine_id: int, item: Mapping[str, object]) -> str:
    item_id = positive_int(item.get("id"), "fixture item id")
    full_name = item.get("full_name")
    if not isinstance(full_name, str) or not full_name.strip():
        raise SuiteError("fixture full_name is missing")
    path = urllib.parse.quote(full_name.strip("/"), safe="/")
    return f"addp://engine/{engine_id}/path/{path}?type=object&item_id={item_id}"


def assert_no_existing_resources(client: GatewayClient, fingerprint: str) -> None:
    tasks = _object(
        client.request("GET", f"/api/v1/manager/tasks?task_type={POINTCLOUD_TASK_TYPE}&page=1&page_size=100", (200,)).payload,
        "PointCloud task list",
    )
    for task in _array(tasks.get("items"), "PointCloud task list items"):
        if not isinstance(task, dict):
            continue
        config = task.get("config")
        source = config.get("source") if isinstance(config, dict) else None
        if isinstance(source, dict) and source.get("item_fingerprint") == fingerprint:
            raise SuiteError("a stale PointCloud task exists for the dedicated fixture")
    query = urllib.parse.urlencode({"item_fingerprint": fingerprint, "page": 1, "page_size": 100})
    results = _object(
        client.request("GET", f"/api/v1/manager/point_cloud_copc?{query}", (200,)).payload,
        "PointCloud result list",
    )
    if non_negative_int(results.get("total"), "PointCloud result total") != 0:
        raise SuiteError("a stale PointCloud result exists for the dedicated fixture")


def wait_for_manager_execution(client: GatewayClient, execution_id: str, deadline: float, label: str) -> dict[str, object]:
    while time.monotonic() < deadline:
        execution = _object(
            client.request("GET", f"/api/v1/manager/executions/{urllib.parse.quote(execution_id)}", (200,)).payload,
            "Manager execution",
        )
        status = execution.get("status")
        if status == "success":
            return execution
        if status in TERMINAL_STATUSES:
            details = execution.get("error_details") or execution.get("metadata")
            raise SuiteError(f"Manager {label} execution ended with status {status}: {details!r}")
        time.sleep(1)
    raise SuiteError(f"Manager {label} execution did not finish before the convergence timeout")


def validate_lineage(
    execution: Mapping[str, object],
    *,
    item_locator: str,
    item_id: int,
    fingerprint: str,
    tenant_id: int,
    task_type: str,
    output_prefix: str,
    output_suffix: str,
    output_label: str,
) -> dict[str, object]:
    metadata = _object(execution.get("metadata"), "execution metadata")
    facts = _object(metadata.get("lineage_facts"), "execution lineage_facts")
    if facts.get("schema_version") != LINEAGE_SCHEMA:
        raise SuiteError("execution lineage_facts schema_version is invalid")
    inputs = _array(facts.get("inputs"), "execution lineage inputs")
    outputs = _array(facts.get("outputs"), "execution lineage outputs")
    operations = _array(facts.get("operations"), "execution lineage operations")
    if len(inputs) != 1 or len(outputs) != 1 or len(operations) != 1:
        raise SuiteError("execution lineage must contain exactly one input, output and operation")
    input_ref = _object(inputs[0], "execution lineage input")
    expected_input = {
        "port": "source",
        "locator": item_locator,
        "item_id": item_id,
        "item_fingerprint": fingerprint,
    }
    for key, value in expected_input.items():
        if input_ref.get(key) != value:
            raise SuiteError(f"execution lineage input {key} is invalid")
    output_ref = _object(outputs[0], "execution lineage output")
    output_locator = output_ref.get("locator")
    expected_prefix = f"addp-infra://minio/manager/tenant_{tenant_id}/{output_prefix}/"
    if output_ref.get("port") != "result" or not isinstance(output_locator, str) or not output_locator.startswith(expected_prefix):
        raise SuiteError(f"execution lineage output is not the Manager infra {output_label} artifact")
    parsed_output = urllib.parse.urlsplit(output_locator)
    if urllib.parse.parse_qs(parsed_output.query) != {"type": ["object"]} or not parsed_output.path.endswith(output_suffix):
        raise SuiteError(f"execution lineage output must be a {output_label} object locator")
    operation = _object(operations[0], "execution lineage operation")
    expected_operation = {
        "kind": "derive",
        "operator": task_type,
        "input_ports": ["source"],
        "output_ports": ["result"],
    }
    for key, value in expected_operation.items():
        if operation.get(key) != value:
            raise SuiteError(f"execution lineage operation {key} is invalid")
    return facts


def validate_monitor_lineage(
    execution: dict[str, object], owner_facts: dict[str, object]
) -> None:
    forbidden_fields = {
        "execution_config", "execution_authorization_id", "actor_principal_id",
        "actor_tenant_membership_id", "issued_authorization_version",
        "lease_token", "lease_owner", "lease_expires_at",
    }
    if forbidden_fields.intersection(execution):
        raise SuiteError("Monitor exposes private execution fields")
    metadata = _object(execution.get("metadata"), "Monitor execution metadata")
    if {"result", "outputs", "step_results"}.intersection(metadata):
        raise SuiteError("Monitor exposes private execution metadata")
    facts = _object(metadata.get("lineage_facts"), "Monitor lineage facts")
    if facts.get("schema_version") != LINEAGE_SCHEMA:
        raise SuiteError("Monitor lineage schema_version is invalid")
    if facts.get("operations") not in (None, []):
        raise SuiteError("Monitor exposes lineage operations")
    resource_fields = {
        "port", "locator", "item_id", "item_fingerprint", "field_name", "write_mode",
    }
    for direction in ("inputs", "outputs"):
        resources = _array(facts.get(direction), f"Monitor lineage {direction}")
        expected_resources = _array(owner_facts.get(direction), f"Owner lineage {direction}")
        if len(resources) != len(expected_resources):
            raise SuiteError(f"Monitor lineage {direction} count differs from Owner")
        for resource, expected in zip(resources, expected_resources):
            actual = dict(_object(resource, "Monitor lineage resource"))
            owner = _object(expected, "Owner lineage resource")
            if set(actual) - resource_fields:
                raise SuiteError("Monitor exposes private lineage resource fields")
            projected = {key: value for key, value in owner.items() if key in resource_fields}
            for candidate in (actual, projected):
                locator = candidate.get("locator")
                if not isinstance(locator, str):
                    raise SuiteError("Monitor lineage resource locator is missing")
                uri = urllib.parse.urlsplit(locator)
                query = urllib.parse.parse_qs(uri.query, keep_blank_values=True)
                if uri.username is not None or uri.fragment or set(query) - {"type", "item_id"}:
                    raise SuiteError("Monitor lineage locator contains private context")
                candidate["locator"] = (uri.scheme, uri.netloc, uri.path, query)
            if actual != projected:
                raise SuiteError("Monitor lineage resource differs from Owner safe projection")


def prepare_model_fixture(client: GatewayClient, engine_id: int, bucket: str, format_name: str) -> ArtifactFixture:
    full_name = f"{bucket}/model3d/{format_name}/model.{format_name}"
    item = find_fixture_item(client, engine_id, full_name, format_name.upper())
    attributes = _object(item.get("attributes"), f"{format_name} attributes")
    facts = _object(attributes.get("item"), f"{format_name} item facts")
    if any(facts.get(key) != value for key, value in {
        "data_type": "model_3d", "format": format_name, "layout": "single",
    }.items()):
        raise SuiteError(f"{format_name} fixture must be scanned as model_3d/single")
    if format_name in {"skp", "max"} and (_object(attributes.get("format_info", {}), f"{format_name} format_info").get(format_name) is not None
            or _object(attributes.get("type_info", {}), f"{format_name} type_info").get("model_3d") is not None):
        raise SuiteError(f"{format_name.upper()} identity scan must not invent native metadata before Runtime parsing")
    if format_name in {"dae", "3ds", "ifc"}:
        format_info = _object(attributes.get("format_info"), f"{format_name} format_info")
        native = _object(format_info.get(format_name), f"{format_name} native metadata")
        if native.get("scan_complete") is not True:
            raise SuiteError(f"{format_name} metadata scan must be complete")
        if format_name in {"dae", "3ds"} and native.get("texture_refs") != ["texture.png"]:
            raise SuiteError(f"{format_name} scan must preserve its declared PNG texture")
        if format_name == "dae" and native.get("unit_meter") != 0.01:
            raise SuiteError("DAE fixture must preserve centimeter units")
        if format_name == "ifc" and (native.get("schema_identifiers") != ["IFC4"]
                or native.get("schema_version") != "IFC4" or native.get("entity_count") != 16
                or _object(native.get("entity_type_counts"), "IFC entity types").get("IFCBUILDINGELEMENTPROXY") != 1):
            raise SuiteError("IFC fixture must preserve its IFC4 schema and 16-entity BIM summary")
    query = urllib.parse.urlencode({"item_fingerprint": item["fingerprint"], "page": 1, "page_size": 100})
    results = _object(client.request("GET", f"/api/v1/manager/model_3d_glb?{query}", (200,)).payload, "initial model results")
    if non_negative_int(results.get("total"), "initial model result total") != 0:
        raise SuiteError(f"a stale {format_name} GLB exists for the dedicated fixture")
    query = urllib.parse.urlencode({"task_type": MODEL_TASK_TYPE, "page": 1, "page_size": 100})
    tasks = _object(client.request("GET", f"/api/v1/manager/tasks?{query}", (200,)).payload, "initial model tasks")
    rows = _array(tasks.get("items"), "initial model task items")
    if non_negative_int(tasks.get("total"), "initial model task total") != len(rows):
        raise SuiteError("model task preflight must inspect the complete task list")
    for row in rows:
        task = _object(row, "initial model task")
        source = _object(_object(task.get("config"), "model task config").get("source"), "model task source")
        if source.get("item_fingerprint") == item["fingerprint"]:
            raise SuiteError(f"a stale {format_name} task exists for the dedicated fixture")
    return ArtifactFixture(format_name, item, build_item_locator(engine_id, item))


def validate_max_png(raw: bytes) -> None:
    """Decode the bounded fixture pixels, allowing Blender to re-encode PNG."""
    import zlib
    if not raw.startswith(b'\x89PNG\r\n\x1a\n'):
        raise SuiteError('MAX fixture texture must be PNG')
    offset, compressed, channels, ended = 8, bytearray(), None, False
    while offset + 12 <= len(raw):
        length, kind = struct.unpack_from('>I4s', raw, offset)
        end = offset + 12 + length
        if end > len(raw):
            raise SuiteError('MAX fixture PNG chunk is truncated')
        data = raw[offset + 8:end - 4]
        if zlib.crc32(kind + data) != struct.unpack_from('>I', raw, end - 4)[0]:
            raise SuiteError('MAX fixture PNG checksum differs')
        if kind == b'IHDR':
            if channels is not None or length != 13:
                raise SuiteError('MAX fixture PNG header is invalid')
            width, height, depth, color, compression, filtering, interlace = struct.unpack('>2I5B', data)
            if (width, height, depth, compression, filtering, interlace) != (4, 4, 8, 0, 0, 0) or color not in (2, 6):
                raise SuiteError('MAX fixture PNG must be 4 by 4 RGB or RGBA')
            channels = 3 if color == 2 else 4
        elif kind == b'IDAT':
            compressed.extend(data)
        elif kind == b'IEND':
            if length or end != len(raw):
                raise SuiteError('MAX fixture PNG ending is invalid')
            ended = True
            break
        offset = end
    if not ended or channels is None:
        raise SuiteError('MAX fixture PNG is incomplete')
    decoder = zlib.decompressobj()
    decoded = decoder.decompress(compressed, 4 * (1 + 4 * channels) + 1)
    if not decoder.eof or decoder.unused_data or len(decoded) != 4 * (1 + 4 * channels):
        raise SuiteError('MAX fixture PNG decoded pixels exceed fixture bounds')
    previous = [0] * (4 * channels)
    for row in range(4):
        start = row * (1 + 4 * channels)
        filtering = decoded[start]
        if filtering > 4:
            raise SuiteError('MAX fixture PNG filter is invalid')
        current = list(decoded[start + 1:start + 1 + 4 * channels])
        for i in range(len(current)):
            left = current[i - channels] if i >= channels else 0
            up = previous[i]
            corner = previous[i - channels] if i >= channels else 0
            predictor = left + up - corner
            distances = [abs(predictor - x) for x in (left, up, corner)]
            paeth = (left, up, corner)[distances.index(min(distances))]
            current[i] = (current[i] + (0, left, up, (left + up) // 2, paeth)[filtering]) % 256
        for i in range(0, len(current), channels):
            if current[i:i + channels] != [220, 30, 60] + ([255] if channels == 4 else []):
                raise SuiteError('MAX fixture must preserve the declared red texture pixels')
        previous = current


def validate_max_glb(doc: dict, view_bytes: Callable, size: int) -> dict[str, object]:
    """Check this licensed static fixture, including real Blender TRS transforms."""
    if doc.get('animations') or doc.get('skins'):
        raise SuiteError('MAX fixture must export only static geometry')
    images = _array(doc.get('images'), 'MAX images')
    if len(images) != 1:
        raise SuiteError('MAX fixture must preserve one embedded PNG')
    image = _object(images[0], 'MAX embedded image')
    if image.get('mimeType') != 'image/png' or 'uri' in image:
        raise SuiteError('MAX fixture must preserve one embedded PNG')
    validate_max_png(view_bytes(image.get('bufferView'))[0])
    accessors = _array(doc.get('accessors'), 'MAX accessors')
    def values(index, kind, component):
        index = non_negative_int(index, 'MAX accessor index')
        if index >= len(accessors):
            raise SuiteError('MAX accessor index is out of bounds')
        item = _object(accessors[index], 'MAX accessor')
        if item.get('type') != kind or item.get('componentType') != component or 'sparse' in item:
            raise SuiteError('MAX fixture accessor encoding differs')
        count = positive_int(item.get('count'), 'MAX accessor count')
        if count > 4096:
            raise SuiteError('MAX accessor exceeds fixture budget')
        encoding = {5126: 'f', 5121: 'B', 5123: 'H', 5125: 'I'}[component]
        width = {'VEC3': 3, 'VEC2': 2, 'SCALAR': 1}[kind]
        packed = struct.calcsize(encoding) * width
        data, view = view_bytes(item.get('bufferView'))
        offset = non_negative_int(item.get('byteOffset', 0), 'MAX accessor offset')
        stride = positive_int(view.get('byteStride', packed), 'MAX accessor stride')
        if stride < packed or offset + (count - 1) * stride + packed > len(data):
            raise SuiteError('MAX fixture accessor bytes are truncated')
        result = [struct.unpack_from('<' + str(width) + encoding, data, offset + i * stride) for i in range(count)]
        if any(not math.isfinite(v) for row in result for v in row):
            raise SuiteError('MAX fixture coordinates must be finite')
        return result
    materials = _array(doc.get('materials'), 'MAX materials')
    textures = _array(doc.get('textures'), 'MAX textures')
    meshes = _array(doc.get('meshes'), 'MAX meshes')
    if len(meshes) != 5:
        raise SuiteError('MAX fixture must retain five static meshes')
    mesh_points, counts, index_counts, textured = [], [], [], 0
    for mesh in meshes:
        mesh = _object(mesh, 'MAX mesh')
        points = []
        for primitive in _array(mesh.get('primitives'), 'MAX primitives'):
            primitive = _object(primitive, 'MAX primitive')
            attributes = _object(primitive.get('attributes'), 'MAX attributes')
            if primitive.get('mode', 4) != 4:
                raise SuiteError('MAX fixture must retain triangles')
            positions = values(attributes.get('POSITION'), 'VEC3', 5126)
            index = non_negative_int(primitive.get('indices'), 'MAX indices accessor')
            if index >= len(accessors) or _object(accessors[index], 'MAX indices').get('componentType') not in (5121, 5123, 5125):
                raise SuiteError('MAX indices must be bounded unsigned integers')
            indices = values(index, 'SCALAR', accessors[index]['componentType'])
            if any(row[0] >= len(positions) for row in indices):
                raise SuiteError('MAX triangle index exceeds vertex count')
            counts.append(len(positions)); index_counts.append(len(indices)); points.extend(positions)
            if 'material' not in primitive:
                continue
            material_index = non_negative_int(primitive['material'], 'MAX material index')
            if material_index >= len(materials):
                raise SuiteError('MAX material is out of bounds')
            pbr = _object(_object(materials[material_index], 'MAX material').get('pbrMetallicRoughness', {}), 'MAX PBR')
            if 'baseColorTexture' in pbr:
                texture = _object(pbr['baseColorTexture'], 'MAX diffuse texture')
                texture_index = non_negative_int(texture.get('index'), 'MAX texture index')
                if texture_index >= len(textures) or _object(textures[texture_index], 'MAX texture').get('source') != 0 or texture.get('texCoord', 0) != 0:
                    raise SuiteError('MAX diffuse texture must reference embedded PNG and UV zero')
                if len(values(attributes.get('TEXCOORD_0'), 'VEC2', 5126)) != len(positions):
                    raise SuiteError('MAX texture UV count differs')
                textured += 1
        mesh_points.append(points)
    if sorted(map(len, mesh_points)) != [4, 4, 24, 206, 206]:
        raise SuiteError('MAX fixture must retain geometry in all five meshes')
    if sorted(counts) != sorted([4] * 8 + [16, 190] * 2) or sorted(index_counts) != sorted([6] * 8 + [24, 912] * 2) or textured != 2:
        raise SuiteError('MAX fixture static primitive geometry or textured mesh count differs')
    identity = [1., 0., 0., 0., 0., 1., 0., 0., 0., 0., 1., 0., 0., 0., 0., 1.]
    def vector(value, length):
        if not isinstance(value, list) or len(value) != length or any(type(v) not in (int, float) or not math.isfinite(v) for v in value):
            raise SuiteError('MAX transform must contain finite coordinates')
        return value
    nodes, scenes = _array(doc.get('nodes'), 'MAX nodes'), _array(doc.get('scenes'), 'MAX scenes')
    if len(scenes) != 1 or doc.get('scene', 0) != 0:
        raise SuiteError('MAX fixture must retain one active scene')
    if len(nodes) > 512:
        raise SuiteError('MAX nodes exceed fixture budget')
    roots = _array(_object(scenes[0], 'MAX scene').get('nodes'), 'MAX scene roots')
    if len(roots) != 1 or type(roots[0]) is not int or not 0 <= roots[0] < len(nodes):
        raise SuiteError('MAX fixture must retain its unit root')
    root = _object(nodes[roots[0]], 'MAX unit root')
    scale = vector(root.get('scale'), 3)
    if root.get('name') != 'ADDP source unit to meters' or any(not math.isclose(v, .001, rel_tol=1e-6) for v in scale):
        raise SuiteError('MAX fixture must apply user millimetres to metres')
    world_points, instances, visited = [], [], set()
    def visit(index, parent):
        index = non_negative_int(index, 'MAX node index')
        if index >= len(nodes) or index in visited:
            raise SuiteError('MAX scene contains repeated nodes or cycles')
        visited.add(index)
        node = _object(nodes[index], 'MAX node')
        if 'matrix' in node:
            if any(key in node for key in ('scale', 'rotation', 'translation')):
                raise SuiteError('MAX node cannot combine matrix and TRS')
            local = vector(node['matrix'], 16)
        else:
            x, y, z, w = vector(node.get('rotation', [0., 0., 0., 1.]), 4)
            if not math.isclose(x*x + y*y + z*z + w*w, 1., abs_tol=1e-5):
                raise SuiteError('MAX rotation must be normalized')
            sx, sy, sz = vector(node.get('scale', [1., 1., 1.]), 3)
            tx, ty, tz = vector(node.get('translation', [0., 0., 0.]), 3)
            local = [(1-2*(y*y+z*z))*sx, 2*(x*y+z*w)*sx, 2*(x*z-y*w)*sx, 0.,
                     2*(x*y-z*w)*sy, (1-2*(x*x+z*z))*sy, 2*(y*z+x*w)*sy, 0.,
                     2*(x*z+y*w)*sz, 2*(y*z-x*w)*sz, (1-2*(x*x+y*y))*sz, 0., tx, ty, tz, 1.]
        world = [sum(parent[k*4+row] * local[column*4+k] for k in range(4)) for column in range(4) for row in range(4)]
        if 'mesh' in node:
            mesh = non_negative_int(node['mesh'], 'MAX mesh index')
            if mesh >= len(meshes) or 'skin' in node:
                raise SuiteError('MAX scene must reference static fixture meshes')
            instances.append(mesh)
            world_points.extend(tuple(sum(world[k*4+axis]*p[k] for k in range(3))+world[12+axis] for axis in range(3)) for p in mesh_points[mesh])
        for child in _array(node.get('children', []), 'MAX children'):
            visit(child, world)
    visit(roots[0], identity)
    if sorted(instances) != list(range(5)):
        raise SuiteError('MAX scene must render all five meshes once')
    extent = [max(p[i] for p in world_points)-min(p[i] for p in world_points) for i in range(3)]
    if any(not math.isclose(a, b, rel_tol=1e-5, abs_tol=1e-7) for a, b in zip(extent, [.007146491023, .004421921513, .006392791685])):
        raise SuiteError('MAX fixture extent must preserve millimetre conversion and node transforms')
    return {'vertex_count': sum(counts), 'extent': extent, 'mesh_count': 5, 'primitive_count': 12,
            'instance_count': 5, 'embedded_images': 1, 'image_mime_type': 'image/png', 'size_bytes': size}


def validate_max_conversion(execution: dict) -> dict:
    metadata = _object(execution.get('metadata'), 'MAX execution metadata')
    conversion = _object(_object(metadata.get('glb_facts'), 'MAX GLB facts').get('conversion'), 'MAX conversion audit')
    expected = {'source_unit': 'mm', 'unit_source': 'user', 'scale_to_meters': .001,
                'importer_version': '1.9.2', 'texture_refs': list(MAX_OPTIONS['texture_files']),
                'source_sha256': MAX_SOURCE_SHA256, 'meshes': 5, 'scope': 'static'}
    if any(conversion.get(k) != v for k, v in expected.items()):
        raise SuiteError('MAX conversion audit must match the declared unit, texture and source')
    runtime = _object(metadata.get('workflow_runtime'), 'MAX workflow runtime')
    if any(runtime.get(k) != v for k, v in {'engine_type': 'model3d_workflow', 'operator': 'max_to_glb', 'mode': 'direct'}.items()):
        raise SuiteError('MAX must use the real direct Model3D operator')
    return expected


def validate_model_glb(raw: bytes, format_name: str) -> dict[str, object]:
    if format_name not in {"dae", "3ds", "ifc", "osgb", "skp", "max"}:
        raise SuiteError("unexpected model fixture format")
    if not 20 <= len(raw) <= 2 * 1024 * 1024:
        raise SuiteError("fixture GLB content must be nonempty and bounded to 2 MiB")
    magic, version, total = struct.unpack_from("<4sII", raw)
    json_size, json_kind = struct.unpack_from("<I4s", raw, 12)
    if magic != b"glTF" or version != 2 or total != len(raw) or json_kind != b"JSON":
        raise SuiteError("fixture content must be GLB 2 with a leading JSON chunk")
    binary_offset = 20 + json_size
    if json_size % 4 or binary_offset + 8 > len(raw):
        raise SuiteError("fixture GLB JSON chunk is truncated or unaligned")
    doc = _object(json.loads(raw[20:binary_offset]), "fixture GLB document")
    binary_size, binary_kind = struct.unpack_from("<I4s", raw, binary_offset)
    binary = raw[binary_offset + 8:]
    if binary_kind != b"BIN\0" or binary_size != len(binary):
        raise SuiteError("fixture GLB must contain exactly one embedded BIN chunk")
    buffers = _array(doc.get("buffers"), "fixture GLB buffers")
    if len(buffers) != 1 or "uri" in _object(buffers[0], "fixture GLB buffer"):
        raise SuiteError("fixture GLB must not depend on external buffers")
    declared_size = positive_int(buffers[0].get("byteLength"), "fixture buffer length")
    if not declared_size <= len(binary) <= declared_size + 3:
        raise SuiteError("fixture embedded buffer length differs from BIN chunk")
    views = _array(doc.get("bufferViews"), "fixture GLB bufferViews")
    def view_bytes(index):
        index = non_negative_int(index, "fixture bufferView index")
        if index >= len(views):
            raise SuiteError("fixture bufferView index is out of bounds")
        view = _object(views[index], "fixture bufferView")
        start = non_negative_int(view.get("byteOffset", 0), "fixture bufferView offset")
        length = positive_int(view.get("byteLength"), "fixture bufferView length")
        if view.get("buffer") != 0 or start + length > declared_size:
            raise SuiteError("fixture bufferView exceeds embedded bytes")
        return binary[start:start + length], view
    if format_name == "max":
        return validate_max_glb(doc, view_bytes, len(raw))
    images = _array(doc.get("images", []), "fixture GLB images")
    mime_type = "image/jpeg" if format_name == "osgb" else "image/png"
    image_count = 0 if format_name == "ifc" else 1
    if len(images) != image_count:
        raise SuiteError(f"fixture GLB must preserve exactly {image_count} embedded images")
    for image in images:
        image = _object(image, "fixture image")
        if "uri" in image or image.get("mimeType") != mime_type:
            raise SuiteError("fixture image must use the expected embedded image bufferView")
        image_bytes, _ = view_bytes(image.get("bufferView"))
        signature = b"\xff\xd8\xff" if format_name == "osgb" else b"\x89PNG\r\n\x1a\n"
        if not image_bytes.startswith(signature):
            raise SuiteError("fixture image bufferView must contain real encoded image bytes")
        if format_name == "skp" and hashlib.sha256(image_bytes).hexdigest() != "3168c3aa8cd3338d29b9ca74ee6794a2290b69d4da04d0869788187cb6ac4cf3":
            raise SuiteError("SKP fixture must preserve its embedded four-color PNG")
    textures = _array(doc.get("textures", []), "fixture textures")
    materials = _array(doc.get("materials"), "fixture materials")
    accessors = _array(doc.get("accessors"), "fixture accessors")
    meshes = _array(doc.get("meshes"), "fixture meshes")
    if len(meshes) != 1:
        raise SuiteError("fixture must retain exactly one mesh")
    primitives = _array(_object(meshes[0], "fixture mesh").get("primitives"), "fixture primitives")
    if len(primitives) != 1:
        raise SuiteError("fixture must retain exactly one primitive")
    primitive = _object(primitives[0], "fixture primitive")
    attributes = _object(primitive.get("attributes"), "fixture attributes")
    def accessor(index):
        index = non_negative_int(index, "fixture accessor index")
        if index >= len(accessors):
            raise SuiteError("fixture accessor index is out of bounds")
        return _object(accessors[index], "fixture accessor")
    position = accessor(attributes.get("POSITION"))
    expected_vertices = 24 if format_name == "ifc" else 3
    if (position.get("type") != "VEC3" or position.get("componentType") != 5126
            or position.get("count") != expected_vertices or primitive.get("mode", 4) != 4):
        raise SuiteError("fixture mesh must preserve its triangle geometry and vertex count")
    position_bytes, position_view = view_bytes(position.get("bufferView"))
    offset = non_negative_int(position.get("byteOffset", 0), "fixture POSITION offset")
    stride = positive_int(position_view.get("byteStride", 12), "fixture POSITION stride")
    if stride < 12 or offset + (expected_vertices - 1) * stride + 12 > len(position_bytes):
        raise SuiteError("fixture POSITION bytes are truncated")
    points = [struct.unpack_from("<3f", position_bytes, offset + i * stride) for i in range(expected_vertices)]
    if not all(math.isfinite(value) for point in points for value in point):
        raise SuiteError("fixture POSITION must contain finite coordinates")
    # Positions are mesh-local: Assimp retains DAE centimeters and puts the
    # meter conversion on the scene root. Verify dimensions after scene transforms.
    nodes = _array(doc.get("nodes"), "fixture nodes")
    scenes = _array(doc.get("scenes"), "fixture scenes")
    if len(scenes) != 1:
        raise SuiteError("fixture must retain exactly one scene")
    identity = [1., 0., 0., 0., 0., 1., 0., 0., 0., 0., 1., 0., 0., 0., 0., 1.]
    world_points = []
    def visit(index, parent, ancestors):
        index = non_negative_int(index, "fixture node index")
        if index >= len(nodes) or index in ancestors:
            raise SuiteError("fixture scene contains an invalid node or cycle")
        node = _object(nodes[index], "fixture node")
        if any(key in node for key in ("translation", "rotation")) or ("scale" in node and format_name != "skp"):
            raise SuiteError("fixture converters must encode transforms as matrices or SKP root scale")
        local = _array(node.get("matrix", identity), "fixture node matrix")
        if "scale" in node:
            if "matrix" in node or node["scale"] != [0.001] * 3:
                raise SuiteError("SKP fixture must use its millimetres-to-metres root scale")
            local = list(identity)
            local[0] = local[5] = local[10] = 0.001
        if len(local) != 16 or any(not isinstance(value, (int, float)) or not math.isfinite(value) for value in local):
            raise SuiteError("fixture node matrix must contain sixteen finite numbers")
        world = [sum(parent[k * 4 + row] * local[column * 4 + k] for k in range(4))
                 for column in range(4) for row in range(4)]
        if "mesh" in node:
            if node["mesh"] != 0:
                raise SuiteError("fixture node must reference its only mesh")
            world_points.extend(tuple(sum(world[k * 4 + axis] * point[k] for k in range(3)) + world[12 + axis]
                                      for axis in range(3)) for point in points)
        for child in _array(node.get("children", []), "fixture node children"):
            visit(child, world, ancestors | {index})
    for node_index in _array(_object(scenes[0], "fixture scene").get("nodes"), "fixture scene roots"):
        visit(node_index, identity, set())
    expected_instances = 2 if format_name == "skp" else 1
    if len(world_points) != expected_vertices * expected_instances:
        raise SuiteError(f"fixture scene must render its only mesh exactly {expected_instances} times")
    extent = [max(point[axis] for point in world_points) - min(point[axis] for point in world_points) for axis in range(3)]
    expected_extent = [1., 1., 1.] if format_name == "ifc" else [1., 1., 0.] if format_name == "osgb" else [1., 2., 0.]
    if format_name == "skp":
        expected_extent = [0.0762, 0., 0.0508]
        if any(not math.isclose(a, b, abs_tol=1e-6) for a, b in zip(extent, expected_extent)):
            raise SuiteError("SKP fixture must preserve inch-to-metre dimensions, instance translation and Y-up axes")
    if any(not math.isclose(actual, expected, abs_tol=1e-5) for actual, expected in zip(sorted(extent), sorted(expected_extent))):
        raise SuiteError("fixture geometry extent must preserve its source dimensions")
    if "indices" in primitive:
        indices = accessor(primitive["indices"])
        expected_indices = 36 if format_name == "ifc" else 3
        if indices.get("count") != expected_indices or indices.get("type") != "SCALAR":
            raise SuiteError("fixture triangle index count changed")
        index_bytes, _ = view_bytes(indices.get("bufferView"))
        index_encoding = {5121: "B", 5123: "H", 5125: "I"}.get(indices.get("componentType"))
        if index_encoding is None:
            raise SuiteError("fixture triangle indices must use unsigned integers")
        index_offset = non_negative_int(indices.get("byteOffset", 0), "fixture index offset")
        index_length = expected_indices * struct.calcsize(index_encoding)
        if index_offset + index_length > len(index_bytes):
            raise SuiteError("fixture triangle index bytes are truncated")
        decoded_indices = struct.unpack_from("<" + str(expected_indices) + index_encoding, index_bytes, index_offset)
        if any(index >= expected_vertices for index in decoded_indices):
            raise SuiteError("fixture triangle index exceeds vertex count")
    elif format_name == "ifc":
        raise SuiteError("IFC cube must preserve its twelve indexed triangles")
    if image_count:
        if "TEXCOORD_0" not in attributes or accessor(attributes["TEXCOORD_0"]).get("count") != expected_vertices:
            raise SuiteError("fixture textured mesh must preserve UV coordinates")
        uv = accessor(attributes["TEXCOORD_0"])
        uv_bytes, uv_view = view_bytes(uv.get("bufferView"))
        uv_offset = non_negative_int(uv.get("byteOffset", 0), "fixture UV offset")
        uv_stride = positive_int(uv_view.get("byteStride", 8), "fixture UV stride")
        if (uv.get("type") != "VEC2" or uv.get("componentType") != 5126 or uv_stride < 8
                or uv_offset + (expected_vertices - 1) * uv_stride + 8 > len(uv_bytes)):
            raise SuiteError("fixture UV coordinates must have bounded float VEC2 bytes")
        if not all(math.isfinite(value) for i in range(expected_vertices)
                   for value in struct.unpack_from("<2f", uv_bytes, uv_offset + i * uv_stride)):
            raise SuiteError("fixture UV coordinates must be finite")
        material_index = non_negative_int(primitive.get("material"), "fixture material index")
        if material_index >= len(materials):
            raise SuiteError("fixture material index is out of bounds")
        material = _object(materials[material_index], "fixture material")
        texture = _object(_object(material.get("pbrMetallicRoughness"), "fixture PBR").get("baseColorTexture"), "fixture diffuse texture")
        texture_index = non_negative_int(texture.get("index"), "fixture texture index")
        if texture_index >= len(textures) or _object(textures[texture_index], "fixture texture").get("source") != 0:
            raise SuiteError("fixture diffuse material must use the embedded image")
    return {"vertex_count": expected_vertices, "extent": extent, "embedded_images": image_count,
            "instance_count": expected_instances, "image_mime_type": mime_type if image_count else None, "size_bytes": len(raw)}


def generate_model_glb(client: GatewayClient, model: ArtifactFixture, tenant_id: int, timeout: float) -> None:
    query = urllib.parse.urlencode({"locator": model.locator})
    capability = _object(client.request("GET", f"/api/v1/manager/quick-view/capability?{query}", (200,)).payload, "initial model capability")
    if "generate_model_3d_glb" not in _array(capability.get("available_actions"), "initial model actions"):
        raise SuiteError(f"{model.format} capability must declare generate_model_3d_glb")
    model.previous_mode = str(capability.get("preferred_mode"))
    if model.previous_mode not in {"basic_preview", "map_quick_view"}:
        raise SuiteError("model capability preferred_mode is missing or invalid")
    started = _object(client.request("POST", "/api/v1/manager/quick-view/actions", (202,), {
        "locator": model.locator, "action": "generate_model_3d_glb",
        **(MAX_OPTIONS if model.format == "max" else {}),
    }).payload, "model GLB generation")
    model.task_id = positive_int(started.get("task_id"), "model task id")
    execution_id = started.get("execution_id")
    if started.get("task_type") != MODEL_TASK_TYPE or not isinstance(execution_id, str) or not execution_id:
        raise SuiteError("model generation must return its task type and execution_id")
    model.execution_id = execution_id
    execution = wait_for_manager_execution(client, execution_id, time.monotonic() + timeout, model.format.upper())
    owner_facts = validate_lineage(
        execution, item_locator=model.locator,
        item_id=positive_int(model.item["id"], "model item id"), fingerprint=str(model.item["fingerprint"]),
        tenant_id=tenant_id, task_type=MODEL_TASK_TYPE,
        output_prefix="model3d-quick-view", output_suffix=".glb", output_label="GLB",
    )
    monitor = _object(client.request("GET", f"/api/v1/monitor/executions/by-execution-id/{urllib.parse.quote(execution_id)}", (200,)).payload, "Monitor model execution")
    validate_monitor_lineage(monitor, owner_facts)
    ready = _object(client.request("GET", f"/api/v1/manager/quick-view/capability?{query}", (200,)).payload, "ready model capability")
    result = _object(ready.get("model_3d"), "ready model GLB")
    model.result_id = positive_int(result.get("result_id"), "model result id")
    expected_url = f"/api/v1/manager/model_3d_glb/{model.result_id}/content"
    if (ready.get("can_use_quick_view") is not True or ready.get("render_source") != "model_3d_glb"
            or result.get("task_id") != model.task_id or result.get("last_execution_id") != model.execution_id
            or result.get("format") != model.format or result.get("preview_url") != expected_url):
        raise SuiteError("ready GLB capability must match this model generation")
    ranged = client.request("GET", expected_url, (206,), headers={"Accept": "model/gltf-binary", "Range": "bytes=0-63"})
    if not ranged.raw.startswith(b"glTF") or len(ranged.raw) != 64:
        raise SuiteError("model GLB Range response must be a 64-byte GLB prefix")
    content = client.request("GET", expected_url, (200,), headers={"Accept": "model/gltf-binary"})
    audit = {"conversion": validate_max_conversion(execution)} if model.format == "max" else {}
    model.artifact = {**audit, **validate_model_glb(content.raw, model.format), "range_bytes": len(ranged.raw), "storage_domain": "addp-infra", "preview_url": expected_url}
    model.mode_changed = True
    client.request("PATCH", "/api/v1/manager/preview-state/preferred-mode", (200,), {"locator": model.locator, "preferred_mode": "map_quick_view"})


def cleanup_managed_artifact(client: GatewayClient, model: ArtifactFixture, timeout: float, resource="model_3d_glb", task_type=MODEL_TASK_TYPE) -> None:
    restore_error: SuiteError | None = None
    if model.mode_changed:
        try:
            client.request("PATCH", "/api/v1/manager/preview-state/preferred-mode", (200,), {"locator": model.locator, "preferred_mode": model.previous_mode})
            query = urllib.parse.urlencode({"locator": model.locator})
            state = _object(client.request("GET", f"/api/v1/manager/preview-state?{query}", (200,)).payload, "restored model preview state")
            if state.get("preferred_mode") != model.previous_mode:
                raise SuiteError("artifact preview mode restoration was not verified")
        except SuiteError as error:
            restore_error = error
    model.cleanup["preview_mode_restored"] = restore_error is None
    if model.task_id is None:
        if restore_error:
            raise restore_error
        return
    if not model.execution_id:
        raise SuiteError(f"{model.format} execution identity is unknown; task retained")
    deadline = time.monotonic() + timeout
    while True:
        execution = _object(client.request("GET", f"/api/v1/manager/executions/{urllib.parse.quote(model.execution_id)}", (200,)).payload, "artifact cleanup execution")
        if execution.get("status") in TERMINAL_STATUSES:
            break
        if time.monotonic() >= deadline:
            raise SuiteError(f"{model.format} execution is still active; resources retained")
        time.sleep(1)
    query = urllib.parse.urlencode({"task_id": model.task_id, "page": 1, "page_size": 100})
    results = _object(client.request("GET", f"/api/v1/manager/{resource}?{query}", (200,)).payload, "artifact cleanup results")
    rows = _array(results.get("data"), "artifact cleanup result rows")
    if non_negative_int(results.get("total"), "artifact cleanup total") != len(rows):
        raise SuiteError("artifact cleanup must inspect all results")
    for value in rows:
        result = _object(value, "artifact cleanup result")
        if result.get("task_id") != model.task_id or result.get("item_fingerprint") != model.item["fingerprint"]:
            raise SuiteError("artifact cleanup result is not owned by this run")
        result_id = positive_int(result.get("id"), "artifact cleanup result id")
        client.request("DELETE", f"/api/v1/manager/{resource}/{result_id}", (200,))
        client.request("GET", f"/api/v1/manager/{resource}/{result_id}/content", (404,))
    model.cleanup["result_deleted"] = True
    model.cleanup["content_unavailable"] = True
    task_path = f"/api/v1/manager/tasks/{task_type}/{model.task_id}"
    client.request("DELETE", task_path, (204,))
    client.request("GET", task_path, (404,))
    model.cleanup["task_deleted"] = True
    remaining = _object(client.request("GET", f"/api/v1/manager/{resource}?{query}", (200,)).payload, "artifact residual results")
    residual = non_negative_int(remaining.get("total"), "artifact residual result total")
    model.cleanup["residual_resources"] = residual
    if residual:
        raise SuiteError(f"{model.format} artifact cleanup has {residual} residual results")
    if restore_error:
        raise restore_error


def raster_physical(repository, environment, action, request):
    artifact = Path(environment['ADDP_ONLINE_ARTIFACT_DIR']) / f'manager-artifact-physical-{action}.json'
    artifact.unlink(missing_ok=True)
    secret_root = Path(environment['ADDP_ONLINE_SECRET_DIR'])
    request_path = secret_root / 'manager-raster-oracle.json'
    request_path.write_text(json.dumps(request))
    request_path.chmod(0o600)
    result = subprocess.run([sys.executable, str(repository / 'business/scripts/online-raster-minio-fixture.py'),
        'manager-' + action, str(request_path)], env=environment, capture_output=True, text=True, check=False)
    if result.returncode:
        raise SuiteError('Manager artifact physical verification failed')
    evidence = _object(json.loads(result.stdout), 'Manager artifact physical evidence')
    artifact.write_text(json.dumps(evidence, ensure_ascii=False, sort_keys=True) + '\n', encoding='utf-8')
    return evidence


def generate_raster_cog(client, raster, tenant_id, timeout, physical):
    query = urllib.parse.urlencode({'locator': raster.locator})
    capability = _object(client.request('GET', f'/api/v1/manager/quick-view/capability?{query}', (200,)).payload, 'initial raster capability')
    if 'generate_raster_cog' not in _array(capability.get('available_actions'), 'raster actions'):
        raise SuiteError('raster capability must declare generate_raster_cog')
    raster.previous_mode = str(capability.get('preferred_mode'))
    if raster.previous_mode not in {'basic_preview', 'map_quick_view'}:
        raise SuiteError('raster preferred mode is invalid')
    started = _object(client.request('POST', '/api/v1/manager/quick-view/actions', (202,), {
        'locator': raster.locator, 'action': 'generate_raster_cog',
    }).payload, 'raster COG start')
    raster.task_id = positive_int(started.get('task_id'), 'raster task id')
    raster.execution_id = str(started.get('execution_id') or '')
    if started.get('task_type') != RASTER_TASK_TYPE or not raster.execution_id:
        raise SuiteError('raster generation identity is invalid')
    execution = wait_for_manager_execution(client, raster.execution_id, time.monotonic() + timeout, 'Raster COG')
    facts = validate_lineage(execution, item_locator=raster.locator, item_id=raster.item['id'],
        fingerprint=raster.item['fingerprint'], tenant_id=tenant_id, task_type=RASTER_TASK_TYPE,
        output_prefix='cog', output_suffix='.cog.tif', output_label='COG')
    monitor = _object(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{raster.execution_id}', (200,)).payload, 'Monitor raster execution')
    validate_monitor_lineage(monitor, facts)
    query_results = urllib.parse.urlencode({'task_id': raster.task_id, 'page': 1, 'page_size': 100})
    results = _object(client.request('GET', f'/api/v1/manager/raster_cog?{query_results}', (200,)).payload, 'raster COG results')
    rows = _array(results.get('data'), 'raster COG result rows')
    if len(rows) != 1 or results.get('total') != 1:
        raise SuiteError('raster generation must produce exactly one COG')
    result = _object(rows[0], 'raster COG result')
    raster.result_id = positive_int(result.get('id'), 'raster result id')
    runtime = _object(_object(result.get('metadata'), 'raster metadata').get('workflow_runtime'), 'raster Runtime audit')
    if (result.get('task_id') != raster.task_id or result.get('last_execution_id') != raster.execution_id
            or result.get('item_fingerprint') != raster.item['fingerprint'] or result.get('status') != 'ready'
            or runtime.get('mode') != 'direct' or runtime.get('operator') != 'raster_to_cog'
            or runtime.get('engine_type') != 'geopython_workflow'
            or not isinstance(runtime.get('execution_id', ''), str)):
        raise SuiteError('Manager COG must preserve its direct Runtime generation identity')
    positive_int(runtime.get('engine_id'), 'raster Runtime engine id')
    ready = _object(client.request('GET', f'/api/v1/manager/quick-view/capability?{query}', (200,)).payload, 'ready raster capability')
    url = f'/api/v1/manager/raster_cog/{raster.result_id}/content'
    raster_facts = _object(ready.get('raster'), 'ready raster facts')
    if (ready.get('can_use_quick_view') is not True or ready.get('render_source') != 'client_cog_render'
            or _object(ready.get('quick_view'), 'ready raster render info').get('preview_url') != url
            or raster_facts.get('profile') != 'cog' or raster_facts.get('client_read_mode') != 'range'
            or any(raster_facts.get(k) != v or result.get(k) != v for k,v in {
                'width': 256, 'height': 256, 'band_count': 2, 'source_srid': 4326}.items())):
        raise SuiteError('ready raster capability must match the persisted COG facts')
    for value in (result, raster_facts, _object(ready['quick_view'], 'raster render facts')):
        extent = _array(value.get('extent'), 'raster extent')
        if (value.get('extent_srid') != 4326 or len(extent) != 4
                or any(not isinstance(actual, (float, int)) or isinstance(actual, bool)
                       or not math.isclose(actual, expected, rel_tol=0, abs_tol=1e-9)
                       for actual, expected in zip(extent, [110., 17.76, 112.56, 20.32]))):
            raise SuiteError('Manager COG persisted and rendering extents differ from its grid')
    size = positive_int(result.get('size_bytes'), 'raster COG size')
    full = client.request('GET', url, (200,), headers={'Accept': 'image/tiff'})
    full_headers = {k.lower():v for k,v in full.headers.items()}
    if size != len(full.raw) or raster_facts.get('size_bytes') != size or full_headers.get('content-length') != str(size):
        raise SuiteError('Manager COG result, capability and HTTP sizes differ')
    for header in ('bytes=0-63', 'bytes=-64'):
        ranged = client.request('GET', url, (206,), headers={'Accept': 'image/tiff', 'Range': header})
        h = {k.lower():v for k,v in ranged.headers.items()}
        start = 0 if header == 'bytes=0-63' else size - 64
        if (ranged.raw != full.raw[start:start+64] or h.get('content-range') != f'bytes {start}-{start+63}/{size}'
                or h.get('content-length') != '64' or h.get('accept-ranges') != 'bytes'):
            raise SuiteError('Manager COG Range bytes or headers differ from the complete object')
    client.request('GET', url, (416,), headers={'Range': f'bytes={size}-'})
    raster.artifact = {'storage_domain': 'addp-infra', 'range_bytes': 64, 'preview_url': url, 'size_bytes': size, 'sha256': hashlib.sha256(full.raw).hexdigest(),
        'runtime_engine_id': runtime['engine_id'], 'runtime_operator': runtime['operator'], 'mode': 'direct',
        'physical_request': {'locator': facts['outputs'][0]['locator'], 'tenant_id': tenant_id,
            'fingerprint': raster.item['fingerprint'], 'size_bytes': size, 'sha256': hashlib.sha256(full.raw).hexdigest()}}
    if runtime.get('execution_id'):
        raster.artifact['runtime_execution_id'] = runtime['execution_id']
    evidence = physical('verify', raster.artifact['physical_request'])
    if (evidence.get('cog_valid') is not True or evidence.get('source_unchanged') is not True
            or evidence.get('pixels_verified') != 131072 or evidence.get('size_bytes') != size
            or evidence.get('sha256') != raster.artifact['sha256']):
        raise SuiteError('Manager COG physical evidence is incomplete')
    raster.artifact['physical'] = evidence
    client.request('PATCH', '/api/v1/manager/preview-state/preferred-mode', (200,), {'locator': raster.locator, 'preferred_mode': 'map_quick_view'})
    raster.mode_changed = True


def validate_browser_report(
    report: object,
    *,
    run_id: str,
    execution_id: str,
    item_id: int,
    output_name: str,
    pptx_item_id: int,
    pptx_page_count: int,
    models: list[dict[str, object]],
    raster: dict[str, object],
    phase: str = "cached-preview",
) -> dict[str, object]:
    payload = _object(report, "Manager lineage browser report")
    if phase not in {"generation-entry", "cached-preview"}:
        raise SuiteError("unknown Manager browser phase")
    expected = {
        "schema_version": "addp.manager-internal-artifact-lineage-browser/v4",
        "phase": phase,
        "suite": "manager-internal-artifact-lineage",
        "run_id": run_id,
        "result": "passed",
        "execution_id": execution_id,
        "item_id": item_id,
        "output_name": output_name,
        "input_resources": 1,
        "output_resources": 1,
        "platform_internal_outputs": 1,
        "pptx_item_id": pptx_item_id,
        "pptx_page_count": pptx_page_count,
        "pptx_page_after_engine_refresh": 2,
        "pptx_generation_requests": 0,
        "model_generation_requests": 0,
        "raster_generation_requests": 0,
        "raster": {**raster, "range_loaded": True, "map_loaded": True},
        "models": [{**model, "model_loaded": True, "content_loaded": True} for model in models],
        "browser_warning_errors": 0,
        "failed_business_responses": 0,
    }
    if phase == "generation-entry":
        expected = {key: expected[key] for key in (
            "schema_version", "phase", "suite", "run_id", "result",
            "model_generation_requests", "browser_warning_errors", "failed_business_responses",
        )}
        expected["models"] = [{**model, "generation_entry_visible": True, **({"conversion_options_verified": True} if model["format"] == "max" else {})} for model in models]
    mismatches = [key for key, value in expected.items() if payload.get(key) != value]
    if mismatches:
        raise SuiteError("Manager lineage browser report contract mismatch: " + ", ".join(mismatches))
    non_negative_int(payload.get("gpu_performance_warnings"), "browser GPU performance warning count")
    if non_negative_int(payload.get("anonymous_refresh_401"), "browser anonymous refresh count") > 1:
        raise SuiteError("Manager lineage browser has repeated anonymous refresh failures")
    return payload


def run_browser(
    repository: Path,
    environment: dict[str, str],
    *,
    execution_id: str,
    item_id: int,
    source_name: str,
    output_name: str,
    pptx_item_locator: str,
    pptx_item_id: int,
    pptx_page_count: int,
    models: list[dict[str, object]],
    raster: dict[str, object],
    phase: str = "cached-preview",
) -> dict[str, object]:
    artifact_dir = Path(environment["ADDP_ONLINE_ARTIFACT_DIR"])
    report_path = artifact_dir / f"manager-internal-artifact-lineage-browser-{phase}.json"
    report_path.unlink(missing_ok=True)
    browser_environment = dict(environment)
    browser_environment.update(
        {
            "ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID": execution_id,
            "ADDP_ONLINE_MANAGER_LINEAGE_ITEM_ID": str(item_id),
            "ADDP_ONLINE_MANAGER_LINEAGE_SOURCE_NAME": source_name,
            "ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME": output_name,
            "ADDP_ONLINE_MANAGER_PPTX_ITEM_LOCATOR": pptx_item_locator,
            "ADDP_ONLINE_MANAGER_PPTX_ITEM_ID": str(pptx_item_id),
            "ADDP_ONLINE_MANAGER_PPTX_PAGE_COUNT": str(pptx_page_count),
            "ADDP_ONLINE_MANAGER_MODELS_JSON": json.dumps(models),
            "ADDP_ONLINE_MANAGER_BROWSER_PHASE": phase,
            "ADDP_ONLINE_MANAGER_RASTER_JSON": json.dumps(raster),
        }
    )
    result = subprocess.run(
        [
            "npm",
            "exec",
            "--",
            "playwright",
            "test",
            "e2e/online/manager-internal-artifact-lineage.spec.js",
            "--config=playwright.online.config.js",
        ],
        cwd=repository / "console/frontend",
        env=browser_environment,
        check=False,
    )
    if result.returncode != 0:
        raise SuiteError(f"Manager lineage browser acceptance exited with status {result.returncode}")
    if not report_path.is_file():
        raise SuiteError("Playwright did not write the Manager browser phase report")
    return validate_browser_report(
        json.loads(report_path.read_text(encoding="utf-8")),
        run_id=environment["ADDP_ONLINE_TEST_RUN_ID"],
        execution_id=execution_id,
        item_id=item_id,
        output_name=output_name,
        pptx_item_id=pptx_item_id,
        pptx_page_count=pptx_page_count,
        models=models,
        raster=raster,
        phase=phase,
    )


def run_scenario(
    repository: Path,
    environment: dict[str, str],
    browser_runner: Callable[..., dict[str, object]] | None = None,
    physical_runner: Callable[..., dict[str, object]] | None = None,
) -> dict[str, object]:
    tenant_id = positive_int(environment["ADDP_ONLINE_TEST_TENANT_ID"], "ADDP_ONLINE_TEST_TENANT_ID")
    engine_id = positive_int(environment["ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID"], "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID")
    timeout = float(environment.get("ADDP_ONLINE_MANAGER_LINEAGE_CONVERGENCE_TIMEOUT_SECONDS", "180"))
    if timeout <= 0:
        raise SuiteError("ADDP_ONLINE_MANAGER_LINEAGE_CONVERGENCE_TIMEOUT_SECONDS must be positive")
    client = GatewayClient(environment["GATEWAY_URL"], environment["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], min(timeout, 30))
    identity = validate_user_identity(client, tenant_id)
    bucket = environment["ADDP_ONLINE_MANAGER_MINIO_BUCKET"].strip("/")
    pointcloud_full_name = f"{bucket}/{environment['ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT'].strip('/')}"
    pptx_full_name = f"{bucket}/{environment['ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT'].strip('/')}"
    deadline = time.monotonic() + timeout
    scan_execution_id = wait_for_meta_scan(client, engine_id, deadline)
    pointcloud_item = find_fixture_item(client, engine_id, pointcloud_full_name, "point-cloud")
    pptx_item = find_fixture_item(client, engine_id, pptx_full_name, "PPTX")
    model_fixtures = [prepare_model_fixture(client, engine_id, bucket, format_name) for format_name in ("dae", "3ds", "ifc", "osgb", "skp", "max")]
    raster_item = find_fixture_item(client, engine_id, f"{bucket}/{environment['ADDP_ONLINE_MANAGER_MINIO_RASTER_OBJECT']}", "raster")
    raster = ArtifactFixture("raster", raster_item, build_item_locator(engine_id, raster_item))
    physical = physical_runner or (lambda action, request: raster_physical(repository, environment, action, request))
    pointcloud_item_id = positive_int(pointcloud_item.get("id"), "point-cloud fixture id")
    pointcloud_fingerprint = str(pointcloud_item["fingerprint"])
    positive_int(pointcloud_item.get("size_bytes"), "point-cloud fixture size_bytes")
    pointcloud_locator = build_item_locator(engine_id, pointcloud_item)
    pptx_item_id = positive_int(pptx_item.get("id"), "PPTX fixture id")
    pptx_fingerprint = str(pptx_item["fingerprint"])
    pptx_locator = build_item_locator(engine_id, pptx_item)
    assert_no_existing_resources(client, pointcloud_fingerprint)

    pointcloud_task_id: int | None = None
    pointcloud_result_id: int | None = None
    pointcloud_execution_id = ""
    pptx_task_id: int | None = None
    pptx_result_id: int | None = None
    pptx_execution_id = ""
    pptx_physical_request = None
    pptx_physical_evidence = {}
    pointcloud_cleanup = {"result_deleted": False, "task_deleted": False, "content_unavailable": False, "residual_resources": -1}
    pptx_cleanup = {"result_deleted": False, "task_deleted": False, "content_unavailable": False, "residual_resources": -1}
    try:
        started = _object(
            client.request(
                "POST", "/api/v1/manager/quick-view/actions", (202,),
                {"locator": pointcloud_locator, "action": "generate_point_cloud_copc"},
            ).payload,
            "PointCloud generation start",
        )
        if started.get("task_type") != POINTCLOUD_TASK_TYPE:
            raise SuiteError("PointCloud generation returned an unexpected task type")
        pointcloud_task_id = positive_int(started.get("task_id"), "PointCloud task id")
        execution_id_value = started.get("execution_id")
        if not isinstance(execution_id_value, str) or not execution_id_value:
            raise SuiteError("PointCloud execution_id is missing")
        pointcloud_execution_id = execution_id_value
        manager_execution = wait_for_manager_execution(client, pointcloud_execution_id, deadline, "PointCloud")
        pointcloud_manager_facts = validate_lineage(
            manager_execution,
            item_locator=pointcloud_locator,
            item_id=pointcloud_item_id,
            fingerprint=pointcloud_fingerprint,
            tenant_id=tenant_id,
            task_type=POINTCLOUD_TASK_TYPE,
            output_prefix="point-cloud-copc",
            output_suffix=".copc.laz",
            output_label="COPC",
        )

        monitor_execution = _object(
            client.request(
                "GET",
                f"/api/v1/monitor/executions/by-execution-id/{urllib.parse.quote(pointcloud_execution_id)}",
                (200,),
            ).payload,
            "Monitor execution",
        )
        validate_monitor_lineage(monitor_execution, pointcloud_manager_facts)

        query = urllib.parse.urlencode({"task_id": pointcloud_task_id, "page": 1, "page_size": 20})
        results = _object(
            client.request("GET", f"/api/v1/manager/point_cloud_copc?{query}", (200,)).payload,
            "PointCloud result list",
        )
        result_rows = _array(results.get("data"), "PointCloud result list data")
        if len(result_rows) != 1:
            raise SuiteError("PointCloud execution must create exactly one result")
        result = _object(result_rows[0], "PointCloud result")
        pointcloud_result_id = positive_int(result.get("id"), "PointCloud result id")
        pointcloud_output_name = result.get("file_name")
        if not isinstance(pointcloud_output_name, str) or not pointcloud_output_name.endswith(".copc.laz"):
            raise SuiteError("PointCloud result file_name must end with .copc.laz")
        pointcloud_content = client.request(
            "GET",
            f"/api/v1/manager/point_cloud_copc/{pointcloud_result_id}/content",
            (206,),
            headers={"Accept": "application/octet-stream", "Range": "bytes=0-63"},
        )
        if not pointcloud_content.raw or len(pointcloud_content.raw) > 64:
            raise SuiteError("PointCloud COPC Range response is empty or unbounded")

        initial_capability = _object(client.request(
            "GET",
            f"/api/v1/manager/quick-view/capability?{urllib.parse.urlencode({'locator': pptx_locator})}",
            (200,),
        ).payload, "initial PPTX capability")
        if "generate_pptx_pdf" not in _array(initial_capability.get("available_actions"), "initial PPTX actions"):
            raise SuiteError("initial PPTX capability must declare generate_pptx_pdf")
        first_preview = client.request(
            "POST",
            "/api/v1/manager/quick-view/actions",
            (202,),
            {"locator": pptx_locator, "action": "generate_pptx_pdf"},
        )
        first_preview_payload = _object(first_preview.payload, "initial PPTX preview")
        if first_preview_payload.get("status") not in {"pending", "running"}:
            raise SuiteError("initial PPTX preview must start a managed conversion")
        pptx_task_id = positive_int(first_preview_payload.get("task_id"), "PPTX task id")
        pptx_execution_id_value = first_preview_payload.get("execution_id")
        if not isinstance(pptx_execution_id_value, str) or not pptx_execution_id_value:
            raise SuiteError("initial PPTX preview execution_id is missing")
        pptx_execution_id = pptx_execution_id_value
        pptx_manager_execution = wait_for_manager_execution(client, pptx_execution_id, deadline, "PPTX PDF")
        pptx_manager_facts = validate_lineage(
            pptx_manager_execution,
            item_locator=pptx_locator,
            item_id=pptx_item_id,
            fingerprint=pptx_fingerprint,
            tenant_id=tenant_id,
            task_type=PPTX_TASK_TYPE,
            output_prefix="document-preview",
            output_suffix=".pdf",
            output_label="PDF",
        )
        pptx_monitor_execution = _object(
            client.request(
                "GET",
                f"/api/v1/monitor/executions/by-execution-id/{urllib.parse.quote(pptx_execution_id)}",
                (200,),
            ).payload,
            "Monitor PPTX execution",
        )
        validate_monitor_lineage(pptx_monitor_execution, pptx_manager_facts)

        cached_capability = _object(
            client.request(
                "GET",
                f"/api/v1/manager/quick-view/capability?{urllib.parse.urlencode({'locator': pptx_locator})}",
                (200,),
            ).payload,
            "cached PPTX capability",
        )
        cached_preview = _object(cached_capability.get("pptx_pdf"), "cached PPTX capability result")
        if cached_preview.get("status") != "ready":
            raise SuiteError("second PPTX capability request must reuse the ready artifact")
        if positive_int(cached_preview.get("task_id"), "cached PPTX task id") != pptx_task_id:
            raise SuiteError("cached PPTX capability must reuse the initial task")
        pptx_result_id = positive_int(cached_preview.get("result_id"), "cached PPTX result id")
        pptx_page_count = positive_int(cached_preview.get("page_count"), "cached PPTX page_count")
        if pptx_page_count != 3:
            raise SuiteError(f"PPTX fixture must convert to exactly 3 pages, got {pptx_page_count}")
        pptx_size_bytes = positive_int(cached_preview.get("size_bytes"), "cached PPTX size_bytes")
        expected_preview_url = f"/api/v1/manager/pptx_pdf/{pptx_result_id}/content"
        if cached_preview.get("preview_url") != expected_preview_url:
            raise SuiteError("cached PPTX preview_url must use the managed content API")
        pptx_content = client.request(
            "GET",
            expected_preview_url,
            (206,),
            headers={"Accept": "application/pdf", "Range": "bytes=0-63"},
        )
        if not pptx_content.raw.startswith(b"%PDF") or len(pptx_content.raw) > 64:
            raise SuiteError("PPTX PDF Range response is not a bounded PDF prefix")

        pptx_physical_request = {'locator': pptx_manager_facts['outputs'][0]['locator'],
            'tenant_id': tenant_id, 'fingerprint': pptx_fingerprint, 'size_bytes': pptx_size_bytes}
        pptx_physical_evidence = physical('pdf-verify', pptx_physical_request)
        if (pptx_physical_evidence.get('object_present') is not True
                or pptx_physical_evidence.get('source_unchanged') is not True
                or pptx_physical_evidence.get('size_bytes') != pptx_size_bytes):
            raise SuiteError('Manager PDF physical evidence is incomplete')

        generate_raster_cog(client, raster, tenant_id, timeout, physical)
        browser_arguments = dict(
            execution_id=pointcloud_execution_id, item_id=pointcloud_item_id,
            source_name=Path(environment["ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT"]).name,
            output_name=pointcloud_output_name, pptx_item_locator=pptx_locator,
            pptx_item_id=pptx_item_id, pptx_page_count=pptx_page_count,
            raster={"locator": raster.locator, "item_id": raster.item["id"], "preview_url": raster.artifact["preview_url"]},
        )
        browser_models = [{"format": model.format, "locator": model.locator,
                           "item_id": model.item["id"], **({"conversion_options": MAX_OPTIONS} if model.format == "max" else {})} for model in model_fixtures]
        entry_evidence: dict[str, object] = {}
        if browser_runner is not None:
            entry_evidence = browser_runner(repository, environment, **browser_arguments,
                                           models=browser_models, phase="generation-entry")
            validate_browser_report(entry_evidence, run_id=environment["ADDP_ONLINE_TEST_RUN_ID"],
                                    **{key: value for key, value in browser_arguments.items()
                                       if key not in {"source_name", "pptx_item_locator"}},
                                    models=browser_models, phase="generation-entry")
        for model in model_fixtures:
            generate_model_glb(client, model, tenant_id, timeout)
        browser_models = [{
            "format": model.format, "locator": model.locator,
            "item_id": model.item["id"], "result_id": model.result_id,
            "preview_url": model.artifact["preview_url"],
            **({"conversion_options": MAX_OPTIONS} if model.format == "max" else {}),
        } for model in model_fixtures]
        browser_evidence: dict[str, object] = {}
        if browser_runner is not None:
            browser_evidence = browser_runner(repository, environment, **browser_arguments,
                                             models=browser_models, phase="cached-preview")
        browser_evidence["generation_entry"] = entry_evidence
        return {
            "schema_version": "addp.manager-internal-artifact-lineage/v3",
            "suite": "manager-internal-artifact-lineage",
            "scenario": "business-object-to-manager-infra-lineage",
            "run_id": environment["ADDP_ONLINE_TEST_RUN_ID"],
            "result": "passed",
            "tenant_id": str(tenant_id),
            "identity": identity,
            "engine_id": engine_id,
            "meta_scan_execution_id": scan_execution_id,
            "sources": {
                "point_cloud": {"item_id": pointcloud_item_id, "fingerprint": pointcloud_fingerprint, "locator": pointcloud_locator},
                "pptx": {"item_id": pptx_item_id, "fingerprint": pptx_fingerprint, "locator": pptx_locator},
                "raster": {"item_id": raster.item["id"], "fingerprint": raster.item["fingerprint"], "locator": raster.locator},
                **{model.format: {"item_id": model.item["id"], "fingerprint": model.item["fingerprint"], "locator": model.locator} for model in model_fixtures},
            },
            "created": {
                "point_cloud": {"task_id": pointcloud_task_id, "execution_id": pointcloud_execution_id, "result_id": pointcloud_result_id},
                "pptx_pdf": {"task_id": pptx_task_id, "execution_id": pptx_execution_id, "result_id": pptx_result_id},
                **{model.format: {"task_id": model.task_id, "execution_id": model.execution_id, "result_id": model.result_id} for model in model_fixtures},
            },
            "lineage": {"schema_version": LINEAGE_SCHEMA, "inputs": 3 + len(model_fixtures), "outputs": 3 + len(model_fixtures), "manager_monitor_equal": True},
            "artifacts": {
                "point_cloud": {"name": pointcloud_output_name, "range_bytes": len(pointcloud_content.raw), "storage_domain": "addp-infra"},
                "pptx_pdf": {"page_count": pptx_page_count, "size_bytes": pptx_size_bytes, "range_bytes": len(pptx_content.raw), "storage_domain": "addp-infra", "cache_reused": True, "physical": pptx_physical_evidence},
                **{model.format: model.artifact for model in model_fixtures},
            },
            "raster_cog": {"task_id": raster.task_id, "execution_id": raster.execution_id, "result_id": raster.result_id,
                **{k:v for k,v in raster.artifact.items() if k != "physical_request"}},
            "browser": browser_evidence,
            "cleanup": {"raster_cog": raster.cleanup, "point_cloud": pointcloud_cleanup, "pptx_pdf": pptx_cleanup, **{model.format: model.cleanup for model in model_fixtures}},
        }
    finally:
        scenario_error = sys.exception()
        cleanup_errors: list[str] = []
        try:
            # Recover owned output locators even when generation validation failed.
            request = raster.artifact.get('physical_request')
            if request is None and raster.execution_id:
                execution = _object(client.request('GET', f'/api/v1/manager/executions/{raster.execution_id}', (200,)).payload, 'raster cleanup execution')
                if execution.get('status') == 'success':
                    facts = validate_lineage(execution, item_locator=raster.locator, item_id=raster.item['id'], fingerprint=raster.item['fingerprint'],
                        tenant_id=tenant_id, task_type=RASTER_TASK_TYPE, output_prefix='cog', output_suffix='.cog.tif', output_label='COG')
                    request = {'locator': facts['outputs'][0]['locator'], 'tenant_id': tenant_id, 'fingerprint': raster.item['fingerprint']}
            cleanup_managed_artifact(client, raster, timeout, 'raster_cog', RASTER_TASK_TYPE)
            if request is not None:
                deleted = physical('deleted', request)
                if deleted != {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}:
                    raise SuiteError('Manager COG physical deletion was not verified')
                raster.cleanup.update(deleted)
        except SuiteError as error:
            cleanup_errors.append(str(error))
        for model in reversed(model_fixtures):
            try:
                cleanup_managed_artifact(client, model, timeout)
            except SuiteError as error:
                cleanup_errors.append(str(error))
        if pptx_result_id is not None:
            try:
                client.request("DELETE", f"/api/v1/manager/pptx_pdf/{pptx_result_id}", (200,))
                pptx_cleanup["result_deleted"] = True
                client.request("GET", f"/api/v1/manager/pptx_pdf/{pptx_result_id}/content", (404,))
                pptx_cleanup["content_unavailable"] = True
                if pptx_physical_request is None:
                    raise SuiteError('Manager PDF cleanup has no owned physical reference')
                deleted = physical('pdf-deleted', pptx_physical_request)
                if deleted != {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}:
                    raise SuiteError('Manager PDF physical deletion was not verified')
                pptx_cleanup.update(deleted)
                capability = _object(client.request('GET', '/api/v1/manager/quick-view/capability?' +
                    urllib.parse.urlencode({'locator': pptx_locator}), (200,)).payload, 'deleted PPTX capability')
                if _object(capability.get('pptx_pdf'), 'deleted PPTX result').get('result_id'):
                    raise SuiteError('deleted PPTX result is still active')
                pptx_cleanup['result_unavailable'] = True
            except SuiteError as error:
                cleanup_errors.append(str(error))
        if pptx_task_id is not None:
            try:
                client.request("DELETE", f"/api/v1/manager/tasks/{PPTX_TASK_TYPE}/{pptx_task_id}", (204,))
                client.request("GET", f"/api/v1/manager/tasks/{PPTX_TASK_TYPE}/{pptx_task_id}", (404,))
                pptx_cleanup["task_deleted"] = True
                preserved_item = find_fixture_item(client, engine_id, pptx_full_name, 'preserved PPTX')
                if preserved_item['id'] != pptx_item_id or preserved_item['fingerprint'] != pptx_fingerprint:
                    raise SuiteError('PPTX cleanup changed the source DataItem')
                execution = _object(client.request('GET', '/api/v1/manager/executions/' +
                    urllib.parse.quote(pptx_execution_id), (200,)).payload, 'preserved PPTX execution')
                facts = validate_lineage(execution, item_locator=pptx_locator, item_id=pptx_item_id,
                    fingerprint=pptx_fingerprint, tenant_id=tenant_id, task_type=PPTX_TASK_TYPE,
                    output_prefix='document-preview', output_suffix='.pdf', output_label='PDF')
                monitor = _object(client.request('GET', '/api/v1/monitor/executions/by-execution-id/' +
                    urllib.parse.quote(pptx_execution_id), (200,)).payload, 'preserved Monitor PPTX execution')
                validate_monitor_lineage(monitor, facts)
                pptx_cleanup.update(source_item_preserved=True, execution_preserved=True)
            except SuiteError as error:
                cleanup_errors.append(str(error))
        if pptx_task_id is not None or pptx_result_id is not None:
            pptx_cleanup["residual_resources"] = int(not pptx_cleanup["result_deleted"]) + int(not pptx_cleanup["task_deleted"])
            if pptx_cleanup["residual_resources"] != 0:
                cleanup_errors.append(f"PPTX residual resource count is {pptx_cleanup['residual_resources']}")

        if pointcloud_result_id is not None:
            try:
                client.request("DELETE", f"/api/v1/manager/point_cloud_copc/{pointcloud_result_id}", (200,))
                pointcloud_cleanup["result_deleted"] = True
                client.request("GET", f"/api/v1/manager/point_cloud_copc/{pointcloud_result_id}/content", (404,))
                pointcloud_cleanup["content_unavailable"] = True
            except SuiteError as error:
                cleanup_errors.append(str(error))
        if pointcloud_task_id is not None:
            try:
                client.request("DELETE", f"/api/v1/manager/tasks/{POINTCLOUD_TASK_TYPE}/{pointcloud_task_id}", (204,))
                client.request("GET", f"/api/v1/manager/tasks/{POINTCLOUD_TASK_TYPE}/{pointcloud_task_id}", (404,))
                pointcloud_cleanup["task_deleted"] = True
            except SuiteError as error:
                cleanup_errors.append(str(error))
        if pointcloud_task_id is not None or pointcloud_result_id is not None:
            try:
                query = urllib.parse.urlencode({"item_fingerprint": pointcloud_fingerprint, "page": 1, "page_size": 100})
                results = _object(
                    client.request("GET", f"/api/v1/manager/point_cloud_copc?{query}", (200,)).payload,
                    "PointCloud residual result list",
                )
                residual_results = non_negative_int(
                    results.get("total"), "PointCloud residual result total"
                )
                residual_tasks = 0 if pointcloud_cleanup["task_deleted"] else 1
                pointcloud_cleanup["residual_resources"] = residual_results + residual_tasks
                if residual_results != 0:
                    cleanup_errors.append(
                        f"PointCloud residual result count is {residual_results}"
                    )
                if residual_tasks != 0:
                    cleanup_errors.append("PointCloud task deletion was not verified")
            except SuiteError as error:
                cleanup_errors.append(str(error))
        if cleanup_errors:
            message = "Manager lineage cleanup failed: " + "; ".join(cleanup_errors)
            if isinstance(scenario_error, SuiteError):
                message = f"{scenario_error}; {message}"
            raise SuiteError(message) from scenario_error


def required_environment() -> dict[str, str]:
    names = (
        "ADDP_ONLINE_ARTIFACT_DIR",
        "ADDP_ONLINE_TEST_RUN_ID",
        "ADDP_ONLINE_TEST_TENANT_ID",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN",
        "ADDP_ONLINE_TEST_USER_USERNAME",
        "ADDP_ONLINE_TEST_USER_PASSWORD",
        "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID",
        "ADDP_ONLINE_MANAGER_MINIO_BUCKET",
        "ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT",
        "ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT",
        "ADDP_ONLINE_MANAGER_MINIO_RASTER_OBJECT",
        "ADDP_ONLINE_SECRET_DIR",
        "ADDP_ONLINE_RASTER_RUNTIME_IMAGE",
        "CONSOLE_URL",
        "GATEWAY_URL",
    )
    missing = [name for name in names if not os.environ.get(name)]
    if missing:
        raise SuiteError("missing Online environment: " + ", ".join(missing))
    return dict(os.environ)


def main() -> int:
    try:
        environment = required_environment()
        repository = Path(__file__).resolve().parents[2]
        report = run_scenario(repository, environment, run_browser)
    except (OSError, ValueError, SuiteError, subprocess.SubprocessError) as error:
        print(f"Manager internal artifact lineage Online acceptance failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

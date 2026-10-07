import copy
import importlib.util
import json
import struct
import sys
import tempfile
import unittest
import urllib.parse
import zlib
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("manager-internal-artifact-lineage-online.py")
SPEC = importlib.util.spec_from_file_location("manager_internal_artifact_lineage_online", SCRIPT)
SUITE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = SUITE
SPEC.loader.exec_module(SUITE)


def response(status, payload=None, *, raw=b""):
    return SUITE.Response(status, payload if payload is not None else {}, {}, raw)


def textured_glb():
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
    png = (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">2I5B", 1, 1, 8, 2, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(b"\x00\xff\x40\x20")) + chunk(b"IEND", b""))
    binary = struct.pack("<9f", 0, 0, 0, 1, 0, 0, 0, 2, 0) + struct.pack("<6f", 0, 0, 1, 0, 0, 1) + png
    doc = {
        "asset": {"version": "2.0"}, "scene": 0, "scenes": [{"nodes": [0]}], "nodes": [{"mesh": 0}],
        "buffers": [{"byteLength": len(binary)}],
        "bufferViews": [{"buffer": 0, "byteOffset": 0, "byteLength": 36}, {"buffer": 0, "byteOffset": 36, "byteLength": 24}, {"buffer": 0, "byteOffset": 60, "byteLength": len(png)}],
        "accessors": [{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3", "min": [0, 0, 0], "max": [1, 2, 0]}, {"bufferView": 1, "componentType": 5126, "count": 3, "type": "VEC2"}],
        "meshes": [{"primitives": [{"attributes": {"POSITION": 0, "TEXCOORD_0": 1}, "material": 0}]}],
        "images": [{"bufferView": 2, "mimeType": "image/png"}], "textures": [{"source": 0}],
        "materials": [{"pbrMetallicRoughness": {"baseColorTexture": {"index": 0}}}],
    }
    encoded = json.dumps(doc).encode()
    encoded += b" " * (-len(encoded) % 4)
    binary += b"\0" * (-len(binary) % 4)
    return (struct.pack("<4sII", b"glTF", 2, 28 + len(encoded) + len(binary))
            + struct.pack("<I4s", len(encoded), b"JSON") + encoded
            + struct.pack("<I4s", len(binary), b"BIN\0") + binary)


def alter_glb(raw, change):
    size = struct.unpack_from("<I", raw, 12)[0]
    doc = json.loads(raw[20:20 + size])
    change(doc)
    encoded = json.dumps(doc).encode()
    encoded += b" " * (-len(encoded) % 4)
    tail = raw[20 + size:]
    return struct.pack("<4sII", b"glTF", 2, 20 + len(encoded) + len(tail)) + struct.pack("<I4s", len(encoded), b"JSON") + encoded + tail


class FakeGatewayClient:
    def __init__(self) -> None:
        self.pointcloud_task_exists = False
        self.pointcloud_result_exists = False
        self.pptx_task_exists = False
        self.pptx_result_exists = False
        self.pptx_capability_calls = 0
        self.calls: list[tuple[str, str]] = []
        self.pointcloud_item_id = 91
        self.pptx_item_id = 92
        self.pointcloud_fingerprint = "a" * 64
        self.pptx_fingerprint = "b" * 64
        self.pointcloud_locator = (
            "addp://engine/27/path/addp-online/pointcloud/"
            "pdal_las12_format0.las?type=object&item_id=91"
        )
        self.pptx_locator = (
            "addp://engine/27/path/addp-online/document/"
            "addp_online_preview_fixture.pptx?type=object&item_id=92"
        )
        self.pointcloud_output_name = "pdal_las12_format0.copc.laz"
        self.models = {
            format_name: {"item_id": item_id, "task_id": item_id + 110, "result_id": item_id + 210,
                          "fingerprint": fingerprint * 64, "task_exists": False, "result_exists": False,
                          "locator": f"addp://engine/27/path/addp-online/model3d/{format_name}/model.{format_name}?type=object&item_id={item_id}"}
            for format_name, item_id, fingerprint in (("dae", 93, "c"), ("3ds", 94, "d"))
        }
        self.raster = {'id': 305, 'item_id': 95, 'task_id': 205, 'result_id': 305, 'fingerprint': 'e'*64,
            'item_fingerprint': 'e'*64, 'task_exists': False, 'result_exists': False,
            'locator': 'addp://engine/27/path/addp-online/raster/source.tif?type=object&item_id=95'}
        self.cog = b'II*\0' + bytes(range(256))
        self.raster_mode = 'basic_preview'
        self.glb = textured_glb()
        self.model_modes = {"dae": "basic_preview", "3ds": "basic_preview"}

    def lineage_execution(self, task_type, format_name=None):
        if task_type == SUITE.RASTER_TASK_TYPE:
            model = self.raster
            item_id = model['item_id']; fingerprint = model['fingerprint']; locator = model['locator']
            output = f'addp-infra://minio/manager/tenant_42/cog/{fingerprint}/source.cog.tif?type=object'
        elif task_type == SUITE.MODEL_TASK_TYPE:
            model = self.models[format_name]
            item_id = model["item_id"]
            fingerprint = model["fingerprint"]
            locator = model["locator"]
            output = f"addp-infra://minio/manager/tenant_42/model3d-quick-view/{fingerprint}/model.glb?type=object"
        elif task_type == SUITE.PPTX_TASK_TYPE:
            item_id = self.pptx_item_id
            fingerprint = self.pptx_fingerprint
            locator = self.pptx_locator
            output = (
                "addp-infra://minio/manager/tenant_42/document-preview/92/"
                "addp_online_preview_fixture.pdf?type=object"
            )
        else:
            item_id = self.pointcloud_item_id
            fingerprint = self.pointcloud_fingerprint
            locator = self.pointcloud_locator
            output = (
                "addp-infra://minio/manager/tenant_42/point-cloud-copc/91/"
                "pdal_las12_format0.copc.laz?type=object"
            )
        return {
            "status": "success",
            "metadata": {
                "lineage_facts": {
                    "schema_version": SUITE.LINEAGE_SCHEMA,
                    "inputs": [{
                        "port": "source",
                        "locator": locator,
                        "item_id": item_id,
                        "item_fingerprint": fingerprint,
                    }],
                    "outputs": [{"port": "result", "locator": output}],
                    "operations": [{
                        "kind": "derive",
                        "operator": task_type,
                        "input_ports": ["source"],
                        "output_ports": ["result"],
                    }],
                }
            },
        }

    def request(self, method, path, expected, body=None, headers=None):
        self.calls.append((method, path))
        result = self._request(method, path, body, headers)
        if result.status not in expected:
            raise AssertionError(
                f"fake response {result.status} is not expected for {method} {path}"
            )
        return result

    def monitor_execution(self, task_type, format_name=None):
        execution = self.lineage_execution(task_type, format_name)
        execution["metadata"]["lineage_facts"]["operations"] = None
        return execution

    def _request(self, method, path, body, headers):
        raster = self.raster
        parsed_raster = urllib.parse.urlsplit(path)
        raster_query = urllib.parse.parse_qs(parsed_raster.query)
        if path == '/api/v1/manager/raster_cog?' + urllib.parse.urlencode({'task_id': raster['task_id'], 'page': 1, 'page_size': 100}):
            rows = [{**raster, 'status': 'ready', 'last_execution_id': 'raster-direct', 'size_bytes': len(self.cog),
                'width': 256, 'height': 256, 'band_count': 2, 'source_srid': 4326,
                'extent': [110., 17.76, 112.56, 20.32], 'extent_srid': 4326,
                'metadata': {'workflow_runtime': {'engine_type': 'geopython_workflow', 'engine_id': 77, 'mode': 'direct', 'operator': 'raster_to_cog', 'execution_id': ''}}}] if raster['result_exists'] else []
            return response(200, {'data': rows, 'total': len(rows)})
        if parsed_raster.path == '/api/v1/manager/quick-view/capability' and raster_query.get('locator') == [raster['locator']]:
            return response(200, {'available_actions': ['generate_raster_cog'], 'preferred_mode': self.raster_mode,
                'can_use_quick_view': raster['result_exists'], 'render_source': 'client_cog_render',
                'quick_view': {'preview_url': '/api/v1/manager/raster_cog/305/content', 'extent': [110., 17.76, 112.56, 20.32], 'extent_srid': 4326},
                'raster': {'width': 256, 'height': 256, 'band_count': 2, 'source_srid': 4326,
                'extent': [110., 17.76, 112.56, 20.32], 'extent_srid': 4326,
                    'size_bytes': len(self.cog), 'profile': 'cog', 'client_read_mode': 'range'}})
        if path == '/api/v1/manager/quick-view/actions' and method == 'POST' and body.get('locator') == raster['locator']:
            if body != {'locator': raster['locator'], 'action': 'generate_raster_cog'}:
                raise AssertionError('raster action must only contain source locator and action')
            raster['task_exists'] = raster['result_exists'] = True
            return response(202, {'task_type': SUITE.RASTER_TASK_TYPE, 'task_id': 205, 'execution_id': 'raster-direct'})
        if path == '/api/v1/manager/executions/raster-direct':
            return response(200, self.lineage_execution(SUITE.RASTER_TASK_TYPE))
        if path == '/api/v1/monitor/executions/by-execution-id/raster-direct':
            return response(200, self.monitor_execution(SUITE.RASTER_TASK_TYPE))
        if path == '/api/v1/manager/raster_cog/305/content':
            if not raster['result_exists']: return response(404)
            range_value = (headers or {}).get('Range')
            if range_value == f'bytes={len(self.cog)}-': return response(416)
            if range_value:
                start = 0 if range_value == 'bytes=0-63' else len(self.cog) - 64
                return SUITE.Response(206, {}, {'Content-Length': '64', 'Accept-Ranges': 'bytes',
                    'Content-Range': f'bytes {start}-{start+63}/{len(self.cog)}'}, self.cog[start:start+64])
            return SUITE.Response(200, {}, {'Content-Length': str(len(self.cog))}, self.cog)
        if path == '/api/v1/manager/raster_cog/305' and method == 'DELETE':
            raster['result_exists'] = False
            return response(200)
        if path == f'/api/v1/manager/tasks/{SUITE.RASTER_TASK_TYPE}/205':
            if method == 'DELETE':
                raster['task_exists'] = False
                return response(204)
            return response(200 if raster['task_exists'] else 404)
        if path == '/api/v1/manager/preview-state/preferred-mode' and body.get('locator') == raster['locator']:
            self.raster_mode = body['preferred_mode']
            return response(200)
        if parsed_raster.path == '/api/v1/manager/preview-state' and raster_query.get('locator') == [raster['locator']]:
            return response(200, {'preferred_mode': self.raster_mode})

        if path == "/api/v1/system/auth/context":
            return response(200, {
                "principal": {"type": "user", "id": "51"},
                "context": {"type": "tenant", "tenant_id": "42"},
                "token": {"type": "first_party_access_token"},
                "authorization": {"role_assignments": [{
                    "role_key": "tenant.data_operator",
                    "permissions": sorted(SUITE.REQUIRED_PERMISSIONS),
                }]},
            })
        if path == "/api/v1/meta/scan/run/manual":
            self._assert_scan_request(body)
            return response(201, {"execution_id": "meta-execution-1"})
        if path == "/api/v1/meta/executions/meta-execution-1":
            return response(200, {"status": "success"})
        if path == "/api/v1/meta/engines/27/items":
            return response(200, [
                {
                    "id": self.pointcloud_item_id,
                    "full_name": "addp-online/pointcloud/pdal_las12_format0.las",
                    "item_type": "object",
                    "fingerprint": self.pointcloud_fingerprint,
                    "size_bytes": 128,
                },
                {
                    "id": self.pptx_item_id,
                    "full_name": "addp-online/document/addp_online_preview_fixture.pptx",
                    "item_type": "object",
                    "fingerprint": self.pptx_fingerprint,
                    "size_bytes": 16575,
                },
                {'id': 95, 'full_name': 'addp-online/raster/source.tif', 'item_type': 'object',
                 'fingerprint': 'e'*64, 'size_bytes': 1000},
            ] + [{
                "id": model["item_id"], "full_name": f"addp-online/model3d/{format_name}/model.{format_name}",
                "item_type": "object", "fingerprint": model["fingerprint"], "size_bytes": 1024,
                "attributes": {"item": {"data_type": "model_3d", "format": format_name, "layout": "single"},
                               "format_info": {format_name: {"texture_refs": ["texture.png"], "scan_complete": True, "unit_meter": 0.01}}},
            } for format_name, model in self.models.items()])
        parsed = urllib.parse.urlsplit(path)
        query = urllib.parse.parse_qs(parsed.query)
        if parsed.path == "/api/v1/manager/tasks" and method == "GET":
            rows = [{"id": model["task_id"], "config": {"source": {"item_fingerprint": model["fingerprint"]}}}
                    for model in self.models.values() if model["task_exists"]]
            return response(200, {"items": rows, "total": len(rows)})
        if parsed.path == "/api/v1/manager/model_3d_glb" and method == "GET":
            rows = [{"id": model["result_id"], "task_id": model["task_id"], "item_fingerprint": model["fingerprint"]}
                    for model in self.models.values() if model["result_exists"]
                    and ("task_id" not in query or str(model["task_id"]) == query["task_id"][0])
                    and ("item_fingerprint" not in query or model["fingerprint"] == query["item_fingerprint"][0])]
            return response(200, {"data": rows, "total": len(rows)})
        for format_name, model in self.models.items():
            if parsed.path == "/api/v1/manager/quick-view/capability" and query.get("locator") == [model["locator"]]:
                if not model["result_exists"]:
                    return response(200, {"available_actions": ["generate_model_3d_glb"], "preferred_mode": self.model_modes[format_name]})
                return response(200, {"can_use_quick_view": True, "render_source": "model_3d_glb", "model_3d": {
                    "format": format_name, "task_id": model["task_id"], "result_id": model["result_id"],
                    "last_execution_id": f"model-{format_name}", "preview_url": f"/api/v1/manager/model_3d_glb/{model['result_id']}/content",
                }})
            if path == "/api/v1/manager/quick-view/actions" and method == "POST" and body.get("locator") == model["locator"]:
                if body.get("action") != "generate_model_3d_glb":
                    raise AssertionError("model action must be generate_model_3d_glb")
                model["task_exists"] = model["result_exists"] = True
                return response(202, {"task_type": SUITE.MODEL_TASK_TYPE, "task_id": model["task_id"], "execution_id": f"model-{format_name}"})
            if path == f"/api/v1/manager/executions/model-{format_name}":
                return response(200, self.lineage_execution(SUITE.MODEL_TASK_TYPE, format_name))
            if path == f"/api/v1/monitor/executions/by-execution-id/model-{format_name}":
                return response(200, self.monitor_execution(SUITE.MODEL_TASK_TYPE, format_name))
            if path == f"/api/v1/manager/model_3d_glb/{model['result_id']}/content":
                if not model["result_exists"]:
                    return response(404)
                return response(206 if headers.get("Range") else 200, raw=self.glb[:64] if headers.get("Range") else self.glb)
            if path == f"/api/v1/manager/model_3d_glb/{model['result_id']}" and method == "DELETE":
                model["result_exists"] = False
                return response(200)
            if path == f"/api/v1/manager/tasks/{SUITE.MODEL_TASK_TYPE}/{model['task_id']}":
                if method == "DELETE":
                    model["task_exists"] = False
                    return response(204)
                return response(200 if model["task_exists"] else 404)
            if path == "/api/v1/manager/preview-state/preferred-mode" and body.get("locator") == model["locator"]:
                self.model_modes[format_name] = body["preferred_mode"]
                return response(200)
            if parsed.path == "/api/v1/manager/preview-state" and query.get("locator") == [model["locator"]]:
                return response(200, {"preferred_mode": self.model_modes[format_name]})
        if path == "/api/v1/manager/tasks?task_type=point_cloud_copc_generation&page=1&page_size=100":
            return response(200, {"items": [], "total": 0})
        if path.startswith("/api/v1/manager/point_cloud_copc?item_fingerprint="):
            data = [{"id": 301}] if self.pointcloud_result_exists else []
            return response(200, {"data": data, "total": len(data)})
        if path == "/api/v1/manager/quick-view/actions" and method == "POST" and body.get("locator") == self.pointcloud_locator:
            if body != {"locator": self.pointcloud_locator, "action": "generate_point_cloud_copc"}:
                raise AssertionError(f"unexpected PointCloud action body: {body!r}")
            self.pointcloud_task_exists = self.pointcloud_result_exists = True
            return response(202, {"task_type": SUITE.POINTCLOUD_TASK_TYPE,
                                  "task_id": 201, "execution_id": "manager-execution-1"})
        if path == "/api/v1/manager/executions/manager-execution-1":
            return response(200, self.lineage_execution(SUITE.POINTCLOUD_TASK_TYPE))
        if path == "/api/v1/monitor/executions/by-execution-id/manager-execution-1":
            return response(200, self.monitor_execution(SUITE.POINTCLOUD_TASK_TYPE))
        if path == "/api/v1/manager/point_cloud_copc?task_id=201&page=1&page_size=20":
            data = ([{"id": 301, "file_name": self.pointcloud_output_name}]
                    if self.pointcloud_result_exists else [])
            return response(200, {"data": data, "total": len(data)})
        if path == "/api/v1/manager/point_cloud_copc/301/content":
            if self.pointcloud_result_exists:
                if headers != {"Accept": "application/octet-stream", "Range": "bytes=0-63"}:
                    raise AssertionError(f"unexpected PointCloud Range headers: {headers!r}")
                return response(206, raw=b"COPC fixture")
            return response(404)
        if path.startswith("/api/v1/manager/quick-view/capability?") and method == "GET":
            self.pptx_capability_calls += 1
            if not self.pptx_result_exists:
                return response(200, {
                    "available_actions": ["generate_pptx_pdf"],
                    "pptx_pdf": {"format": "pptx", "status": "missing"},
                })
            return response(200, {
                "available_actions": [],
                "pptx_pdf": {
                    "status": "ready",
                    "task_id": 202,
                    "result_id": 302,
                    "preview_url": "/api/v1/manager/pptx_pdf/302/content",
                    "page_count": 3,
                    "size_bytes": 4096,
                },
            })
        if path == "/api/v1/manager/quick-view/actions" and method == "POST":
            if body != {"locator": self.pptx_locator, "action": "generate_pptx_pdf"}:
                raise AssertionError(f"unexpected PPTX action body: {body!r}")
            self.pptx_task_exists = True
            self.pptx_result_exists = True
            return response(202, {
                "action": "generate_pptx_pdf",
                "task_type": SUITE.PPTX_TASK_TYPE,
                "status": "pending",
                "task_id": 202,
                "execution_id": "manager-execution-2",
            })
        if path == "/api/v1/manager/executions/manager-execution-2":
            return response(200, self.lineage_execution(SUITE.PPTX_TASK_TYPE))
        if path == "/api/v1/monitor/executions/by-execution-id/manager-execution-2":
            return response(200, self.monitor_execution(SUITE.PPTX_TASK_TYPE))
        if path == "/api/v1/manager/pptx_pdf/302/content":
            if self.pptx_result_exists:
                if headers != {"Accept": "application/pdf", "Range": "bytes=0-63"}:
                    raise AssertionError(f"unexpected PPTX Range headers: {headers!r}")
                return response(206, raw=b"%PDF-1.7 fixture")
            return response(404)
        if path == "/api/v1/manager/pptx_pdf/302" and method == "DELETE":
            self.pptx_result_exists = False
            return response(200)
        if path == "/api/v1/manager/tasks/pptx_pdf_generation/202" and method == "DELETE":
            self.pptx_task_exists = False
            return response(204)
        if path == "/api/v1/manager/tasks/pptx_pdf_generation/202" and method == "GET":
            return response(200 if self.pptx_task_exists else 404)
        if path == "/api/v1/manager/point_cloud_copc/301" and method == "DELETE":
            self.pointcloud_result_exists = False
            return response(200)
        if path == "/api/v1/manager/tasks/point_cloud_copc_generation/201" and method == "DELETE":
            self.pointcloud_task_exists = False
            return response(204)
        if path == "/api/v1/manager/tasks/point_cloud_copc_generation/201" and method == "GET":
            return response(200 if self.pointcloud_task_exists else 404)
        raise AssertionError(f"unexpected request {method} {path} body={body!r}")

    @staticmethod
    def _assert_scan_request(body):
        if body != {
            "engine_id": 27,
            "scan_depth": "deep",
            "trigger_type": "manual",
            "force": True,
        }:
            raise AssertionError(f"unexpected Meta scan body: {body!r}")


def scenario_environment(artifact_dir: str):
    return {
        "ADDP_ONLINE_ARTIFACT_DIR": artifact_dir,
        "ADDP_ONLINE_TEST_RUN_ID": "run-1",
        "ADDP_ONLINE_TEST_TENANT_ID": "42",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "token",
        "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID": "27",
        "ADDP_ONLINE_MANAGER_MINIO_BUCKET": "addp-online",
        "ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT": "pointcloud/pdal_las12_format0.las",
        "ADDP_ONLINE_MANAGER_MINIO_RASTER_OBJECT": "raster/source.tif",
        "ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT": "document/addp_online_preview_fixture.pptx",
        "GATEWAY_URL": "http://127.0.0.1:8000",
    }


class ManagerInternalArtifactLineageOnlineTest(unittest.TestCase):
    def setUp(self):
        def physical(repository, environment, action, request):
            if action == 'pdf-verify':
                return {'object_present': True, 'source_unchanged': True, 'source_sha256': 'e' * 64, 'size_bytes': request['size_bytes']}
            if action in ('deleted', 'pdf-deleted'):
                return {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}
            return {'cog_valid': True, 'source_unchanged': True, 'pixels_verified': 131072,
                    'size_bytes': request['size_bytes'], 'sha256': request['sha256']}
        patch = mock.patch.object(SUITE, 'raster_physical', side_effect=physical)
        patch.start(); self.addCleanup(patch.stop)

    def browser_raster(self):
        return {'locator': FakeGatewayClient().raster['locator'], 'item_id': 95,
                'preview_url': '/api/v1/manager/raster_cog/305/content'}

    def browser_models(self):
        client = FakeGatewayClient()
        return [{"format": format_name, "locator": model["locator"], "item_id": model["item_id"],
                 "result_id": model["result_id"], "preview_url": f"/api/v1/manager/model_3d_glb/{model['result_id']}/content"}
                for format_name, model in client.models.items()]

    def browser_report(self, environment, evidence):
        if evidence.get("phase") == "generation-entry":
            return {"schema_version": "addp.manager-internal-artifact-lineage-browser/v4",
                    "phase": "generation-entry", "suite": "manager-internal-artifact-lineage",
                    "run_id": environment["ADDP_ONLINE_TEST_RUN_ID"], "result": "passed",
                    "models": [{**model, "generation_entry_visible": True} for model in evidence["models"]],
                    "model_generation_requests": 0, "browser_warning_errors": 0, "gpu_performance_warnings": 0,
                    "failed_business_responses": 0, "anonymous_refresh_401": 0}
        return {
            "schema_version": "addp.manager-internal-artifact-lineage-browser/v4",
            "phase": evidence.get("phase", "cached-preview"),
            "suite": "manager-internal-artifact-lineage",
            "run_id": environment["ADDP_ONLINE_TEST_RUN_ID"],
            "result": "passed",
            "execution_id": evidence["execution_id"],
            "item_id": evidence["item_id"],
            "output_name": evidence["output_name"],
            "input_resources": 1,
            "output_resources": 1,
            "platform_internal_outputs": 1,
            "pptx_item_id": evidence["pptx_item_id"],
            "pptx_page_count": evidence["pptx_page_count"],
            "pptx_page_after_engine_refresh": 2,
            "pptx_generation_requests": 0,
            "model_generation_requests": 0,
            "raster_generation_requests": 0,
            "raster": {**evidence["raster"], "range_loaded": True, "map_loaded": True},
            "models": [{**model, "model_loaded": True, "content_loaded": True} for model in evidence["models"]],
            "gpu_performance_warnings": 0,
            "browser_warning_errors": 0,
            "failed_business_responses": 0,
            "anonymous_refresh_401": 0,
        }

    def test_direct_runtime_execution_id_is_optional_and_only_reported_when_present(self):
        for runtime_id in (None, "", "runtime-diagnostic-1"):
            with self.subTest(runtime_id=runtime_id):
                client = FakeGatewayClient()
                raster = SUITE.ArtifactFixture('raster', {'id': 95, 'fingerprint': 'e'*64}, client.raster['locator'])
                original = client._request
                def changed(method, path, body, headers):
                    value = original(method, path, body, headers)
                    if '/raster_cog?' in path and value.payload.get('data'):
                        runtime = value.payload['data'][0]['metadata']['workflow_runtime']
                        if runtime_id is None:
                            runtime.pop('execution_id')
                        else:
                            runtime['execution_id'] = runtime_id
                    return value
                def physical(action, request):
                    return {'cog_valid': True, 'source_unchanged': True, 'pixels_verified': 131072,
                            'sha256': request['sha256'], 'size_bytes': request['size_bytes']}
                with mock.patch.object(client, '_request', side_effect=changed):
                    SUITE.generate_raster_cog(client, raster, 42, 1, physical)
                    SUITE.cleanup_managed_artifact(client, raster, 1, 'raster_cog', SUITE.RASTER_TASK_TYPE)
                self.assertEqual(raster.artifact['runtime_engine_id'], 77)
                if runtime_id:
                    self.assertEqual(raster.artifact['runtime_execution_id'], runtime_id)
                else:
                    self.assertNotIn('runtime_execution_id', raster.artifact)

    def test_cleanup_failure_preserves_the_original_raster_assertion(self):
        client = FakeGatewayClient()
        original = client._request
        def changed(method, path, body, headers):
            if method == 'DELETE' and path == '/api/v1/manager/pptx_pdf/302':
                raise SUITE.SuiteError('injected PPTX cleanup failure')
            value = original(method, path, body, headers)
            if '/raster_cog?' in path and value.payload.get('data'):
                value.payload['data'][0]['metadata']['workflow_runtime']['mode'] = 'workflow'
            return value
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch.object(SUITE, 'GatewayClient', return_value=client), mock.patch.object(client, '_request', side_effect=changed):
                with self.assertRaisesRegex(SUITE.SuiteError, 'direct Runtime.*cleanup failed:.*PPTX cleanup failure'):
                    SUITE.run_scenario(Path('/repository'), scenario_environment(directory))

    def test_raster_rejects_size_range_or_direct_identity_mismatches(self):
        for kind in ('size', 'range', 'mode', 'extent', 'physical'):
            with self.subTest(kind=kind):
                client = FakeGatewayClient()
                raster = SUITE.ArtifactFixture('raster', {'id': 95, 'fingerprint': 'e'*64}, client.raster['locator'])
                original = client._request
                def changed(method, path, body, headers):
                    value = original(method, path, body, headers)
                    if '/raster_cog?' in path and value.payload.get('data'):
                        if kind == 'size': value.payload['data'][0]['size_bytes'] += 1
                        if kind == 'mode': value.payload['data'][0]['metadata']['workflow_runtime']['mode'] = 'workflow'
                        if kind == 'extent': value.payload['data'][0]['extent'][0] = 109.
                    if kind == 'range' and path.endswith('/raster_cog/305/content') and value.status == 206:
                        value.raw = b'corrupt'
                    return value
                def physical(action, request):
                    return {'cog_valid': True, 'source_unchanged': True, 'pixels_verified': 131072,
                            'sha256': request['sha256'], 'size_bytes': request['size_bytes'] + (kind == 'physical')}
                with mock.patch.object(client, '_request', side_effect=changed):
                    with self.assertRaises(SUITE.SuiteError):
                        SUITE.generate_raster_cog(client, raster, 42, 1, physical)
                    SUITE.cleanup_managed_artifact(client, raster, 1, 'raster_cog', SUITE.RASTER_TASK_TYPE)
                self.assertFalse(client.raster['task_exists'] or client.raster['result_exists'])

    def test_raster_generation_failure_recovers_owned_locator_for_physical_delete(self):
        client = FakeGatewayClient()
        original = client._request
        def bad_runtime(method, path, body, headers):
            value = original(method, path, body, headers)
            if '/raster_cog?' in path and value.payload.get('data'):
                value.payload['data'][0]['metadata']['workflow_runtime']['mode'] = 'workflow'
            return value
        deletes = []
        def physical(action, request):
            if action.startswith('pdf-'):
                return SUITE.raster_physical(Path('/repository'), {}, action, request)
            deletes.append((action, request))
            return {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch.object(SUITE, 'GatewayClient', return_value=client), mock.patch.object(client, '_request', side_effect=bad_runtime):
                with self.assertRaisesRegex(SUITE.SuiteError, 'direct Runtime'):
                    SUITE.run_scenario(Path('/repository'), scenario_environment(directory), physical_runner=physical)
        self.assertEqual([action for action, _ in deletes], ['deleted'])
        self.assertIn('/tenant_42/cog/', deletes[0][1]['locator'])
        self.assertFalse(client.raster['task_exists'] or client.raster['result_exists'])

    def test_pdf_physical_cleanup_cannot_be_replaced_by_teardown(self):
        for evidence in ({'object_deleted': False, 'source_unchanged': True, 'residual_objects': 0},
                         {'object_deleted': True, 'source_unchanged': False, 'residual_objects': 0},
                         {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 1}):
            client = FakeGatewayClient()
            oracle = SUITE.raster_physical
            def physical(action, request):
                if action == 'pdf-deleted':
                    return evidence
                return oracle(Path('/repository'), {}, action, request)
            with self.subTest(evidence=evidence), tempfile.TemporaryDirectory() as directory:
                with mock.patch.object(SUITE, 'GatewayClient', return_value=client):
                    with self.assertRaisesRegex(SUITE.SuiteError, 'PDF physical deletion'):
                        SUITE.run_scenario(Path('/repository'), scenario_environment(directory), physical_runner=physical)
                self.assertFalse(client.pptx_result_exists or client.pptx_task_exists)

    def test_raster_browser_evidence_is_mandatory(self):
        evidence = {'execution_id': 'execution-1', 'item_id': 91, 'output_name': 'source.copc.laz',
                    'pptx_item_id': 92, 'pptx_page_count': 3, 'models': self.browser_models(), 'raster': self.browser_raster()}
        for field in ('range_loaded', 'map_loaded'):
            report = self.browser_report({'ADDP_ONLINE_TEST_RUN_ID': 'run-1'}, evidence)
            report['raster'][field] = False
            with self.assertRaisesRegex(SUITE.SuiteError, 'raster'):
                SUITE.validate_browser_report(report, run_id='run-1', **evidence)

    def test_runs_all_owner_routes_and_verifies_zero_residual_resources(self) -> None:
        client = FakeGatewayClient()
        browser_calls = []

        def browser(repository, environment, **evidence):
            browser_calls.append((repository, evidence))
            generated = any(method == "POST" and path == "/api/v1/manager/quick-view/actions"
                            and model["task_exists"] for method, path in client.calls for model in client.models.values())
            self.assertEqual(generated, evidence["phase"] == "cached-preview")
            return self.browser_report(environment, evidence)

        with tempfile.TemporaryDirectory() as artifact_dir:
            with mock.patch.object(SUITE, "GatewayClient", return_value=client):
                report = SUITE.run_scenario(
                    Path("/repository"), scenario_environment(artifact_dir), browser
                )

        self.assertEqual(report["lineage"]["inputs"], 5)
        self.assertEqual(report["lineage"]["outputs"], 5)
        self.assertTrue(report["artifacts"]["pptx_pdf"]["cache_reused"])
        self.assertEqual(report['raster_cog']['mode'], 'direct')
        self.assertTrue(report['raster_cog']['physical']['cog_valid'])
        self.assertTrue(report['cleanup']['raster_cog']['object_deleted'])
        self.assertEqual(report['cleanup']['raster_cog']['residual_objects'], 0)
        self.assertFalse(client.raster['task_exists'] or client.raster['result_exists'])
        expected_cleanup = {
            "result_deleted": True,
            "task_deleted": True,
            "content_unavailable": True,
            "residual_resources": 0,
        }
        self.assertEqual(report["cleanup"]["point_cloud"], expected_cleanup)
        self.assertEqual(report["cleanup"]["pptx_pdf"], {**expected_cleanup,
            "object_deleted": True, "source_unchanged": True, "residual_objects": 0,
            "result_unavailable": True, "source_item_preserved": True, "execution_preserved": True})
        for format_name, model in client.models.items():
            self.assertEqual(report["cleanup"][format_name], {**expected_cleanup, "preview_mode_restored": True})
            self.assertFalse(model["task_exists"])
            self.assertFalse(model["result_exists"])
            self.assertEqual(client.model_modes[format_name], "basic_preview")
            self.assertEqual(report["artifacts"][format_name]["embedded_images"], 1)
            self.assertEqual(report["artifacts"][format_name]["vertex_count"], 3)
        self.assertFalse(client.pointcloud_task_exists)
        self.assertFalse(client.pointcloud_result_exists)
        self.assertFalse(client.pptx_task_exists)
        self.assertFalse(client.pptx_result_exists)
        self.assertTrue(all(not model["task_exists"] and not model["result_exists"] for model in client.models.values()))
        self.assertEqual(client.model_modes, {"dae": "basic_preview", "3ds": "basic_preview"})
        self.assertEqual(client.pptx_capability_calls, 3)
        self.assertEqual(browser_calls[0][1]["pptx_page_count"], 3)
        self.assertEqual([call[1]["phase"] for call in browser_calls], ["generation-entry", "cached-preview"])
        self.assertTrue(report["browser"]["generation_entry"]["models"][1]["generation_entry_visible"])
        self.assertLess(
            client.calls.index(("DELETE", "/api/v1/manager/pptx_pdf/302")),
            client.calls.index(("DELETE", "/api/v1/manager/tasks/pptx_pdf_generation/202")),
        )

    def test_browser_failure_still_deletes_all_results_and_tasks(self) -> None:
        client = FakeGatewayClient()

        def browser(_repository, environment, **evidence):
            if evidence["phase"] == "generation-entry":
                return self.browser_report(environment, evidence)
            raise SUITE.SuiteError("injected browser failure")

        with tempfile.TemporaryDirectory() as artifact_dir:
            with mock.patch.object(SUITE, "GatewayClient", return_value=client):
                with self.assertRaisesRegex(SUITE.SuiteError, "injected browser failure"):
                    SUITE.run_scenario(
                        Path("/repository"), scenario_environment(artifact_dir), browser
                    )

        self.assertFalse(client.pointcloud_task_exists)
        self.assertFalse(client.pointcloud_result_exists)
        self.assertFalse(client.pptx_task_exists)
        self.assertFalse(client.pptx_result_exists)
        self.assertTrue(all(not model["task_exists"] and not model["result_exists"] for model in client.models.values()))
        self.assertEqual(client.model_modes, {"dae": "basic_preview", "3ds": "basic_preview"})

    def test_rejects_missing_scanned_texture_before_manager_writes(self):
        client = FakeGatewayClient()
        original = client._request

        def missing_texture(method, path, body, headers):
            result = original(method, path, body, headers)
            if path == "/api/v1/meta/engines/27/items":
                result.payload[-1]["attributes"]["format_info"]["3ds"]["texture_refs"] = []
            return result

        with mock.patch.object(client, "_request", side_effect=missing_texture), mock.patch.object(SUITE, "GatewayClient", return_value=client):
            with self.assertRaisesRegex(SUITE.SuiteError, "declared PNG texture"):
                SUITE.run_scenario(Path("/repository"), scenario_environment("/tmp"))
        self.assertFalse(any(method == "POST" and path.startswith("/api/v1/manager") for method, path in client.calls))

    def test_failed_model_execution_deletes_its_failed_result_and_other_completed_results(self):
        client = FakeGatewayClient()
        original = client._request

        def failed_model(method, path, body, headers):
            if path == "/api/v1/manager/executions/model-3ds":
                return response(200, {"status": "failed"})
            return original(method, path, body, headers)

        with mock.patch.object(client, "_request", side_effect=failed_model), mock.patch.object(SUITE, "GatewayClient", return_value=client):
            with self.assertRaisesRegex(SUITE.SuiteError, "3DS execution ended with status failed"):
                SUITE.run_scenario(Path("/repository"), scenario_environment("/tmp"))
        self.assertTrue(all(not model["task_exists"] and not model["result_exists"] for model in client.models.values()))
        self.assertFalse(client.pptx_task_exists or client.pptx_result_exists or client.pointcloud_task_exists or client.pointcloud_result_exists)
        self.assertEqual(client.model_modes, {"dae": "basic_preview", "3ds": "basic_preview"})

    def test_active_execution_is_retained_and_cannot_report_zero_residuals(self):
        client = FakeGatewayClient()
        model = SUITE.prepare_model_fixture(client, 27, "addp-online", "dae")
        model.task_id = 203
        model.execution_id = "model-dae"
        client.models["dae"]["task_exists"] = client.models["dae"]["result_exists"] = True
        original = client._request

        def active(method, path, body, headers):
            if path == "/api/v1/manager/executions/model-dae":
                return response(200, {"status": "running"})
            return original(method, path, body, headers)

        with mock.patch.object(client, "_request", side_effect=active):
            with self.assertRaisesRegex(SUITE.SuiteError, "still active; resources retained"):
                SUITE.cleanup_managed_artifact(client, model, 0)
        self.assertFalse(any(method == "DELETE" for method, _ in client.calls))
        self.assertEqual(model.cleanup["residual_resources"], -1)

    def test_foreign_result_is_never_deleted(self):
        client = FakeGatewayClient()
        model = SUITE.prepare_model_fixture(client, 27, "addp-online", "dae")
        model.task_id = 203
        model.execution_id = "model-dae"
        original = client._request

        def foreign(method, path, body, headers):
            if path.startswith("/api/v1/manager/model_3d_glb?task_id="):
                return response(200, {"data": [{"id": 999, "task_id": 203, "item_fingerprint": "foreign"}], "total": 1})
            return original(method, path, body, headers)

        with mock.patch.object(client, "_request", side_effect=foreign):
            with self.assertRaisesRegex(SUITE.SuiteError, "not owned by this run"):
                SUITE.cleanup_managed_artifact(client, model, 0)
        self.assertFalse(any(method == "DELETE" for method, _ in client.calls))
        self.assertEqual(model.cleanup["residual_resources"], -1)

    def test_preview_restoration_failure_still_deletes_owned_artifact_and_task(self):
        client = FakeGatewayClient()
        model = SUITE.prepare_model_fixture(client, 27, "addp-online", "dae")
        SUITE.generate_model_glb(client, model, 42, 1)
        original = client._request
        def wrong_state(method, path, body, headers):
            if path.startswith("/api/v1/manager/preview-state?"):
                return response(200, {"preferred_mode": "map_quick_view"})
            return original(method, path, body, headers)
        with mock.patch.object(client, "_request", side_effect=wrong_state):
            with self.assertRaisesRegex(SUITE.SuiteError, "restoration was not verified"):
                SUITE.cleanup_managed_artifact(client, model, 1)
        self.assertFalse(client.models["dae"]["task_exists"] or client.models["dae"]["result_exists"])
        self.assertFalse(model.cleanup["preview_mode_restored"])
        self.assertEqual(model.cleanup["residual_resources"], 0)

    def test_glb_requires_a_textured_mesh_and_embedded_image(self):
        self.assertEqual(SUITE.validate_model_glb(textured_glb())["embedded_images"], 1)
        for change, reason in (
            (lambda doc: doc.update(images=[]), "exactly one embedded PNG"),
            (lambda doc: doc["images"][0].update(uri="texture.png"), "embedded PNG bufferView"),
            (lambda doc: doc["buffers"][0].update(uri="mesh.bin"), "external buffers"),
            (lambda doc: doc["accessors"][0].update(count=0), "textured three-vertex mesh"),
            (lambda doc: doc["meshes"][0]["primitives"][0]["attributes"].pop("TEXCOORD_0"), "textured three-vertex mesh"),
            (lambda doc: doc["textures"][0].update(source=1), "must use the embedded PNG"),
        ):
            with self.subTest(reason=reason), self.assertRaisesRegex(SUITE.SuiteError, reason):
                SUITE.validate_model_glb(alter_glb(textured_glb(), change))

    def execution(self, output_locator: str | None = None):
        return {
            "metadata": {"lineage_facts": {
                "schema_version": SUITE.LINEAGE_SCHEMA,
                "inputs": [{
                    "port": "source",
                    "locator": "addp://engine/12/path/addp-online/pointcloud/source.las?type=object&item_id=91",
                    "item_id": 91,
                    "item_fingerprint": "sha256:" + "a" * 64,
                }],
                "outputs": [{
                    "port": "result",
                    "locator": output_locator or "addp-infra://minio/manager/tenant_42/point-cloud-copc/source/source.copc.laz?type=object",
                }],
                "operations": [{
                    "kind": "derive",
                    "operator": SUITE.POINTCLOUD_TASK_TYPE,
                    "input_ports": ["source"],
                    "output_ports": ["result"],
                }],
            }}
        }

    def validate_pointcloud_lineage(self, execution):
        return SUITE.validate_lineage(
            execution,
            item_locator="addp://engine/12/path/addp-online/pointcloud/source.las?type=object&item_id=91",
            item_id=91,
            fingerprint="sha256:" + "a" * 64,
            tenant_id=42,
            task_type=SUITE.POINTCLOUD_TASK_TYPE,
            output_prefix="point-cloud-copc",
            output_suffix=".copc.laz",
            output_label="COPC",
        )

    def test_validates_owner_facts_for_business_input_and_infra_output(self) -> None:
        facts = self.validate_pointcloud_lineage(self.execution())
        self.assertEqual(facts["schema_version"], SUITE.LINEAGE_SCHEMA)
        self.assertEqual(facts["inputs"][0]["port"], "source")
        self.assertEqual(facts["outputs"][0]["port"], "result")

    def test_monitor_accepts_safe_projection_without_changing_owner_or_response(self) -> None:
        owner = self.execution()["metadata"]["lineage_facts"]
        owner["inputs"][0]["schema_snapshot"] = {"private_column": "private_type"}
        execution = copy.deepcopy(self.execution())
        facts = execution["metadata"]["lineage_facts"]
        facts["operations"] = None
        facts["inputs"][0]["locator"] = facts["inputs"][0]["locator"].replace(
            "type=object&item_id=91", "item_id=91&type=object"
        )
        before = copy.deepcopy(execution)
        SUITE.validate_monitor_lineage(execution, owner)
        self.assertEqual(execution, before)
        self.assertIn("schema_snapshot", owner["inputs"][0])

    def test_monitor_rejects_private_payload_and_changed_resource_identity(self) -> None:
        owner = self.execution()["metadata"]["lineage_facts"]
        safe = self.execution()
        safe["metadata"]["lineage_facts"]["operations"] = None
        for field, value in {
            "execution_config": {"query": "private SQL"},
            "actor_principal_id": 51,
            "execution_authorization_id": "private-reference",
            "lease_token": "private-token",
        }.items():
            with self.subTest(field=field):
                execution = copy.deepcopy(safe)
                execution[field] = value
                with self.assertRaisesRegex(SUITE.SuiteError, "private execution fields"):
                    SUITE.validate_monitor_lineage(execution, owner)
        for field in ("result", "outputs", "step_results"):
            with self.subTest(metadata=field):
                execution = copy.deepcopy(safe)
                execution["metadata"][field] = {"private": "value"}
                with self.assertRaisesRegex(SUITE.SuiteError, "private execution metadata"):
                    SUITE.validate_monitor_lineage(execution, owner)
        for field, value, reason in (
            ("schema_snapshot", {"private_column": "type"}, "private lineage resource"),
            ("item_id", 92, "differs from Owner"),
            ("locator", owner["inputs"][0]["locator"] + "&token=private", "private context"),
        ):
            with self.subTest(resource=field):
                execution = copy.deepcopy(safe)
                execution["metadata"]["lineage_facts"]["inputs"][0][field] = value
                with self.assertRaisesRegex(SUITE.SuiteError, reason):
                    SUITE.validate_monitor_lineage(execution, owner)
        with self.assertRaisesRegex(SUITE.SuiteError, "lineage operations"):
            SUITE.validate_monitor_lineage(self.execution(), owner)

    def test_rejects_business_output_instead_of_manager_internal_artifact(self) -> None:
        with self.assertRaisesRegex(SUITE.SuiteError, "not the Manager infra COPC artifact"):
            self.validate_pointcloud_lineage(
                self.execution("addp://engine/12/path/output/source.copc.laz?type=object")
            )

    def test_builds_canonical_resource_locator_from_meta_item(self) -> None:
        locator = SUITE.build_item_locator(
            12, {"id": 91, "full_name": "addp-online/point cloud/source.las"}
        )
        self.assertEqual(
            locator,
            "addp://engine/12/path/addp-online/point%20cloud/source.las?type=object&item_id=91",
        )

    def test_accepts_zero_as_a_canonical_non_negative_count(self) -> None:
        self.assertEqual(SUITE.non_negative_int(0, "total"), 0)

    def test_rejects_non_canonical_non_negative_count(self) -> None:
        with self.assertRaisesRegex(SUITE.SuiteError, "canonical"):
            SUITE.non_negative_int("00", "total")

    def test_validates_browser_report_contract(self) -> None:
        evidence = {
            "execution_id": "execution-1",
            "item_id": 91,
            "output_name": "source.copc.laz",
            "pptx_item_id": 92,
            "pptx_page_count": 3,
            "models": self.browser_models(), "raster": self.browser_raster(),
        }
        report = self.browser_report({"ADDP_ONLINE_TEST_RUN_ID": "run-1"}, evidence)
        validated = SUITE.validate_browser_report(
            report,
            run_id="run-1",
            execution_id="execution-1",
            item_id=91,
            output_name="source.copc.laz",
            pptx_item_id=92,
            pptx_page_count=3,
            models=evidence["models"],
            raster=evidence["raster"],
        )
        self.assertEqual(validated, report)

    def test_rejects_browser_report_without_stable_pptx_page(self) -> None:
        evidence = {
            "execution_id": "execution-1",
            "item_id": 91,
            "output_name": "source.copc.laz",
            "pptx_item_id": 92,
            "pptx_page_count": 3,
            "models": self.browser_models(), "raster": self.browser_raster(),
        }
        report = self.browser_report({"ADDP_ONLINE_TEST_RUN_ID": "run-1"}, evidence)
        report["pptx_page_after_engine_refresh"] = 1
        with self.assertRaisesRegex(SUITE.SuiteError, "pptx_page_after_engine_refresh"):
            SUITE.validate_browser_report(
                report,
                run_id="run-1",
                execution_id="execution-1",
                item_id=91,
                output_name="source.copc.laz",
                pptx_item_id=92,
                pptx_page_count=3,
                models=evidence["models"],
            raster=evidence["raster"],
            )

    def test_browser_model_report_must_match_both_artifacts_and_loaded_content(self):
        evidence = {"execution_id": "execution-1", "item_id": 91, "output_name": "source.copc.laz",
                    "pptx_item_id": 92, "pptx_page_count": 3, "models": self.browser_models(), "raster": self.browser_raster()}
        for change in (
            lambda report: report.pop("models"),
            lambda report: report["models"][0].update(result_id=999),
            lambda report: report["models"][1].update(content_loaded=False),
            lambda report: report.update(model_generation_requests=1),
        ):
            report = self.browser_report({"ADDP_ONLINE_TEST_RUN_ID": "run-1"}, evidence)
            change(report)
            with self.assertRaisesRegex(SUITE.SuiteError, "report contract mismatch"):
                SUITE.validate_browser_report(report, run_id="run-1", **evidence)

    def test_generation_entry_report_rejects_missing_or_wrong_model_and_generation(self):
        models = [{key: model[key] for key in ("format", "locator", "item_id")} for model in self.browser_models()]
        evidence = {"execution_id": "execution-1", "item_id": 91, "output_name": "source.copc.laz",
                    "pptx_item_id": 92, "pptx_page_count": 3, "models": models,
                    "raster": self.browser_raster(), "phase": "generation-entry"}
        for mutate in (lambda report: report["models"][0].update(generation_entry_visible=False),
                       lambda report: report["models"].pop(),
                       lambda report: report["models"][1].update(item_id=999),
                       lambda report: report.update(model_generation_requests=1)):
            report = self.browser_report({"ADDP_ONLINE_TEST_RUN_ID": "run-1"}, evidence)
            mutate(report)
            with self.assertRaisesRegex(SUITE.SuiteError, "report contract mismatch"):
                SUITE.validate_browser_report(report, run_id="run-1", **evidence)

    def test_browser_report_keeps_anonymous_diagnostics_and_rejects_business_failures(self):
        for phase in ("generation-entry", "cached-preview"):
            models = self.browser_models()
            if phase == "generation-entry":
                models = [{key: model[key] for key in ("format", "locator", "item_id")} for model in models]
            evidence = {"execution_id": "execution-1", "item_id": 91, "output_name": "source.copc.laz",
                        "pptx_item_id": 92, "pptx_page_count": 3, "models": models,
                        "raster": self.browser_raster(), "phase": phase}
            report = self.browser_report({"ADDP_ONLINE_TEST_RUN_ID": "run-1"}, evidence)
            report["anonymous_refresh_401"] = 1
            SUITE.validate_browser_report(report, run_id="run-1", **evidence)
            for mutate in (lambda value: value.update(anonymous_refresh_401=2),
                           lambda value: value.pop("anonymous_refresh_401"),
                           lambda value: value.update(failed_business_responses=1)):
                invalid = copy.deepcopy(report)
                mutate(invalid)
                with self.assertRaises(SUITE.SuiteError):
                    SUITE.validate_browser_report(invalid, run_id="run-1", **evidence)

    def test_generation_entry_failure_prevents_model_writes_and_preserves_cleanup(self):
        client = FakeGatewayClient()
        def browser(_repository, environment, **evidence):
            report = self.browser_report(environment, evidence)
            report["models"][1]["generation_entry_visible"] = False
            return report
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(SUITE, "GatewayClient", return_value=client):
            with self.assertRaisesRegex(SUITE.SuiteError, "report contract mismatch"):
                SUITE.run_scenario(Path("/repository"), scenario_environment(directory), browser)
        self.assertFalse(client.pptx_result_exists or client.pptx_task_exists)
        self.assertFalse(client.raster["result_exists"] or client.raster["task_exists"])
        self.assertTrue(all(not model["task_exists"] for model in client.models.values()))

    def test_browser_runner_uses_frontend_working_directory_and_current_report_contract(self):
        evidence = {"execution_id": "execution-1", "item_id": 91, "output_name": "source.copc.laz",
                    "pptx_item_id": 92, "pptx_page_count": 3, "models": self.browser_models(), "raster": self.browser_raster()}
        with tempfile.TemporaryDirectory() as directory:
            environment = {"ADDP_ONLINE_ARTIFACT_DIR": directory, "ADDP_ONLINE_TEST_RUN_ID": "run-1"}
            def browser(*args, **kwargs):
                self.assertEqual(kwargs["cwd"], Path("/repository/console/frontend"))
                self.assertEqual(args[0][:4], ["npm", "exec", "--", "playwright"])
                self.assertIn("--config=playwright.online.config.js", args[0])
                self.assertEqual(json.loads(kwargs["env"]["ADDP_ONLINE_MANAGER_MODELS_JSON"]), evidence["models"])
                Path(directory, "manager-internal-artifact-lineage-browser-cached-preview.json").write_text(json.dumps(self.browser_report(environment, evidence)))
                return mock.Mock(returncode=0)
            with mock.patch.object(SUITE.subprocess, "run", side_effect=browser):
                report = SUITE.run_browser(Path("/repository"), environment,
                                          source_name="source.las", pptx_item_locator="addp://engine/27/path/slides.pptx?type=object&item_id=92", **evidence)
            self.assertEqual(report["schema_version"], "addp.manager-internal-artifact-lineage-browser/v4")


if __name__ == "__main__":
    unittest.main()

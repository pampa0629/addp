"""Tenant Develop service transport for one Runtime's private execution files."""
from __future__ import annotations

import asyncio
import json
import os
import threading
from datetime import datetime, timezone
from functools import wraps

import httpx
from flask import Response, jsonify, request, stream_with_context

from addp_common.auth import resolve_authorization_context
from addp_common.workflow_runtime import ExecutionSnapshot, WorkflowValidationError, validate_execution_authorization
from private_files import FileManifest, PrivateFileError, PrivateFiles


class PrivateFileAPI:
    def __init__(self, app, *, parent=None):
        self._parent = parent
        self._lock = threading.RLock()
        self._files = {}
        app.register_error_handler(PrivateFileError, self.error)
        for suffix, methods, handler in [
            ("", ["PUT"], self.create),
            ("", ["GET"], self.status),
            ("", ["DELETE"], self.cleanup),
            ("/inputs/<int:index>", ["PUT"], self.upload),
            ("/outputs/<int:index>", ["GET"], self.download),
            ("/result", ["GET"], self.professional_result),
        ]:
            app.add_url_rule("/api/private-files/<uuid:runtime_id>" + suffix,
                             "private_files_" + handler.__name__, self.protected(handler), methods=methods)

    def protected(self, handler):
        @wraps(handler)
        def call(**kwargs):
            try:
                return handler(self.creator(), **kwargs)
            except OSError:
                raise PrivateFileError("PRIVATE_FILES_IO_FAILED", 500) from None
        return call

    @staticmethod
    def creator():
        header = request.headers.get("Authorization", "").split()
        if len(header) != 2 or header[0] != "Bearer" or request.headers.get("X-API-Key"):
            raise PrivateFileError("PRIVATE_FILES_UNAUTHENTICATED", 401)
        try:
            context = asyncio.run(resolve_authorization_context(
                os.getenv("SYSTEM_URL", "http://localhost:8180"), header[1]))
        except httpx.HTTPStatusError as exc:
            if exc.response.status_code == 401:
                raise PrivateFileError("PRIVATE_FILES_UNAUTHENTICATED", 401) from None
            raise PrivateFileError("PRIVATE_FILES_AUTH_UNAVAILABLE", 503) from None
        except (httpx.HTTPError, ValueError):
            raise PrivateFileError("PRIVATE_FILES_AUTH_UNAVAILABLE", 503) from None
        if context.expires_at <= datetime.now(timezone.utc):
            raise PrivateFileError("PRIVATE_FILES_UNAUTHENTICATED", 401)
        if (context.principal_type != "service_principal" or context.context_type != "tenant" or
                context.tenant_id is None or context.client_id != "addp-develop" or
                context.token_type != "service_access_token" or "addp.api" not in context.audiences or
                "system.workflow_private_files.execute" not in context.permissions):
            raise PrivateFileError("PRIVATE_FILES_FORBIDDEN", 403)
        return str(context.principal_id), str(context.tenant_id)

    @staticmethod
    def error(exc):
        language = request.accept_languages.best_match(["zh-cn", "en"]) or "zh-cn"
        messages = {
            "zh-cn": {401: "需要有效的服务访问身份", 403: "服务无权访问执行私有文件",
                      404: "执行私有文件不存在", 503: "暂时无法确认服务访问身份"},
            "en": {401: "A valid service identity is required", 403: "Service access to private execution files is denied",
                   404: "Private execution files were not found", 503: "Service identity verification is unavailable"},
        }
        message = messages[language].get(exc.status, "执行私有文件请求失败" if language == "zh-cn" else
                                        "Private execution file request failed")
        return jsonify(status="failed", error=message, error_code=exc.code), exc.status

    def lookup(self, runtime_id, creator):
        with self._lock:
            files = self._files.get(str(runtime_id))
        if files is None or files.creator != creator:
            raise PrivateFileError("PRIVATE_FILES_NOT_FOUND", 404)
        return files

    def contains(self, runtime_id):
        with self._lock:
            return runtime_id in self._files

    def create(self, creator, runtime_id):
        manifest = FileManifest.parse(request.get_json(silent=True))
        if runtime_id.int == 0:
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        with self._lock:
            if str(runtime_id) in self._files:
                self.lookup(runtime_id, creator)
                raise PrivateFileError("PRIVATE_FILES_ALREADY_CREATED")
            files = PrivateFiles(manifest, creator, parent=self._parent)
            self._files[str(runtime_id)] = files
        return jsonify(runtime_execution_id=str(runtime_id), **files.status(creator)), 201

    def status(self, creator, runtime_id):
        return jsonify(runtime_execution_id=str(runtime_id), **self.lookup(runtime_id, creator).status(creator))

    def upload(self, creator, runtime_id, index):
        files = self.lookup(runtime_id, creator)
        if request.mimetype != "application/octet-stream":
            raise PrivateFileError("PRIVATE_FILES_MEDIA_TYPE", 415)
        files.put_input(creator, index, request.stream)
        return jsonify(runtime_execution_id=str(runtime_id), input_index=index, state="ready")

    def download(self, creator, runtime_id, index):
        files = self.lookup(runtime_id, creator)
        # Admission happens before headers. The generator retains the admitted
        # stream until the WSGI server closes the response, including disconnect.
        transfer = files.open_output(creator, index)
        stream = transfer.__enter__()
        receipt = files.status(creator)["outputs"][index]

        def chunks():
            for data in iter(lambda: stream.read(64 * 1024), b""):
                yield data

        response = Response(stream_with_context(chunks()), mimetype="application/geopackage+sqlite3")
        response.content_length = int(receipt["size_bytes"])
        response.call_on_close(lambda: transfer.__exit__(None, None, None))
        return response

    def cleanup(self, creator, runtime_id):
        result = self.lookup(runtime_id, creator).cleanup(creator)
        return jsonify(result), 200 if result["cleanup"] == "passed" else 202

    def professional_result(self, creator, runtime_id):
        value = {"runtime_execution_id": str(runtime_id),
                 **self.lookup(runtime_id, creator).professional_result(creator)}
        response = Response(json.dumps(value, ensure_ascii=False, allow_nan=False), mimetype="application/json")
        response.headers["Cache-Control"] = "no-store"
        return response

    def execute(self, data, language):
        creator = self.creator()
        runtime = data.get("runtime")
        if (set(data) != {"runtime"} or not isinstance(runtime, dict) or
                set(runtime) != {"tenant_id", "execution_authorization", "private_files"} or
                not isinstance(runtime["private_files"], dict) or
                set(runtime["private_files"]) != {"execution_id"} or
                not isinstance(runtime["private_files"]["execution_id"], str)):
            raise PrivateFileError("PRIVATE_FILES_INVALID", 400)
        runtime_id = runtime["private_files"]["execution_id"]
        files = self.lookup(runtime_id, creator)
        workflow = json.loads(files.manifest.workflow_json)
        from operators import OPERATORS
        try:
            validate_execution_authorization(workflow, operator_effects={
                key: ["read"] if key in {"load", "save"} else op["effects"] for key, op in OPERATORS.items()},
                                             runtime=runtime)
        except WorkflowValidationError:
            raise PrivateFileError("PRIVATE_FILES_WORKFLOW_MISMATCH", 400) from None
        authorization = runtime["execution_authorization"]
        if (set(authorization) != {"id", "effects"} or str(runtime["tenant_id"]) != files.manifest.tenant_id or
                str(authorization["id"]) != files.manifest.authorization_id or
                set(authorization["effects"]) != {"read"}):
            raise PrivateFileError("PRIVATE_FILES_WORKFLOW_MISMATCH", 400)
        try:
            files.start(creator, language)
        except OSError:
            raise PrivateFileError("PRIVATE_FILES_IO_FAILED", 500) from None
        return jsonify(self.execution_snapshot(runtime_id, creator)), 202

    def execution_snapshot(self, runtime_id, creator=None):
        creator = self.creator() if creator is None else creator
        try:
            report = self.lookup(runtime_id, creator).status(creator)["execution"]
        except OSError:
            raise PrivateFileError("PRIVATE_FILES_IO_FAILED", 500) from None
        return ExecutionSnapshot(
            execution_id=runtime_id, status=report["status"], progress=100 if report["status"] == "success" else 0,
            result=report.get("result"), all_results=report.get("all_results"), task_order=report.get("task_order", []),
            error_code=report.get("error_code", ""), diagnostic=report.get("diagnostic"),
        ).to_dict()

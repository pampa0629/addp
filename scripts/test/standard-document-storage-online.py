"""Verify pinned Standard HTTP storage and cross-tenant denial through Gateway."""
import hashlib
import json
import os
import sys
from functools import cache
from io import BytesIO
from pathlib import Path
from importlib import import_module

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
api = import_module("scripts.utils.online-api")


def require(condition, message):
    if not condition:
        raise api.SuiteError(message)


@cache
def object_store():
    from minio import Minio
    from urllib3 import PoolManager, Timeout

    return Minio(os.environ["ADDP_ONLINE_OBJECTSTORE_ENDPOINT"],
                 access_key=os.environ["MINIO_ROOT_USER"], secret_key=os.environ["MINIO_ROOT_PASSWORD"],
                 secure=False, http_client=PoolManager(timeout=Timeout(connect=5, read=20), retries=False))


def object_bytes(bucket, key):
    response = object_store().get_object(bucket, key)
    try:
        return response.read()
    finally:
        response.close()
        response.release_conn()


def put_object(bucket, key, content):
    object_store().put_object(bucket, key, BytesIO(content), len(content))


def absent(bucket, key):
    from minio.error import S3Error

    try:
        object_store().stat_object(bucket, key)
    except S3Error as error:
        require(error.code in {"NoSuchKey", "NoSuchBucket"}, "S3 must report a missing object or bucket")
        return
    raise api.SuiteError("expected physical object to be missing")


def main():
    tenant = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"])
    foreign_tenant = int(os.environ["ADDP_ONLINE_STANDARD_FOREIGN_TENANT_ID"])
    require(tenant != foreign_tenant and min(tenant, foreign_tenant) > 1, "distinct nondefault tenants required")
    client = api.GatewayClient(os.environ["GATEWAY_URL"], os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], 30)
    foreign = api.GatewayClient(os.environ["GATEWAY_URL"], os.environ["ADDP_ONLINE_STANDARD_FOREIGN_TOKEN"], 30)
    owner_identity = api.validate_user_identity(client, tenant, {
        "standard.document.create", "standard.document.read", "standard.document.update", "standard.document.delete"})
    foreign_identity = api.validate_user_identity(foreign, foreign_tenant, {"standard.document.read"})
    # The foreign reader must reach the same route with its own read permission.
    foreign.request("GET", "/api/v1/standard/documents", (200,))
    root = "/api/v1/standard/documents"
    doc = client.request("POST", root, (201,), {
        "scope_type": "tenant_common", "code": "http_storage_acceptance",
        "name": "HTTP storage acceptance", "doc_type": "reference",
        "effective_from": "2026-01-01T00:00:00Z",
    }).payload
    document_id, revision_id = doc["id"], doc["draft_revision"]["id"]
    path = f"{root}/{document_id}"
    file_path = f"{path}/revisions/{revision_id}/file"
    bucket = "standard-http-documents"
    report = {"suite": "standard-document-storage", "product_sha": os.environ["STANDARD_HTTP_SOURCE_COMMIT"],
              "harness_sha": os.environ["GITHUB_SHA"], "document_id": document_id, "revision_id": revision_id,
              "objectstore_provider": os.environ["ADDP_INFRA_OBJECTSTORE_PROVIDER"],
              "physical_bucket": bucket, "owner_identity": owner_identity, "foreign_identity": foreign_identity,
              "observations": []}
    try:
        version = doc["version"]
        prior_key = None
        for content in (b"first\x00document\xff", b"replacement\x00document\xfe"):
            boundary = "addp-standard-http-boundary"
            body = (
                f'--{boundary}\r\nContent-Disposition: form-data; name="version"\r\n\r\n{version}\r\n'
                f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="document.bin"\r\n'
                'Content-Type: application/octet-stream\r\n\r\n'
            ).encode() + content + f"\r\n--{boundary}--\r\n".encode()
            uploaded = client.request("POST", file_path, (200,), encoded_body=body,
                                      content_type=f"multipart/form-data; boundary={boundary}").payload
            version = uploaded["version"]
            revision = client.request("GET", f"{path}/revisions/{revision_id}", (200,)).payload
            key = revision["file_key"]
            digest = hashlib.sha256(content).hexdigest()
            require(revision["content_sha256"] == digest and int(revision["file_size"]) == len(content),
                    "revision metadata does not match uploaded bytes")
            require(client.request("GET", file_path, (200,), raw_response=True).payload == content,
                    "Gateway download bytes differ")
            require(object_bytes(bucket, key) == content, "physical bucket bytes differ")
            absent("standard", key)
            if prior_key:
                require(key != prior_key, "replacement reused key")
                absent(bucket, prior_key)
            report["observations"].append({"key": key, "sha256": digest, "bytes": len(content), "version": version})
            prior_key = key
        denied = foreign.request("GET", file_path, (404,))
        foreign.request("GET", f"{path}/revisions/{revision_id}", (404,))
        require(denied.status == 404, "cross-tenant file read was not refused")
        report["cross_tenant_http_status"] = denied.status
    finally:
        client.request("DELETE", path, (200,))
        client.request("GET", path, (404,))
    absent(bucket, prior_key)
    report["cleanup"] = "passed"
    report["schema_version"] = "addp.online-suite/v1"
    print(json.dumps(report))


if __name__ == "__main__":
    main()

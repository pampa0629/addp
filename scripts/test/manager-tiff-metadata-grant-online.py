"""One NFS TIFF through the formal Meta and System authorization APIs."""
import importlib
import json
import re
import time
from datetime import datetime
from pathlib import Path
from urllib.parse import urlencode


CASE = "nfs-tiff-metadata-grant"
SOURCE = "source.tif"
REGISTRATION = importlib.import_module("scripts.test.online-engine-registration")


def run(environment, owner):
    if environment.get("ADDP_ONLINE_MANAGER_CASE") != CASE:
        raise owner.SuiteError("requires the isolated TIFF metadata case")
    importlib.import_module("scripts.test.orchestrator-execution-faults").deployment_secret()
    run_id = environment["ADDP_ONLINE_TEST_RUN_ID"]
    if not re.fullmatch(r"manager-[0-9]+-[0-9]+", run_id):
        raise owner.SuiteError("requires the Hosted Manager run identity")

    tenant = owner.positive_int(environment["ADDP_ONLINE_TEST_TENANT_ID"], "TIFF tenant")
    engine = owner.positive_int(environment["ADDP_ONLINE_MANAGER_NFS_ENGINE_ID"], "TIFF engine")
    gateway = environment["GATEWAY_URL"]
    consumer = owner.GatewayClient(gateway, environment["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], 10)
    issuer = owner.GatewayClient(gateway, environment["ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"], 10)
    artifact = Path(environment["ADDP_ONLINE_ARTIFACT_DIR"]) / f"{CASE}.json"
    route = "/api/v1/meta/items/by-catalog-path?" + urlencode({"engine_id": engine, "catalog_path": SOURCE})
    report = {
        "schema_version": "addp.nfs-tiff-metadata-grant/v1", "run_id": run_id, "engine_id": engine,
        "result": "failed", "phase": "identity", "elapsed_ms": {}, "pending": [], "cleanup": "pending",
    }

    def save():
        temporary = artifact.with_suffix(".tmp")
        temporary.write_text(json.dumps(report, sort_keys=True) + "\n")
        temporary.replace(artifact)

    def checkpoint(pending):
        report["pending"] = list(pending)
        save()

    def record_revocation(row, response, visibility):
        if visibility is None:
            receipt = None
            if response.status == 200:
                receipt = owner._object(response.payload, "TIFF revocation receipt")
                expected = {
                    "request_id": row["request_id"], "revoked_request_ids": [row["request_id"]],
                    "revoked_by_principal_id": grants.operator, "reason": "Disposable Online metadata cleanup",
                }
                membership = receipt.get("revoked_by_membership_id")
                revoked_at = datetime.fromisoformat(receipt["revoked_at"])
                if (any(receipt.get(key) != value for key, value in expected.items())
                        or not isinstance(membership, str) or not re.fullmatch(r"[1-9][0-9]*", membership)
                        or revoked_at.tzinfo is None):
                    raise owner.SuiteError("TIFF revocation receipt differs from the issued Grant")
                receipt = {key: receipt[key] for key in (*expected, "revoked_at", "revoked_by_membership_id")}
            report["revocation"] = {"http_status": response.status, "receipt": receipt}
            save()
            return
        proof = report["revocation"]
        receipt = proof["receipt"]
        observed_at = datetime.fromisoformat(visibility["observed_at"])
        if observed_at.tzinfo is None or (receipt and observed_at < datetime.fromisoformat(receipt["revoked_at"])):
            raise owner.SuiteError("TIFF hidden observation must follow revocation")
        proof["visibility"] = visibility
        proof["meta_status"] = consumer.request("GET", route, (404,)).status
        save()

    grants = None
    started = time.monotonic()
    passed = False
    try:
        identity = owner.validate_user_identity(consumer, tenant, required_permissions={"meta.catalog.read"})
        report["identity"] = identity
        grants = REGISTRATION.ExactFileMetadataGrants(
            issuer, consumer, tenant, scan_preparation=True, on_change=checkpoint,
        )
        if grants.recipient != identity["principal_id"]:
            raise owner.SuiteError("TIFF Grant recipient differs from the observing User")
        report["issuer_principal_id"] = grants.operator
        report["phase"] = "scan"
        scan_started = time.monotonic()
        report["scan_execution_id"] = owner.wait_for_meta_scan(
            issuer, engine, scan_started + 180,
            scope={"targets": [f"addp://engine/{engine}/path/{SOURCE}?type=file"]},
        )
        report["elapsed_ms"]["scan"] = round((time.monotonic() - scan_started) * 1000)
        report["phase"] = "grant"
        grant_started = time.monotonic()
        report["before_grant_status"] = consumer.request("GET", route, (404,)).status
        receipt = grants.grant(engine, SOURCE, "nfs", data_type="media", format_name="tiff", layout="single")
        report["grant"] = {key: receipt[key] for key in (
            "request_id", "item_fingerprint", "boundary_hash", "file_composition", "recipient_id", "action",
        )}
        composition = receipt["file_composition"]
        if not composition or len(composition["refs"]) != 1:
            raise owner.SuiteError("TIFF requires its formal single-file composition")
        report["elapsed_ms"]["grant"] = round((time.monotonic() - grant_started) * 1000)
        report["phase"] = "visible_metadata"
        read_started = time.monotonic()
        response = consumer.request("GET", route, (200,))
        item = owner._object(response.payload, "TIFF Meta item")
        attrs = owner._object(item.get("attributes"), "TIFF structural attributes")
        type_info = owner._object(attrs.get("type_info", {}), "TIFF structural type info")
        capabilities = owner._object(attrs.get("capabilities", {}), "TIFF structural capabilities")
        spatial = owner._object(capabilities.get("spatial", {}), "TIFF structural spatial declaration")
        if (item.get("tenant_id") != tenant or item.get("engine_id") != engine
                or item.get("full_name") != SOURCE or item.get("item_type") != "file"
                or item.get("fingerprint") != receipt["item_fingerprint"]
                or attrs.get("schema_version") != 1
                or attrs.get("item") != {"data_type": "media", "format": "tiff", "layout": "single"}
                or set(attrs) - {"schema_version", "item", "type_info", "capabilities"}
                or type_info.get("media") or spatial.get("extent") is not None
                or any(item.get(key) is not None for key in ("size_bytes", "row_count", "data_updated_at"))):
            raise owner.SuiteError("TIFF public Meta projection differs from the exact structural Grant")
        report["visible_metadata"] = {"http_status": response.status, "item_id": owner.positive_int(item.get("id"), "TIFF item id")}
        report["elapsed_ms"]["visible_metadata"] = round((time.monotonic() - read_started) * 1000)
        passed = True
    except REGISTRATION.RegistrationError as error:
        report["grant_error"] = REGISTRATION.metadata_error_evidence(error, report["phase"])
        raise owner.SuiteError(str(error)) from error
    finally:
        cleanup_started = time.monotonic()
        try:
            if grants is not None and grants.pending:
                report["cleanup"] = grants.cleanup(on_revocation=record_revocation)
            else:
                report["cleanup"] = "no_grant_created"
            if passed:
                report["phase"] = "complete"
                report["result"] = "passed"
        except Exception as error:
            report["cleanup"] = {"result": "failed", "error_type": type(error).__name__}
            if passed:
                report["phase"] = "revocation"
                raise owner.SuiteError("TIFF revocation evidence or cleanup failed") from error
        finally:
            report["elapsed_ms"]["revocation"] = round((time.monotonic() - cleanup_started) * 1000)
            report["elapsed_ms"]["total"] = round((time.monotonic() - started) * 1000)
            save()
    return report

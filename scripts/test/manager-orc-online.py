"""Ordinary-user ORC scan, page, download and real Transfer batch acceptance."""

from __future__ import annotations
import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import time
from urllib.parse import urlencode, quote

PERMISSIONS = {
    "manager.content.read",
    "manager.data_item.read",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "monitor.execution.read",
    "transfer.task.create",
    "transfer.task.read",
    "transfer.task.execute",
    "transfer.task.delete",
    "system.execution_authorization.create",
}
COUNT = 2503


def require(value, message):
    if not value:
        raise ValueError(message)


def wait_execution(client, execution_id):
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        execution = client.request(
            "GET", "/api/v1/transfer/executions/" + quote(str(execution_id)), (200,)
        ).payload
        status = execution.get("status")
        if status == "success":
            require(
                execution.get("records_read") == COUNT
                and execution.get("records_written") == COUNT,
                "ORC Transfer counts differ from complete source",
            )
            return {
                "execution_id": execution_id,
                "status": status,
                "records_read": COUNT,
                "records_written": COUNT,
            }
        require(
            status not in {"failed", "cancelled", "timeout"},
            "ORC Transfer execution failed",
        )
        time.sleep(1)
    raise ValueError("ORC Transfer execution did not converge")


def task_body(source, parent, name):
    return {
        "name": "online-orc-" + name,
        "task_type": "sync",
        "enabled": False,
        "auto_scan_metadata": False,
        "batch_size": 1000,
        "config": {
            "runtime": {"boundary": "bounded"},
            "load": {"mode": "snapshot"},
            "source": {
                "locator": source,
                "data_type": "table",
                "representation": "encoded",
                "format": "orc",
            },
            "target": {
                "parent_locator": parent,
                "name": name,
                "data_type": "table",
                "representation": "encoded",
                "format": "parquet",
                "policy": {"apply_mode": "replace"},
            },
            "batch_size": 1000,
        },
    }


def nested_public_row(index):
    number = str(2**53 + index)
    profile = (
        None
        if index % 3 == 0
        else (
            {"counter": None, "amount": None, "blob": None}
            if index % 5 == 0
            else {
                "counter": number,
                "amount": f"12345678901234567890.{index:09d}",
                "blob": "" if index % 2 == 0 else "AP8=",
            }
        )
    )
    samples = (
        None
        if index % 7 == 0
        else (
            []
            if index % 5 == 0
            else [{"value": number, "label": None}, {"value": None, "label": "嵌套"}]
        )
    )
    matrix = (
        None
        if index % 7 == 0
        else ([] if index % 5 == 0 else [[number, None], [], None])
    )
    return {"id": number, "profile": profile, "samples": samples, "matrix": matrix}


def check_nested(client, owner, engine, full_name, name, seed, artifact):
    item = owner.find_fixture_item(client, engine, full_name, name + " nested ORC")
    detail = client.request("GET", f"/api/v1/meta/items/{item['id']}", (200,)).payload
    attrs = detail.get("attributes", {})
    binding = attrs.get("format_info", {}).get("orc", {})
    require(
        attrs.get("item", {}).get("format") == "orc"
        and attrs.get("item", {}).get("layout") == "single"
        and binding.get("source_version")
        and binding.get("validation") == "footer",
        "nested ORC source binding differs",
    )
    info = attrs.get("type_info", {}).get("table", {})
    fields = info.get("fields", [])
    require(
        info.get("row_count") == 43
        and [f["name"] for f in fields] == ["id", "profile", "samples", "matrix"],
        "nested ORC top-level facts differ",
    )
    profile, samples, matrix = fields[1:]
    require(
        profile["type"] == "json"
        and [(f["name"], f["type"]) for f in profile["fields"]]
        == [("counter", "bigint"), ("amount", "decimal"), ("blob", "bytes")]
        and profile["fields"][1]["precision"] == 38
        and profile["fields"][1]["scale"] == 9
        and samples["element_type"] == "json"
        and samples["element"]["type"] == "json"
        and [(f["name"], f["type"]) for f in samples["element"]["fields"]]
        == [("value", "bigint"), ("label", "string")]
        and matrix["element_type"] == "array"
        and matrix["element"]["element_type"] == "bigint"
        and matrix["element"]["element"]["type"] == "bigint",
        "nested ORC declared type tree differs",
    )
    locator = owner.build_item_locator(engine, item)
    pages = []
    for page in (1, 2, 3):
        preview = client.request(
            "GET",
            "/api/v1/manager/preview?"
            + urlencode({"locator": locator, "page": page, "page_size": 20}),
            (200,),
        ).payload
        table = preview.get("data", {})
        start = (page - 1) * 20
        require(
            preview.get("preview_type") == "table"
            and table.get("total") == 43
            and table.get("has_more") is (page != 3)
            and table.get("rows")
            == [nested_public_row(i) for i in range(start, min(start + 20, 43))],
            "nested ORC page lost structure or precision",
        )
        pages.append(
            {"page": page, "rows": len(table["rows"]), "values_verified": True}
        )
    downloaded = client.request(
        "GET",
        "/api/v1/manager/downloads/file?" + urlencode({"locator": locator}),
        (200,),
    ).raw
    require(
        len(downloaded) == seed["size_bytes"]
        and hashlib.sha256(downloaded).hexdigest() == seed["sha256"],
        "nested ORC original download differs",
    )
    (artifact / f"orc-nested-source-{name}.orc").write_bytes(downloaded)
    (artifact / f"orc-nested-meta-{name}.json").write_text(
        json.dumps(detail, ensure_ascii=False, indent=2)
    )
    require(
        client.request("GET", f"/api/v1/meta/items/{item['id']}", (200,)).payload
        == detail,
        "nested ORC preview modified metadata",
    )
    return {
        "source_case": name,
        "locator": locator,
        "fields_verified": True,
        "pages": pages,
        "original_download_verified": True,
        "sha256": seed["sha256"],
        "size_bytes": seed["size_bytes"],
    }


def run(repository, environment, owner, *, physical_runner=None, browser_runner=None):
    require(
        environment.get("ADDP_ONLINE_MANAGER_CASE") == "native-table-orc",
        "ORC requires its explicit Hosted case",
    )
    artifact = Path(environment["ADDP_ONLINE_ARTIFACT_DIR"])
    report = {
        "schema_version": "addp.manager-orc/v1",
        "case": "native-table-orc",
        "run_id": environment["ADDP_ONLINE_TEST_RUN_ID"],
        "result": "incomplete",
        "cases": [],
        "nested_cases": [],
        "cleanup": {},
    }
    spec = importlib.util.spec_from_file_location(
        "orc_fixture", Path(repository) / "business/scripts/online-orc-fixture.py"
    )
    fixture = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(fixture)
    physical = physical_runner or (
        lambda action: fixture.physical(action, repository, environment)
    )
    client = owner.GatewayClient(
        environment["GATEWAY_URL"],
        environment["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"],
        30,
    )
    tenant = owner.positive_int(environment["ADDP_ONLINE_TEST_TENANT_ID"], "ORC tenant")
    report["identity"] = owner.validate_user_identity(client, tenant, PERMISSIONS)
    minio = owner.positive_int(
        environment["ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID"], "ORC MinIO"
    )
    nfs = owner.positive_int(
        environment["ADDP_ONLINE_MANAGER_NFS_ENGINE_ID"], "ORC NFS"
    )
    require(minio != nfs, "ORC sources must be separate Engines")
    tasks = []
    cleanup = []
    seeded = False
    primary_failed = False
    stage = "physical_seed"
    try:
        seeded = True
        seed = physical("seed")
        require(
            seed.get("count") == COUNT and seed.get("pyorc") == "0.12.0",
            "ORC physical fixture differs from fixed profile",
        )
        report["source"] = seed
        bucket = environment["ADDP_ONLINE_MANAGER_MINIO_BUCKET"]
        for name, engine, scope, full_name in [
            ("minio", minio, bucket + "/orc", bucket + "/orc/table.orc"),
            ("nfs", nfs, "orc", "orc/table.orc"),
        ]:
            stage = "metadata_scan"
            owner.wait_for_meta_scan(
                client, engine, time.monotonic() + 180, scope={"catalog_paths": [scope]}
            )
            item = owner.find_fixture_item(client, engine, full_name, name + " ORC")
            detail = client.request(
                "GET", f"/api/v1/meta/items/{item['id']}", (200,)
            ).payload
            attrs = detail.get("attributes", {})
            require(
                {
                    k: attrs.get("item", {}).get(k)
                    for k in ["data_type", "format", "layout"]
                }
                == {"data_type": "table", "format": "orc", "layout": "single"},
                "ORC item identity changed",
            )
            info = attrs.get("type_info", {}).get("table", {})
            fields = info.get("fields", [])
            require(
                info.get("row_count") == COUNT
                and [(f["name"], f["type"]) for f in fields]
                == [
                    ("id", "bigint"),
                    ("measure", "decimal"),
                    ("flag", "bool"),
                    ("label", "string"),
                    ("blob", "bytes"),
                ],
                "ORC schema or row count changed",
            )
            require(
                fields[1].get("precision") == 38
                and fields[1].get("scale") == 9
                and all(f.get("nullable") is True for f in fields),
                "ORC field precision or nullability changed",
            )
            private = attrs.get("format_info", {}).get("orc", {})
            require(
                private.get("source_version") and private.get("validation") == "footer",
                "ORC source binding is absent",
            )
            locator = owner.build_item_locator(engine, item)
            case = {
                "source_case": name,
                "locator": locator,
                "item_id": item["id"],
                "source_version": private["source_version"],
                "fields_verified": True,
                "pages": [],
            }
            report["cases"].append(case)
            (artifact / f"orc-meta-{name}.json").write_text(
                json.dumps(detail, ensure_ascii=False, indent=2)
            )
            stage = "paged_preview"
            for page in [1, 2, 126]:
                preview = client.request(
                    "GET",
                    "/api/v1/manager/preview?"
                    + urlencode({"locator": locator, "page": page, "page_size": 20}),
                    (200,),
                ).payload
                require(
                    preview.get("preview_type") == "table",
                    "ORC must use real table preview",
                )
                table = preview.get("data", {})
                rows = table.get("rows", [])
                start = (page - 1) * 20
                expected = min(20, COUNT - start)
                require(
                    table.get("total") == COUNT
                    and len(rows) == expected
                    and table.get("has_more") is (page != 126),
                    "ORC page count, total or continuation differs",
                )
                for i, row in enumerate(rows, start):
                    require(
                        row["id"] == str(2**53 + i)
                        and row["measure"] == f"12345678901234567890.{i:09d}"
                        and row["label"] == f"ORC记录-{i:04d}"
                        and row["flag"] == (None if i % 7 == 0 else i % 2 == 0),
                        "ORC page lost exact values or nulls",
                    )
                case["pages"].append(
                    {"page": page, "rows": len(rows), "values_verified": True}
                )
            stage = "original_download"
            downloaded = client.request(
                "GET",
                "/api/v1/manager/downloads/file?" + urlencode({"locator": locator}),
                (200,),
            )
            require(
                len(downloaded.raw) == seed["size_bytes"]
                and hashlib.sha256(downloaded.raw).hexdigest() == seed["sha256"],
                "ORC original download differs from physical source",
            )
            (artifact / f"orc-source-{name}.orc").write_bytes(downloaded.raw)
            case["original_download_verified"] = True
            stage = "transfer_batch"
            target = "from-" + name + ".parquet"
            parent = f"addp://engine/{minio}/path/{bucket}/orc-output?type=prefix"
            created = client.request(
                "POST",
                "/api/v1/transfer/task-definitions",
                (201,),
                task_body(locator, parent, target),
            ).payload
            task_id = owner.positive_int(created.get("id"), "ORC Transfer task")
            tasks.append(task_id)
            started = client.request(
                "POST",
                f"/api/v1/transfer/task-definitions/{task_id}/start",
                (200, 201),
                {},
            ).payload
            execution_id = started.get("execution_id")
            require(
                isinstance(execution_id, str) and execution_id,
                "ORC Transfer execution identity missing",
            )
            case["transfer"] = wait_execution(client, execution_id)
            require(
                client.request(
                    "GET", f"/api/v1/meta/items/{item['id']}", (200,)
                ).payload
                == detail,
                "ORC read modified source metadata",
            )
        stage = "nested_preview"
        require(seed.get("nested", {}).get("count") == 43, "nested ORC fixture absent")
        for name, engine, full_name in [
            ("minio", minio, bucket + "/orc/nested.orc"),
            ("nfs", nfs, "orc/nested.orc"),
        ]:
            report["nested_cases"].append(
                check_nested(
                    client, owner, engine, full_name, name, seed["nested"], artifact
                )
            )
        stage = "physical_verification"
        verified = physical("verify")
        require(
            len(verified.get("outputs", [])) == 2
            and all(
                x.get("all_values_and_nulls_verified") is True
                for x in verified["outputs"]
            )
            and verified.get("source_sha256") == seed["sha256"]
            and verified.get("nested_source_sha256") == seed["nested"]["sha256"]
            and verified.get("nested_all_values_verified") is True,
            "ORC physical batch output or source changed",
        )
        require(
            {x.get("object") for x in verified["outputs"]}
            == {"orc-output/from-minio.parquet", "orc-output/from-nfs.parquet"},
            "ORC output identities differ",
        )
        for output in verified["outputs"]:
            raw = base64.b64decode(output.pop("content_base64"), validate=True)
            require(
                0 < len(raw) <= 2 * 1024 * 1024
                and len(raw) == output["size_bytes"]
                and hashlib.sha256(raw).hexdigest() == output["sha256"],
                "ORC archived output differs from physical oracle",
            )
            filename = "orc-result-" + Path(output["object"]).name
            (artifact / filename).write_bytes(raw)
            output["archive_file"] = filename
        report["physical"] = verified
        browser_context = {
            "run_id": report["run_id"],
            "identity": report["identity"],
            "cases": report["cases"],
            "nested_cases": report["nested_cases"],
        }
        stage = "browser"
        if browser_runner:
            browser = browser_runner(browser_context)
        else:
            env = dict(
                environment, ADDP_ONLINE_MANAGER_ORC_JSON=json.dumps(browser_context)
            )
            subprocess.run(
                [
                    "npm",
                    "exec",
                    "--",
                    "playwright",
                    "test",
                    "e2e/online/manager-orc.spec.js",
                    "--config=playwright.online.config.js",
                ],
                cwd=Path(repository) / "console/frontend",
                env=env,
                check=True,
            )
            browser = json.loads((artifact / "manager-orc-browser.json").read_text())
        require(
            browser.get("result") == "passed"
            and browser.get("run_id") == report["run_id"]
            and len(browser.get("cases", [])) == 2
            and all(
                c.get("page_button_verified") is True
                and c.get("original_button_download_verified") is True
                and c.get("sha256") == seed["sha256"]
                and c.get("size_bytes") == seed["size_bytes"]
                for c in browser["cases"]
            ),
            "ORC real browser acceptance incomplete",
        )
        require(
            len(browser.get("nested_cases", [])) == 2
            and all(
                c.get("page_button_verified") is True
                and c.get("nested_values_verified") is True
                and c.get("original_button_download_verified") is True
                and c.get("sha256") == seed["nested"]["sha256"]
                and c.get("size_bytes") == seed["nested"]["size_bytes"]
                for c in browser["nested_cases"]
            ),
            "nested ORC browser acceptance incomplete",
        )
        report["browser"] = browser
        report["result"] = "passed"
    except BaseException as error:
        primary_failed = True
        report["result"] = "failed"
        report["failure_stage"] = stage
        report["error_type"] = type(error).__name__
        raise
    finally:
        for task in reversed(tasks):
            try:
                client.request(
                    "DELETE", f"/api/v1/transfer/task-definitions/{task}", (200, 204)
                )
                client.request(
                    "GET", f"/api/v1/transfer/task-definitions/{task}", (404,)
                )
            except Exception:
                cleanup.append("owned_task_delete_failed")
        if seeded:
            try:
                report["cleanup"].update(physical("delete"))
            except Exception:
                cleanup.append("owned_output_delete_failed")
        report["cleanup"].update(
            tasks_deleted=len(tasks) if not cleanup else None,
            errors=cleanup,
            result="passed" if not cleanup else "failed",
            deployment_and_infra="verified separately by Hosted lifecycle",
        )
        if cleanup:
            report["result"] = "failed"
            if not primary_failed:
                report["failure_stage"] = "cleanup"
        (artifact / "manager-orc.json").write_text(
            json.dumps(report, ensure_ascii=False, indent=2) + "\n"
        )
        if cleanup and not primary_failed:
            raise ValueError("ORC owned-resource cleanup failed")
    return report

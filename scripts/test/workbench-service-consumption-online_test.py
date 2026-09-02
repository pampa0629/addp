import importlib.util
import json
import sys
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("workbench-service-consumption-online.py")
SPEC = importlib.util.spec_from_file_location("workbench_service_consumption_online", SCRIPT)
SUITE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = SUITE
SPEC.loader.exec_module(SUITE)


def response(status, payload=None, *, headers=None, raw=b""):
    return SUITE.Response(status, payload if payload is not None else {}, headers or {}, raw)


class FakeClient:
    def __init__(self) -> None:
        self.service_exists = False
        self.application_exists = False
        self.application_status = "unpublished"
        self.application_version = 1
        self.application_snapshot = None
        self.contract_changed = False
        self.fail_query = False
        self.calls: list[tuple[str, str]] = []

    @staticmethod
    def contract(*, published=False):
        types = {
            "order_no": "string",
            "customer_code": "string",
            "city": "string",
            "membership_level": "string",
            "status": "string",
            "total_amount": "decimal",
            "payment_method": "string",
            "ordered_at": "timestamp",
            "shipped_at": "timestamp",
            "active_customer": "bool" if published else "int",
        }
        return {
            "table": {
                "fields": [
                    {"name": name, "type": types[name], "nullable": name in {"city", "payment_method", "shipped_at"}}
                    for name in SUITE.FIELDS
                ]
            }
        }

    def descriptor(self):
        fields = []
        for field in self.contract(published=True)["table"]["fields"]:
            name = field["name"]
            fields.append(
                {
                    **field,
                    "selectable": True,
                    "filterable": name in SUITE.FILTERABLE_FIELDS,
                    "operators": ["eq", "in", "gte"] if name in SUITE.FILTERABLE_FIELDS else [],
                    "sortable": name == "order_no",
                }
            )
        fingerprint = "sha256:" + ("b" if self.contract_changed else "a") * 64
        return {
            "schema_version": "addp.service_consumer/v1",
            "ref": {"service_type": "query", "service_id": 23},
            "title": "Commerce order analysis",
            "description": "fixture",
            "status": "active",
            "access_mode": "private",
            "contract_fingerprint": fingerprint,
            "operations": [
                {
                    "key": "query",
                    "method": "POST",
                    "path": f"/api/query/{SUITE.SERVICE_NAME}/query",
                    "input_kind": "structured_query",
                    "output_kind": "tabular",
                }
            ],
            "input_contract": {
                "kind": "structured_query",
                "fields": fields,
                "default_selection": SUITE.FIELDS[:-1] if self.contract_changed else SUITE.FIELDS,
                "filter": {"combinators": ["and", "or", "not"], "max_depth": 16, "max_nodes": 256, "max_in_values": 1000},
                "order": {"directions": ["asc", "desc"], "stable_key": ["order_no"]},
                "page": {"kind": "cursor", "default_limit": 50, "max_limit": 100},
                "formats": ["json", "csv"],
                "intent": {"header": "X-ADDP-Query-Intent", "allowed_values": ["query", "export"], "default_value": "query"},
            },
            "output_contract": {"kind": "tabular", "fields": self.contract(published=True)["table"]["fields"]},
        }

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path))
        if path == "/api/v1/system/auth/context":
            return response(
                200,
                {
                    "principal": {"type": "user", "id": "51"},
                    "context": {"type": "tenant", "tenant_id": "42"},
                    "token": {"type": "first_party_access_token"},
                    "authorization": {
                        "role_assignments": [
                            {"role_key": "tenant.workbench_operator", "permissions": sorted(SUITE.REQUIRED_PERMISSIONS)}
                        ]
                    },
                },
            )
        if path.startswith("/api/v1/service/query?search="):
            return response(200, {"data": [], "total": 0})
        if path == "/api/v1/service/sql/output-contract":
            return response(200, self.contract())
        if path == "/api/v1/service/query" and method == "POST":
            self.service_exists = True
            return response(201, {"id": 23, "tenant_id": 42, "service_name": SUITE.SERVICE_NAME})
        if path == "/api/v1/service/consumer/services/query/23":
            return response(200, self.descriptor())
        if path == "/api/v1/workbench/data_applications" and method == "POST":
            self.application_exists = True
            self.application_snapshot = json.loads(json.dumps(body["snapshot"]))
            for component in self.application_snapshot["components"]:
                component["contract_fingerprint"] = "sha256:" + "a" * 64
            return response(
                201,
                {
                    "id": "10000000-0000-0000-0000-000000000001",
                    "tenant_id": 42,
                    "version": self.application_version,
                    "publication_status": self.application_status,
                    "snapshot": self.application_snapshot,
                },
            )
        application_path = "/api/v1/workbench/data_applications/10000000-0000-0000-0000-000000000001"
        if path == "/api/v1/service/query/23" and method == "PUT":
            self.contract_changed = True
            return response(200, {"id": 23})
        if path == application_path:
            if method == "DELETE":
                self.application_exists = False
                return response(200)
            if self.application_exists:
                return response(200, {
                    "id": "10000000-0000-0000-0000-000000000001",
                    "tenant_id": 42,
                    "version": self.application_version,
                    "publication_status": self.application_status,
                    "snapshot": self.application_snapshot,
                })
            return response(404)
        if path == "/api/v1/service/query/23":
            if method == "DELETE":
                self.service_exists = False
                return response(200)
            return response(200 if self.service_exists else 404, {"id": 23} if self.service_exists else {})
        raise AssertionError(f"unexpected request {method} {path} body={body!r}")

    @staticmethod
    def row(number, city, status, amount, shipped):
        return {
            "order_no": number,
            "customer_code": "CUST-" + number[-3:],
            "city": city,
            "membership_level": "gold",
            "status": status,
            "total_amount": amount,
            "payment_method": "alipay",
            "ordered_at": "2026-04-20T10:12:00Z",
            "shipped_at": shipped,
            "active_customer": True,
        }

    def query(self, body, *, intent="query"):
        self.calls.append(("QUERY", intent))
        if self.fail_query:
            raise SUITE.SuiteError("injected query failure")
        if body["format"] == "csv":
            lines = [",".join(SUITE.FIELDS)]
            for index in range(1, 5):
                lines.append(
                    f"ORD-{index},CUST-{index},city,gold,delivered,{index}.00,alipay,2026-04-20T10:12:00Z,,true"
                )
            return response(
                200,
                headers={"Content-Type": "text/csv; charset=utf-8", "X-ADDP-Has-More": "false"},
                raw=("\n".join(lines) + "\n").encode(),
            )
        if body["page"].get("cursor"):
            rows = [
                self.row("ORD-3", "深圳", "paid", "798.00", None),
                self.row("ORD-4", "成都", "delivered", 11499.0, "2026-04-24T11:40:00Z"),
            ]
            return response(200, {"data": rows, "page": {"limit": 2, "has_more": False}, "service_version": "v1"})
        rows = [
            self.row("ORD-1", "上海", "delivered", "2897.00", "2026-04-21T15:30:00Z"),
            self.row("ORD-2", "北京", "processing", 16999.0, None),
        ]
        return response(
            200,
            {"data": rows, "page": {"limit": 2, "has_more": True, "next_cursor": "cursor-2"}, "service_version": "v1"},
        )


class WorkbenchServiceConsumptionOnlineTest(unittest.TestCase):
    def test_accepts_mysql_cursor_export_application_and_contract_change(self) -> None:
        client = FakeClient()

        report = SUITE.run_suite(client, 42, 9, "run-1")

        self.assertEqual(report["source_engine"], "mysql")
        self.assertEqual(report["cursor"]["rows"], 4)
        self.assertEqual(report["export"]["rows"], 4)
        self.assertTrue(report["contract_change_blocked"])
        self.assertEqual(report["renderers"], {"table": True, "chart": True, "map": False})
        self.assertEqual(report["residual_resources"], 0)
        self.assertFalse(client.application_exists)
        self.assertFalse(client.service_exists)

    def test_failure_still_deletes_created_application_and_service(self) -> None:
        client = FakeClient()
        client.fail_query = True

        with self.assertRaisesRegex(SUITE.SuiteError, "injected query failure"):
            SUITE.run_suite(client, 42, 9, "run-1")

        self.assertFalse(client.application_exists)
        self.assertFalse(client.service_exists)

    def test_rejects_administrator_identity(self) -> None:
        client = FakeClient()
        original = client.request

        def request(method, path, expected, body=None):
            result = original(method, path, expected, body)
            if path == "/api/v1/system/auth/context":
                result.payload["authorization"]["role_assignments"][0]["role_key"] = "tenant.administrator"
            return result

        client.request = request
        with self.assertRaisesRegex(SUITE.SuiteError, "administrator roles"):
            SUITE.validate_user_identity(client, 42)

    def test_validates_browser_report_contract(self) -> None:
        report = {
            "schema_version": "addp.workbench-service-consumption-browser/v1",
            "suite": "workbench-service-consumption",
            "run_id": "run-1",
            "result": "passed",
            "tenant_id": "42",
            "service_id": 23,
            "application_id": "10000000-0000-0000-0000-000000000001",
            "table_rows": 2,
            "chart_rendered": True,
            "map_available": False,
            "contract_change_blocked": True,
        }

        self.assertEqual(
            SUITE.validate_browser_report(
                report,
                "run-1",
                "42",
                23,
                "10000000-0000-0000-0000-000000000001",
            ),
            report,
        )

        report["chart_rendered"] = False
        with self.assertRaisesRegex(SUITE.SuiteError, "chart_rendered"):
            SUITE.validate_browser_report(
                report,
                "run-1",
                "42",
                23,
                "10000000-0000-0000-0000-000000000001",
            )


if __name__ == "__main__":
    unittest.main()

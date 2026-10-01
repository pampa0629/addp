import copy
import importlib.util
import sys
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("enterprise-catalog-publishing-online.py")
SPEC = importlib.util.spec_from_file_location("enterprise_catalog_publishing_online", SCRIPT)
SUITE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = SUITE
SPEC.loader.exec_module(SUITE)


class FakeClient:
    def __init__(self) -> None:
        self.entry = {
            "id": "10000000-0000-0000-0000-000000000001",
            "version": 4,
            "business_name": "Stable fixture",
            "business_description": "Stable description",
            "governance_status": "curated",
            "visibility": "tenant",
            "source": {"source_identity": "fingerprint-1", "source_status": "active"},
            "semantic_links": [{"semantic_type": "domain", "semantic_id": "31", "relation_role": "primary"}],
            "responsibilities": [
                {"role": "accountable_department", "subject_type": "department", "subject_id": "41"},
                {"role": "business_owner", "subject_type": "user", "subject_id": "51"},
                {"role": "data_steward", "subject_type": "user", "subject_id": "51"},
            ],
        }
        self.asset_exists = False
        self.asset_status = ""
        self.catalog_exists = False
        self.category_name = ""
        self.calls: list[tuple[str, str]] = []
        self.writes: list[tuple[str, dict]] = []
        self.execution_count = 0

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path))
        if body is not None:
            self.writes.append((path, copy.deepcopy(body)))
        if path == "/api/v1/meta/scan/run/manual":
            self.execution_count += 1
            return SUITE.Response(201, {"execution_id": f"execution-{self.execution_count}", "status": "pending"})
        if path.startswith("/api/v1/meta/executions/execution-"):
            return SUITE.Response(200, {"execution_id": path.rsplit("/", 1)[-1], "status": "success"})
        if path == "/api/v1/meta/engines/7/items":
            return SUITE.Response(200, [{"id": 9, "name": SUITE.FIXTURE_TABLE, "fingerprint": "fingerprint-1"}])
        if path.startswith("/api/v1/catalog/entries?view=inventory&source_identity="):
            return SUITE.Response(200, {"data": [{"id": self.entry["id"]}], "total": 1})
        if path.startswith("/api/v1/catalog/entries?view=governance&source_identity="):
            data = [{"id": self.entry["id"]}] if self.entry["governance_status"] != "discovered" else []
            return SUITE.Response(200, {"data": data, "total": len(data)})
        if path == "/api/v1/catalog/entries/resolve-sources" and method == "POST":
            return SUITE.Response(
                200,
                {
                    "results": [
                        {
                            "source_module": "meta",
                            "source_type": "data_item",
                            "source_identity": "fingerprint-1",
                            "found": True,
                            "entry": {
                                "id": self.entry["id"],
                                "display_name": self.entry.get("business_name") or "fixture",
                                "source_status": "active",
                                "source_identity": "fingerprint-1",
                            },
                        }
                    ]
                },
            )
        if path == "/api/v1/catalog/governance/coverage":
            status = self.entry["governance_status"]
            statuses = [
                {"status": key, "count": 1 if key == status else 0}
                for key in ("discovered", "curated", "certified", "deprecated")
            ]
            business_covered = int(bool(self.entry.get("business_name") and self.entry.get("business_description")))
            primary_domain_covered = int(bool(self.entry.get("semantic_links")))
            responsibility_roles = {
                item.get("role")
                for item in self.entry.get("responsibilities") or []
                if isinstance(item, dict)
            }
            dimensions = [
                self._coverage_dimension("business_definition", business_covered, 1),
                self._coverage_dimension("primary_domain", primary_domain_covered, 1),
                self._coverage_dimension("accountable_department", int("accountable_department" in responsibility_roles), 1),
                self._coverage_dimension("business_owner", int("business_owner" in responsibility_roles), 1),
                self._coverage_dimension("data_steward", int("data_steward" in responsibility_roles), 1),
                self._coverage_dimension("glossary", 0, 1),
                self._coverage_dimension("component_standard_mapping", 0, 0),
            ]
            return SUITE.Response(
                200,
                {"view": "inventory", "total_entries": 1, "governance_statuses": statuses, "dimensions": dimensions},
            )
        entry_path = f"/api/v1/catalog/entries/{self.entry['id']}"
        if path in {entry_path + "/governance", entry_path + "/responsibilities"} and method == "PUT":
            if body["version"] != self.entry["version"]:
                return SUITE.Response(409, {"error_code": "catalog_entry_version_conflict"})
            if path.endswith("/governance"):
                if set(body) != {"version", "governance_status", "reason"}:
                    raise AssertionError("unexpected governance fields")
                self.entry["governance_status"] = body["governance_status"]
                self.entry["recommended_successor_entry_id"] = None
            else:
                if set(body) != {"version", "reason", "responsibilities"}:
                    raise AssertionError("unexpected responsibility transfer fields")
                if self.entry["governance_status"] != "deprecated":
                    raise AssertionError("responsibility-only update requires deprecated state")
                self.entry["responsibilities"] = copy.deepcopy(body["responsibilities"])
            self.entry["version"] += 1
            return SUITE.Response(200, copy.deepcopy(self.entry))
        if path == entry_path:
            if method == "GET":
                return SUITE.Response(200, copy.deepcopy(self.entry))
            expected_fields = {
                "version", "business_name", "business_description", "governance_status",
                "visibility", "domains", "glossary_ids", "responsibilities",
            }
            if set(body) != expected_fields:
                raise AssertionError(f"unexpected Catalog update fields: {set(body) ^ expected_fields}")
            if self.entry["governance_status"] == "deprecated":
                raise AssertionError("complete curation cannot bypass deprecation")
            if body["version"] != self.entry["version"]:
                return SUITE.Response(409, {"error_code": "catalog_entry_version_conflict"})
            self.entry.update(copy.deepcopy(body))
            if "domains" in body:
                self.entry["semantic_links"] = [
                    {"semantic_type": "domain", "semantic_id": item["id"], "relation_role": item["role"]}
                    for item in body["domains"]
                ]
            if "glossary_ids" in body:
                self.entry["semantic_links"] += [
                    {"semantic_type": "glossary", "semantic_id": item, "relation_role": "related"}
                    for item in body["glossary_ids"]
                ]
            self.entry["version"] += 1
            return SUITE.Response(200, copy.deepcopy(self.entry))
        if path == "/api/v1/asset/type-definitions":
            return SUITE.Response(200, [{"id": 2, "enabled": True}])
        if path == "/api/v1/asset/categories" and method == "POST":
            self.catalog_exists = True
            self.category_name = str(body["name"])
            return SUITE.Response(201, {"id": 20, "version": 1})
        if path == "/api/v1/asset/categories/20" and method == "DELETE":
            if body != {"version": 1}:
                raise AssertionError(f"unexpected AssetCategory delete body: {body}")
            self.catalog_exists = False
            return SUITE.Response(200, {})
        if path == "/api/v1/asset/categories/20" and method == "GET":
            return SUITE.Response(404, {})
        if path == "/api/v1/asset/assets" and method == "POST":
            self.asset_exists = True
            self.asset_status = "draft"
            return SUITE.Response(201, {"id": 30, "status": "draft"})
        if path == "/api/v1/asset/assets/30/publish":
            self.asset_status = "published"
            return SUITE.Response(200, {})
        if path == "/api/v1/asset/assets/30/offline":
            self.asset_status = "offline"
            return SUITE.Response(200, {})
        if path == "/api/v1/asset/assets/30" and method == "DELETE":
            if self.asset_status != "offline":
                raise AssertionError("published Asset was not offlined before deletion")
            self.asset_exists = False
            return SUITE.Response(200, {})
        if path == "/api/v1/asset/assets/30" and method == "GET":
            if self.asset_exists:
                return SUITE.Response(200, {"id": 30, "status": self.asset_status})
            return SUITE.Response(404, {})
        if path == "/api/v1/portal/assets/30" and method == "GET":
            if not self.asset_exists or self.asset_status != "published":
                return SUITE.Response(404, {})
            return SUITE.Response(
                200,
                {
                    "id": 30,
                    "status": "published",
                    "components": [{"catalog_entry_id": self.entry["id"], "role": "primary"}],
                },
            )
        if path == "/api/v1/portal/categories" and method == "GET":
            if not self.catalog_exists or not self.asset_exists or self.asset_status != "published":
                return SUITE.Response(200, [])
            return SUITE.Response(
                200,
                [{"id": 20, "name": self.category_name, "count": 1, "children": []}],
            )
        if path == "/api/v1/portal/categories/20/assets?page=1&page_size=100" and method == "GET":
            if not self.catalog_exists or not self.asset_exists or self.asset_status != "published":
                return SUITE.Response(200, {"data": [], "total": 0, "page": 1, "page_size": 100})
            return SUITE.Response(
                200,
                {
                    "data": [{"id": 30, "category_id": 20, "status": "published"}],
                    "total": 1,
                    "page": 1,
                    "page_size": 100,
                    "total_pages": 1,
                },
            )
        raise AssertionError(f"unexpected request {method} {path} body={body!r}")

    @staticmethod
    def _coverage_dimension(key, covered, applicable):
        return {
            "key": key,
            "covered": covered,
            "applicable": applicable,
            "not_covered": applicable - covered,
            "not_applicable": 1 - applicable,
            "coverage_rate": 100 if covered and applicable else 0,
        }


class EnterpriseCatalogPublishingOnlineTest(unittest.TestCase):
    def test_catalog_contract_uses_independent_standard_mappings(self) -> None:
        client = FakeClient()
        self.assertEqual(
            SUITE.COVERAGE_DIMENSIONS,
            {
                "business_definition", "primary_domain", "accountable_department",
                "business_owner", "data_steward", "glossary", "component_standard_mapping",
            },
        )
        payload = SUITE.editable_catalog_payload(client.entry)
        self.assertNotIn("component_elements", payload)
        self.assertNotIn("deprecation_reason", payload)

    def test_runs_unique_route_and_removes_temporary_resources(self) -> None:
        client = FakeClient()
        original = copy.deepcopy(client.entry)

        browser_calls = []

        def browser(entry_id, fingerprint, business_name, total_entries, category_id, asset_id):
            browser_calls.append((entry_id, fingerprint, business_name, total_entries, category_id, asset_id))
            return {"result": "passed"}

        report = SUITE.run_suite(client, 42, 7, "run-1", 31, 41, 51, 10, browser)

        self.assertEqual(report["schema_version"], "addp.enterprise-catalog-publishing/v2")
        self.assertEqual(report["route"], ["meta", "catalog", "asset", "portal"])
        self.assertEqual(report["meta_execution_ids"], ["execution-1", "execution-2"])
        self.assertEqual(report["cases"]["scan_idempotency"], "passed")
        self.assertEqual(report["cases"]["source_identity_resolution"], "passed")
        self.assertEqual(report["cases"]["governance_coverage"], "passed")
        self.assertEqual(report["cases"]["deprecated_responsibility_transfer_and_withdrawal"], "passed")
        self.assertEqual(report["catalog_lifecycle"]["states"], ["curated", "deprecated", "deprecated", "curated"])
        self.assertEqual(report["cases"]["browser"], "passed")
        self.assertEqual(report["cases"]["asset_category_portal_navigation"], "passed")
        self.assertEqual(
            browser_calls,
            [(client.entry["id"], "fingerprint-1", "ADDP Online Catalog Fixture run-1", 1, 20, 30)],
        )
        self.assertEqual(report["residual_resources"], 0)
        self.assertFalse(client.asset_exists)
        self.assertFalse(client.catalog_exists)
        self.assertEqual(client.entry["business_name"], original["business_name"])
        self.assertEqual(client.entry["business_description"], original["business_description"])
        self.assertEqual(SUITE.catalog_fixture_facts(client.entry), SUITE.catalog_fixture_facts(original))
        self.assertLess(
            client.calls.index(("POST", "/api/v1/asset/assets/30/offline")),
            client.calls.index(("DELETE", "/api/v1/asset/assets/30")),
        )
        self.assertIn(("GET", "/api/v1/portal/categories"), client.calls)
        self.assertIn(("GET", "/api/v1/portal/categories/20/assets?page=1&page_size=100"), client.calls)

    def test_first_discovered_fixture_is_curated_to_stable_permanent_state(self) -> None:
        client = FakeClient()
        client.entry.update(
            {
                "business_name": None,
                "business_description": None,
                "governance_status": "discovered",
                "visibility": "inventory",
                "semantic_links": [],
                "responsibilities": [],
            }
        )

        curated, restore, initialized = SUITE.curate_fixture_entry(
            client, client.entry, "run-1", 31, 41, 51
        )

        self.assertTrue(initialized)
        self.assertIsNone(restore)
        self.assertEqual(curated["governance_status"], "curated")
        self.assertEqual(curated["business_name"], "ADDP Online Catalog Fixture")
        self.assertEqual(curated["domains"], [{"id": "31", "role": "primary"}])

    def test_existing_fixture_rejects_a_different_primary_domain_without_mutating_it(self) -> None:
        client = FakeClient()
        client.entry["semantic_links"] = [
            {"semantic_type": "domain", "semantic_id": "99", "relation_role": "primary"}
        ]

        with self.assertRaisesRegex(SUITE.SuiteError, "configured business domain"):
            SUITE.curate_fixture_entry(client, client.entry, "run-1", 31, 41, 51)

        self.assertNotIn(("PUT", f"/api/v1/catalog/entries/{client.entry['id']}"), client.calls)

    def test_lifecycle_changes_optional_responsibility_and_restores_full_aggregate(self) -> None:
        for existing_technical_owner in (False, True):
            with self.subTest(existing_technical_owner=existing_technical_owner):
                client = FakeClient()
                if existing_technical_owner:
                    client.entry["responsibilities"].append(
                        {"role": "technical_owner", "subject_type": "user", "subject_id": "52"}
                    )
                original = copy.deepcopy(client.entry)
                report = SUITE.validate_deprecated_fixture_lifecycle(client, original, "run-1", 51)
                self.assertEqual(report["versions"], [4, 5, 6, 7])
                self.assertEqual(report["fixture_restoration"], "passed")
                self.assertEqual(SUITE.catalog_fixture_facts(client.entry), SUITE.catalog_fixture_facts(original))
                transfer = [body for path, body in client.writes if path.endswith("/responsibilities")][0]
                self.assertNotEqual(transfer["responsibilities"], original["responsibilities"])
                self.assertEqual(set(transfer), {"version", "reason", "responsibilities"})

    def test_lifecycle_restores_after_committed_write_response_is_lost(self) -> None:
        for phase in ("deprecation", "transfer", "withdrawal"):
            with self.subTest(phase=phase):
                class LostResponseClient(FakeClient):
                    lost = False

                    def request(self, method, path, expected, body=None):
                        response = super().request(method, path, expected, body)
                        matches = body is not None and (
                            (phase == "deprecation" and body.get("reason", "").startswith("Dedicated Online deprecation")) or
                            (phase == "transfer" and body.get("reason", "").startswith("Dedicated Online responsibility")) or
                            (phase == "withdrawal" and body.get("reason", "").startswith("Dedicated Online withdrawal"))
                        )
                        if matches and not self.lost:
                            self.lost = True
                            raise SUITE.SuiteError("injected lost write response")
                        return response

                client = LostResponseClient()
                original = copy.deepcopy(client.entry)
                with self.assertRaisesRegex(SUITE.SuiteError, "lost write response"):
                    SUITE.validate_deprecated_fixture_lifecycle(client, original, "run-1", 51)
                self.assertEqual(SUITE.catalog_fixture_facts(client.entry), SUITE.catalog_fixture_facts(original))
                attempted = [body for _, body in client.writes if body.get("reason", "").startswith({
                    "deprecation": "Dedicated Online deprecation",
                    "transfer": "Dedicated Online responsibility",
                    "withdrawal": "Dedicated Online withdrawal",
                }[phase])]
                self.assertEqual(len(attempted), 1, "lost command must not be retried")

    def test_lifecycle_detects_conflict_side_effects_and_restores_fixture(self) -> None:
        for suffix, message in (("/governance", "stale withdrawal"), ("/responsibilities", "stale responsibility transfer")):
            with self.subTest(suffix=suffix):
                class BrokenConflictClient(FakeClient):
                    def request(self, method, path, expected, body=None):
                        response = super().request(method, path, expected, body)
                        if response.status == 409 and path.endswith(suffix):
                            self.entry["business_description"] = "unexpected stale write"
                        return response

                client = BrokenConflictClient()
                original = copy.deepcopy(client.entry)
                with self.assertRaisesRegex(SUITE.SuiteError, message + " produced side effects"):
                    SUITE.validate_deprecated_fixture_lifecycle(client, original, "run-1", 51)
                self.assertEqual(SUITE.catalog_fixture_facts(client.entry), SUITE.catalog_fixture_facts(original))

    def test_lifecycle_cleanup_failure_cannot_report_success(self) -> None:
        class FailedCleanupClient(FakeClient):
            def request(self, method, path, expected, body=None):
                if method == "PUT" and body is not None and "business_name" in body:
                    raise SUITE.SuiteError("injected cleanup failure")
                return super().request(method, path, expected, body)

        client = FailedCleanupClient()
        with self.assertRaisesRegex(SUITE.SuiteError, "cleanup failed.*injected cleanup failure"):
            SUITE.validate_deprecated_fixture_lifecycle(client, client.entry, "run-1", 51)

    def test_cleanup_checks_description_and_all_associations_not_just_name(self) -> None:
        for key, broken_value in (("business_description", "not restored"), ("semantic_links", []),
                                  ("responsibilities", []), ("visibility", "inventory")):
            with self.subTest(key=key):
                class IncompleteRestoreClient(FakeClient):
                    def request(self, method, path, expected, body=None):
                        response = super().request(method, path, expected, body)
                        if method == "PUT" and body is not None and "business_name" in body:
                            self.entry[key] = broken_value
                        return response

                client = IncompleteRestoreClient()
                with self.assertRaisesRegex(SUITE.SuiteError, "complete curation aggregate was not restored"):
                    SUITE.restore_catalog_fixture(client, client.entry["id"], SUITE.editable_catalog_payload(client.entry), "run-1")

    def test_first_initialization_keeps_stable_curated_fixture_after_lifecycle_failure(self) -> None:
        class LostDeprecationClient(FakeClient):
            lost = False

            def request(self, method, path, expected, body=None):
                response = super().request(method, path, expected, body)
                if body and body.get("governance_status") == "deprecated" and not self.lost:
                    self.lost = True
                    raise SUITE.SuiteError("injected lost response")
                return response

        client = LostDeprecationClient()
        client.entry.update({"governance_status": "discovered", "business_name": None,
                             "business_description": None, "semantic_links": [], "responsibilities": []})
        with self.assertRaisesRegex(SUITE.SuiteError, "lost response"):
            SUITE.run_suite(client, 42, 7, "run-1", 31, 41, 51, 10)
        self.assertEqual(client.entry["governance_status"], "curated")
        self.assertEqual(client.entry["business_name"], "ADDP Online Catalog Fixture")
        self.assertEqual(len(client.entry["responsibilities"]), 3)

    def test_domain_preflight_failure_does_not_trigger_cleanup_write(self) -> None:
        client = FakeClient()
        client.entry["semantic_links"] = [
            {"semantic_type": "domain", "semantic_id": "99", "relation_role": "primary"}
        ]

        with self.assertRaisesRegex(SUITE.SuiteError, "configured business domain"):
            SUITE.run_suite(client, 42, 7, "run-1", 31, 41, 51, 10)

        self.assertNotIn(("PUT", f"/api/v1/catalog/entries/{client.entry['id']}"), client.calls)

    def test_validates_tenant_user_identity_and_permissions(self) -> None:
        class IdentityClient:
            permissions = SUITE.REQUIRED_PERMISSIONS

            def request(self, *_args, **_kwargs):
                return SUITE.Response(
                    200,
                    {
                        "principal": {"type": "user", "id": "51"},
                        "context": {"type": "tenant", "tenant_id": "42"},
                        "token": {"type": "first_party_access_token"},
                        "authorization": {
                            "role_assignments": [
                                {
                                    "role_key": "tenant.catalog_operator",
                                    "permissions": sorted(self.permissions),
                                }
                            ]
                        },
                    },
                )

        identity = SUITE.validate_user_identity(IdentityClient(), 42)
        self.assertEqual(identity["principal_id"], "51")
        self.assertEqual(identity["tenant_id"], "42")
        missing_deprecation = IdentityClient()
        missing_deprecation.permissions = SUITE.REQUIRED_PERMISSIONS - {"catalog.entry.deprecate"}
        with self.assertRaisesRegex(SUITE.SuiteError, "missing required permissions: catalog.entry.deprecate"):
            SUITE.validate_user_identity(missing_deprecation, 42)

    def test_validates_browser_report_contract(self) -> None:
        report = {
            "schema_version": "addp.enterprise-catalog-publishing-browser/v2",
            "suite": "enterprise-catalog-publishing",
            "run_id": "run-1",
            "result": "passed",
            "tenant_id": "42",
            "catalog_entry_id": self.entry_id,
            "source_identity": "fingerprint-1",
            "coverage_total_entries": 1,
            "coverage_dimensions": 7,
            "human_readable_filter_selectors": 3,
            "explicit_batch_governance_ui": True,
            "portal_category_id": "20",
            "portal_asset_id": "30",
            "portal_category_assets": 1,
            "browser_warning_errors": 0,
        }

        validated = SUITE.validate_browser_report(
            report, "run-1", "42", self.entry_id, "fingerprint-1", 1, 20, 30
        )

        self.assertEqual(validated, report)

    @property
    def entry_id(self) -> str:
        return "10000000-0000-0000-0000-000000000001"


if __name__ == "__main__":
    unittest.main()

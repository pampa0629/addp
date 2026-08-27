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
            "component_elements": [],
        }
        self.asset_exists = False
        self.asset_status = ""
        self.catalog_exists = False
        self.calls: list[tuple[str, str]] = []

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path))
        if path == "/api/v1/meta/scan/run/manual":
            return SUITE.Response(201, {"execution_id": "execution-1", "status": "pending"})
        if path == "/api/v1/meta/executions/execution-1":
            return SUITE.Response(200, {"execution_id": "execution-1", "status": "success"})
        if path == "/api/v1/meta/engines/7/items":
            return SUITE.Response(200, [{"id": 9, "name": SUITE.FIXTURE_TABLE, "fingerprint": "fingerprint-1"}])
        if path.startswith("/api/v1/catalog/entries?view=inventory&source_identity="):
            return SUITE.Response(200, {"data": [{"id": self.entry["id"]}], "total": 1})
        if path == f"/api/v1/catalog/entries/{self.entry['id']}":
            if method == "GET":
                return SUITE.Response(200, copy.deepcopy(self.entry))
            self.entry.update(copy.deepcopy(body))
            self.entry["version"] += 1
            return SUITE.Response(200, copy.deepcopy(self.entry))
        if path == "/api/v1/asset/type-definitions":
            return SUITE.Response(200, [{"id": 2, "enabled": True}])
        if path == "/api/v1/asset/catalogs" and method == "POST":
            self.catalog_exists = True
            return SUITE.Response(201, {"id": 20})
        if path == "/api/v1/asset/catalogs/20" and method == "DELETE":
            self.catalog_exists = False
            return SUITE.Response(200, {})
        if path == "/api/v1/asset/catalogs/20" and method == "GET":
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
        raise AssertionError(f"unexpected request {method} {path} body={body!r}")


class EnterpriseCatalogPublishingOnlineTest(unittest.TestCase):
    def test_runs_unique_route_and_removes_temporary_resources(self) -> None:
        client = FakeClient()
        original = copy.deepcopy(client.entry)

        report = SUITE.run_suite(client, 42, 7, "run-1", 31, 41, 51, 10)

        self.assertEqual(report["route"], ["meta", "catalog", "asset", "portal"])
        self.assertEqual(report["residual_resources"], 0)
        self.assertFalse(client.asset_exists)
        self.assertFalse(client.catalog_exists)
        self.assertEqual(client.entry["business_name"], original["business_name"])
        self.assertEqual(client.entry["business_description"], original["business_description"])
        self.assertLess(
            client.calls.index(("POST", "/api/v1/asset/assets/30/offline")),
            client.calls.index(("DELETE", "/api/v1/asset/assets/30")),
        )

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

    def test_validates_tenant_user_identity_and_permissions(self) -> None:
        class IdentityClient:
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
                                    "permissions": sorted(SUITE.REQUIRED_PERMISSIONS),
                                }
                            ]
                        },
                    },
                )

        identity = SUITE.validate_user_identity(IdentityClient(), 42)
        self.assertEqual(identity["principal_id"], "51")
        self.assertEqual(identity["tenant_id"], "42")


if __name__ == "__main__":
    unittest.main()

import importlib.util
import sys
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("standard-model-reference-deletion-online.py")
SPEC = importlib.util.spec_from_file_location("standard_model_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class FakeGateway:
    def __init__(self, malformed_block=False, fail_entity_delete=False):
        self.domain = False
        self.entity = False
        self.malformed_block = malformed_block
        self.fail_entity_delete = fail_entity_delete
        self.calls = []

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, body))
        if method == "POST" and path.endswith("/domains"):
            self.domain = True
            return ONLINE.Response(201, {"id": 11, "tenant_id": 42, "version": 1})
        if method == "POST" and path.endswith("/entities"):
            self.entity = True
            return ONLINE.Response(201, {"id": 22, "tenant_id": 42, "version": 1})
        if method == "DELETE" and path.endswith("/domains/11"):
            if self.entity:
                return ONLINE.Response(
                    409,
                    {
                        "error_code": "wrong" if self.malformed_block else "standard_resource_referenced",
                        "reference_count": 1,
                    },
                )
            self.domain = False
            return ONLINE.Response(200, {"message": "deleted"})
        if method == "DELETE" and path.endswith("/entities/22"):
            if self.fail_entity_delete:
                raise ONLINE.SuiteError("forced entity cleanup failure")
            self.entity = False
            return ONLINE.Response(200, {"message": "deleted"})
        if method == "GET" and path.endswith("/domains/11"):
            if self.domain:
                return ONLINE.Response(200, {"id": 11, "version": 1, "lifecycle_state": "active"})
            return ONLINE.Response(404, {"error_code": "resource_not_found"})
        if method == "GET" and path.endswith("/entities/22"):
            if self.entity:
                return ONLINE.Response(200, {"id": 22, "version": 1})
            return ONLINE.Response(404, {"error_code": "entity_not_found"})
        raise AssertionError(f"unexpected request: {method} {path}")


class FakeSystem:
    def __init__(self, *, principal="user", tenant_id="42", roles=None, permissions=None):
        self.payload = {
            "principal": {"type": principal, "id": "99"},
            "context": {"type": "tenant", "tenant_id": tenant_id},
            "token": {"type": "first_party_access_token"},
            "authorization": {
                "role_assignments": [
                    {
                        "role_key": role,
                        "permissions": sorted(permissions or ONLINE.REQUIRED_USER_PERMISSIONS),
                    }
                    for role in (roles or ["tenant.governance_manager", "tenant.data_architect"])
                ]
            },
        }

    def request(self, method, path, expected, body=None):
        if (method, path, tuple(expected), body) != (
            "GET",
            "/api/v1/system/auth/context",
            (200,),
            None,
        ):
            raise AssertionError(f"unexpected identity request: {method} {path}")
        return ONLINE.Response(200, self.payload)


class StandardModelOnlineTest(unittest.TestCase):
    def test_accepts_dedicated_tenant_user_with_exact_required_permissions(self):
        report = ONLINE.validate_user_identity(FakeSystem(), 42)
        self.assertEqual(report["principal_type"], "user")
        self.assertEqual(report["tenant_id"], "42")
        self.assertEqual(
            set(report["permissions_verified"]), ONLINE.REQUIRED_USER_PERMISSIONS
        )

    def test_rejects_admin_or_underprivileged_online_user(self):
        with self.assertRaisesRegex(ONLINE.SuiteError, "administrator roles"):
            ONLINE.validate_user_identity(
                FakeSystem(roles=["tenant.administrator"]), 42
            )
        with self.assertRaisesRegex(ONLINE.SuiteError, "missing required permissions"):
            ONLINE.validate_user_identity(
                FakeSystem(permissions={"standard.domain.read"}), 42
            )

    def test_rejects_wrong_tenant_or_non_user_token(self):
        with self.assertRaisesRegex(ONLINE.SuiteError, "does not match"):
            ONLINE.validate_user_identity(FakeSystem(tenant_id="7"), 42)
        with self.assertRaisesRegex(ONLINE.SuiteError, "belong to a User"):
            ONLINE.validate_user_identity(FakeSystem(principal="service_principal"), 42)

    def test_accepts_reference_block_release_and_zero_residual(self):
        gateway = FakeGateway()
        report = ONLINE.run_suite(gateway, 42, "run-42")

        self.assertEqual(report["created_resources"], 2)
        self.assertEqual(report["deleted_resources"], 2)
        self.assertEqual(report["residual_resources"], 0)
        self.assertFalse(gateway.domain)
        self.assertFalse(gateway.entity)

    def test_business_failure_still_cleans_both_owner_resources(self):
        gateway = FakeGateway(malformed_block=True)
        with self.assertRaisesRegex(ONLINE.SuiteError, "standard_resource_referenced"):
            ONLINE.run_suite(gateway, 42, "run-42")
        self.assertFalse(gateway.domain)
        self.assertFalse(gateway.entity)

    def test_cleanup_failure_overrides_business_result(self):
        gateway = FakeGateway(malformed_block=True, fail_entity_delete=True)
        with self.assertRaisesRegex(ONLINE.SuiteError, "cleanup failed"):
            ONLINE.run_suite(gateway, 42, "run-42")
        self.assertTrue(gateway.entity)
        self.assertTrue(gateway.domain)

    def test_rejects_created_resource_from_another_tenant(self):
        gateway = FakeGateway()
        original = gateway.request

        def wrong_tenant(method, path, expected, body=None):
            response = original(method, path, expected, body)
            if method == "POST" and path.endswith("/domains"):
                response.payload["tenant_id"] = 7
            return response

        gateway.request = wrong_tenant
        with self.assertRaisesRegex(ONLINE.SuiteError, "configured Tenant"):
            ONLINE.run_suite(gateway, 42, "run-42")
        self.assertFalse(gateway.domain)


if __name__ == "__main__":
    unittest.main()

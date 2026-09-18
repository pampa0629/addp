import copy
import hashlib
import importlib
import json
import os
import unittest
from unittest.mock import patch

ONLINE = importlib.import_module("scripts.test.ontology-revision-lifecycle-online")
API = ONLINE.API


class Gateway:
    """Strict owner contract double; does not claim real IAM/Falkor acceptance."""
    def __init__(self, defect=None):
        self.defect, self.calls, self.revisions, self.projections = defect, [], {}, {}
        self.head = None
        self.polls = {}

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, copy.deepcopy(body)))
        status, result = self.respond(method, path, body)
        if status not in expected:
            raise API.SuiteError(f"HTTP {status}")
        return API.Response(status, copy.deepcopy(result))

    def respond(self, method, path, body):
        parts = path.removeprefix(ONLINE.BASE + "/").split("/")
        ontology_id = parts[0]
        if len(parts) == 1:
            return (200, self.head) if self.head else (404, {})
        if len(parts) == 2 and method == "POST":
            revision = body["revision"]
            if not self.head:
                self.head = dict(ontology_id=ontology_id, last_revision=0, activation_version=0,
                                 active_revision=None, active_generation=None)
            self.head["last_revision"] = revision
            if self.defect == "activate_draft" and revision == 2:
                self.head["active_revision"] = 2
            definition = copy.deepcopy(body["definition"])
            definition["scope"] = dict(tenant_id=43 if self.defect == "tenant" else 42,
                                       ontology_id=ontology_id, revision=revision)
            if self.defect == "lost_definition":
                definition["rules"] = []
            item = dict(ontology_id=ontology_id, revision=revision, version=1, status="draft",
                        snapshot={"definition": definition}, digest=hashlib.sha256(str(revision).encode()).hexdigest())
            self.revisions[revision] = item
            return 201, item
        if parts[1] == "projections":
            item = self.projections[parts[2]]
            self.polls[parts[2]] = self.polls.get(parts[2], 0) + 1
            if self.polls[parts[2]] > 1:
                if self.defect == "build_failed":
                    item["status"] = "failed"
                else:
                    item["status"] = "ready"
                    self.head.update(active_revision=item["revision"], active_generation=item["generation"],
                                     activation_version=item["baseline_version"] + 1)
            if self.defect == "digest":
                item["digest"] = "0" * 64
            return 200, item
        revision = int(parts[2])
        item = self.revisions[revision]
        if len(parts) == 3:
            return 200, item
        action = parts[3]
        if action == "projection":
            generation = item.get("initial_generation")
            if not generation:
                return 404, {}
            result = copy.deepcopy(self.projections[generation])
            if self.defect == "latest_identity":
                result["execution_id"] = "incorrect"
            return 200, result
        if body["version"] != item["version"]:
            return 409, {"error_code": "resource_version_conflict"}
        item["version"] += 1
        if action == "submit":
            item["status"] = "in_review"
            return 200, item
        if action == "publish":
            if self.defect == "admission":
                return 502, {"error_code": "projection_admission_failed"}
            item["status"] = "published"
            generation = f"00000000-0000-4000-8000-{revision:012d}"
            execution = f"00000000-0000-4000-8001-{revision:012d}"
            item.update(initial_generation=generation, initial_execution_id=execution)
            self.projections[generation] = dict(ontology_id=ontology_id, revision=revision, generation=generation,
                execution_id=execution, digest=item["digest"], status="pending", predecessor_generation=None,
                baseline_version=self.head["activation_version"])
            if self.defect == "mutated_history" and revision == 2:
                self.revisions[1]["digest"] = "0" * 64
            return 202, dict(ontology_id=ontology_id, revision=revision, version=item["version"],
                             generation=generation, execution_id=execution)
        if action == "withdraw":
            item["status"] = "withdrawn"
            if self.head["active_revision"] == revision:
                self.head.update(active_revision=None, active_generation=None,
                                 activation_version=self.head["activation_version"] + 1)
            if self.defect == "fallback":
                self.head["active_revision"] = 1
            return 200, item
        raise AssertionError(action)


class OntologyOnlineTest(unittest.TestCase):
    def run_scenario(self, gateway, report):
        def wait(client, path, accepted, digest):
            return ONLINE.wait_active(client, path, accepted, digest, sleep=lambda _: None)
        return ONLINE.run_suite(gateway, 42, "fixture-run", report, wait=wait)

    def test_two_revisions_require_real_activation_and_withdraw_without_fallback(self):
        gateway, report = Gateway(), {}
        self.run_scenario(gateway, report)
        self.assertEqual("passed", report["result"])
        self.assertEqual("deployment_teardown_required", report["cleanup"])
        self.assertEqual(2, len(report["generations"]))
        self.assertEqual({"withdrawn"}, {r["status"] for r in gateway.revisions.values()})
        self.assertTrue(all(n >= 2 for n in gateway.polls.values()))
        self.assertNotIn("token", json.dumps(report))

    def test_contract_errors_fail_closed_without_replaying_writes(self):
        for defect in ("tenant", "lost_definition", "activate_draft", "digest", "build_failed",
                       "latest_identity", "admission", "mutated_history", "fallback"):
            with self.subTest(defect=defect):
                gateway, report = Gateway(defect), {}
                with self.assertRaises(API.SuiteError):
                    self.run_scenario(gateway, report)
                self.assertEqual("failed", report["result"])
                writes = [(m, p, json.dumps(b, sort_keys=True)) for m, p, b in gateway.calls if m != "GET"]
                self.assertEqual(len(writes), len(set(writes)))

    def test_pending_is_not_success_and_wait_has_a_deadline(self):
        accepted = dict(ontology_id="test", revision=1, generation="generation", execution_id="execution")
        class Pending:
            def request(self, *args):
                return API.Response(200, dict(accepted, digest="digest", status="pending", predecessor_generation=None))
        ticks = iter((0, 0, 0.5, 1))
        with self.assertRaisesRegex(API.SuiteError, "timed out"):
            ONLINE.wait_active(Pending(), "path", accepted, "digest", timeout=1,
                               clock=lambda: next(ticks), sleep=lambda _: None)

    def test_personal_environment_rejected_before_http(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(API, "GatewayClient") as client:
            self.assertEqual(1, ONLINE.main())
            client.assert_not_called()

    def test_registration_is_manual_and_checks_all_three_owners(self):
        registry = importlib.import_module("scripts.test.online-gate")
        suite = registry.SUITES[ONLINE.SUITE]
        self.assertFalse(suite.nightly)
        self.assertEqual({"gateway", "system", "ontology"}, {name for name, _ in suite.services})


if __name__ == "__main__":
    unittest.main()

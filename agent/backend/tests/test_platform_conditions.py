import copy
import json
import unittest
from pathlib import Path

from agents.checkpoint import capture_owner_facts, confirm_selection, new_checkpoint, normalize_checkpoint, canonicalize_clarification_options
from agents.platform_conditions import CREATE_TOOL, prepare_review, validate_create


def platform_fixture():
    definition = json.loads((Path(__file__).resolve().parents[3] / "ontology/backend/internal/platform/transfer.json").read_text())
    definition["digest"] = "a" * 64  # Controlled owner response, not a publication proof.
    source = {
        "locator": "addp://engine/11/path/demo/events?type=collection&item_id=42",
        "engine_id": 11, "source_engine_type": "mongodb", "item_type": "collection",
        "query_names": {"mql": "events"}, "schema_coverage": "sampled",
        "fields": [{"name": "_id", "type": "string"}, {"name": "status", "type": "string"},
                   {"name": "members", "type": "array"}, {"name": "members.id", "type": "string"}],
    }
    target = {"locator": "addp://engine/12/path/demo?type=schema&node_id=5", "type": "schema", "children": []}
    arguments = {
        "name": "reviewed fixture", "config": {
            "runtime": {"boundary": "bounded"}, "load": {"mode": "snapshot"},
            "source": {"locator": source["locator"], "data_type": "table", "representation": "native",
                       "query": {"language": "mql", "statement": json.dumps({"aggregate": "events", "pipeline": [{"$project": {"id": "$_id", "status": 1, "_id": 0}}]})}},
            "target": {"parent_locator": target["locator"], "name": "new_events", "data_type": "table", "representation": "native", "policy": {"apply_mode": "replace"}},
            "transforms": [{"type": "field_mapping", "version": "v1", "mode": "project", "fields": [
                {"source": "id", "target": "id", "target_type": "string"}, {"source": "status", "target": "status", "target_type": "string"},
            ]}],
        },
    }
    checkpoint = new_checkpoint()
    for name, result in [("platform.capability.context", definition), ("resource.facts.get", source), ("resource.children.list", target)]:
        capture_owner_facts(name, result, checkpoint)
    return definition, source, target, arguments, checkpoint


class PlatformConditionTests(unittest.TestCase):
    def test_published_conditions_have_semantic_concepts_and_adapters(self):
        definition, _, _, arguments, checkpoint = platform_fixture()
        ids = {concept["id"] for concept in definition["concepts"]}
        edges = {(relation["from"], relation["kind"], relation["to"]) for relation in definition["relations"]}
        for condition in definition["operation"]["inputs_required"]:
            self.assertIn(condition, ids)
            self.assertIn(("transfer_task", "requires", condition), edges)
        self.assertEqual(validate_create(arguments, checkpoint, require_review=False)["revision"], definition["revision"])

    def test_review_binds_exact_arguments_semantics_and_owner_facts(self):
        _, source, _, arguments, checkpoint = platform_fixture()
        with self.assertRaisesRegex(ValueError, "user_review"):
            validate_create(arguments, checkpoint)
        identity = prepare_review(arguments, checkpoint)
        confirm_selection(checkpoint, {"value": identity["fingerprint"], "candidate": {"operation_review": identity}})
        self.assertEqual(validate_create(arguments, normalize_checkpoint(checkpoint)), identity)
        for path, value in [("name", "different"), ("batch_size", 500), ("description", "changed")]:
            changed = {**arguments, path: value}
            with self.subTest(path=path), self.assertRaisesRegex(ValueError, "user_review"):
                validate_create(changed, checkpoint)
        changed = copy.deepcopy(checkpoint)
        changed["observed"]["platform_capabilities"][CREATE_TOOL]["digest"] = "b" * 64
        with self.assertRaisesRegex(ValueError, "user_review"):
            validate_create(arguments, changed)
        changed = copy.deepcopy(checkpoint)
        changed["observed"]["resources"][source["locator"]]["fields"][0]["type"] = "integer"
        with self.assertRaisesRegex(ValueError, "user_review"):
            validate_create(arguments, changed)
        for key, value in (("name", "another_table"), ("policy", {"apply_mode": "append"})):
            changed = copy.deepcopy(arguments)
            changed["config"]["target"][key] = value
            with self.subTest(target=key), self.assertRaisesRegex(ValueError, "user_review"):
                validate_create(changed, checkpoint)
        changed = copy.deepcopy(arguments)
        changed["config"]["source"]["query"]["statement"] = json.dumps({"aggregate": "events", "pipeline": [{"$project": {"id": "$_id", "status": "$status", "_id": 0}}]})
        with self.assertRaisesRegex(ValueError, "user_review"):
            validate_create(changed, checkpoint)

    def test_conditions_reject_missing_evidence_unknown_conditions_and_forged_review(self):
        _, source, target, arguments, checkpoint = platform_fixture()
        for locator in (source["locator"], target["locator"]):
            changed = copy.deepcopy(checkpoint)
            changed["observed"]["resources"].pop(locator)
            with self.subTest(locator=locator), self.assertRaises(ValueError):
                validate_create(arguments, changed, require_review=False)
        changed = copy.deepcopy(checkpoint)
        changed["observed"]["platform_capabilities"][CREATE_TOOL]["operation"]["inputs_required"].append("unsupported_requirement")
        with self.assertRaisesRegex(ValueError, "unsupported:"):
            validate_create(arguments, changed, require_review=False)
        with self.assertRaisesRegex(ValueError, "not_observed"):
            confirm_selection(checkpoint, {"candidate": {"operation_review": {"tool": CREATE_TOOL}}})
        with self.assertRaisesRegex(ValueError, "runtime_preparation"):
            canonicalize_clarification_options("anything", [{"candidate": {"operation_review": {}}}], checkpoint)

    def test_only_explicit_schema_grounded_projection_is_admitted(self):
        _, _, _, arguments, checkpoint = platform_fixture()
        bad_commands = [
            {"aggregate": "other", "pipeline": [{"$project": {"id": "$_id"}}]},
            {"aggregate": "events", "pipeline": [{"$project": {"id": "$invented"}}]},
            {"aggregate": "events", "pipeline": [{"$project": {"id": {"$ifNull": ["$_id", None]}}}]},
            {"aggregate": "events", "pipeline": [{"$unwind": "$unknown"}, {"$project": {"id": "$_id"}}]},
            {"aggregate": "events", "pipeline": [{"$project": {"id": "$members.id"}}]},
        ]
        for command in bad_commands:
            changed = copy.deepcopy(arguments)
            changed["config"]["source"]["query"]["statement"] = json.dumps(command)
            with self.subTest(command=command), self.assertRaisesRegex(ValueError, "row_grain"):
                validate_create(changed, checkpoint, require_review=False)
        good = copy.deepcopy(arguments)
        good["config"]["source"]["query"]["statement"] = json.dumps({"aggregate": "events", "pipeline": [{"$unwind": "$members"}, {"$project": {"id": "$members.id", "status": 1, "_id": 0}}]})
        validate_create(good, checkpoint, require_review=False)
        changed = copy.deepcopy(arguments)
        changed["config"]["transforms"][0]["fields"][0]["source"] = "invented_output"
        with self.assertRaisesRegex(ValueError, "row_grain"):
            validate_create(changed, checkpoint, require_review=False)

    def test_cancel_and_consumption_do_not_authorize_a_write(self):
        _, _, _, arguments, checkpoint = platform_fixture()
        identity = prepare_review(arguments, checkpoint)
        confirm_selection(checkpoint, {"value": "cancel", "candidate": {"operation_review": identity}})
        with self.assertRaisesRegex(ValueError, "user_review"):
            validate_create(arguments, checkpoint)
        confirm_selection(checkpoint, {"value": identity["fingerprint"], "candidate": {"operation_review": identity}})
        checkpoint["confirmed"]["operation_reviews"].pop(CREATE_TOOL)
        with self.assertRaisesRegex(ValueError, "user_review"):
            validate_create(arguments, checkpoint)

    def test_refreshed_formal_facts_do_not_retain_missing_fields(self):
        _, source, _, arguments, checkpoint = platform_fixture()
        identity = prepare_review(arguments, checkpoint)
        confirm_selection(checkpoint, {"value": identity["fingerprint"], "candidate": {"operation_review": identity}})
        refreshed = {key: value for key, value in source.items() if key not in {"fields", "query_names"}}
        capture_owner_facts("resource.facts.get", refreshed, checkpoint)
        fact = checkpoint["observed"]["resources"][source["locator"]]
        self.assertNotIn("fields", fact)
        self.assertNotIn("query_names", fact)
        with self.assertRaisesRegex(ValueError, "confirmed_source"):
            validate_create(arguments, checkpoint)

    def test_snapshot_policies_are_manifest_owned_and_reject_incremental_keys(self):
        _, _, _, arguments, checkpoint = platform_fixture()
        for mode in ("replace", "append"):
            changed = copy.deepcopy(arguments)
            changed["config"]["target"]["policy"] = {"apply_mode": mode}
            with self.subTest(mode=mode):
                validate_create(changed, checkpoint, require_review=False)
        for policy in (
            {"apply_mode": "upsert"}, {"apply_mode": "upsert", "keys": ["id"]},
            {"apply_mode": "replace", "keys": ["id"]}, {"apply_mode": "append", "keys": ["id"]},
        ):
            changed = copy.deepcopy(arguments)
            changed["config"]["target"]["policy"] = policy
            with self.subTest(policy=policy):
                with self.assertRaisesRegex(ValueError, "platform_condition_unsatisfied:arguments"):
                    prepare_review(changed, checkpoint)
                with self.assertRaisesRegex(ValueError, "platform_condition_unsatisfied:arguments"):
                    validate_create(changed, checkpoint, require_review=False)

    def test_preview_cannot_change_or_fill_formal_schema_evidence(self):
        _, source, _, arguments, checkpoint = platform_fixture()
        preview = {
            "preview_type": "table", "metadata": {"locator": source["locator"]},
            "data": {"query_names": {"mql": "invented"}, "column_metadata": [{"column_name": "invented", "type": "string"}]},
        }
        before = copy.deepcopy(checkpoint)
        self.assertEqual(capture_owner_facts("data.preview", preview, checkpoint), {})
        self.assertEqual(checkpoint, before)
        incomplete = {key: value for key, value in source.items() if key not in {"fields", "query_names"}}
        capture_owner_facts("resource.facts.get", incomplete, checkpoint)
        self.assertEqual(capture_owner_facts("data.preview", preview, checkpoint), {})
        with self.assertRaisesRegex(ValueError, "confirmed_source"):
            validate_create(arguments, checkpoint, require_review=False)

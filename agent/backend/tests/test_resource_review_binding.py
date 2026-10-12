"""Formal resource metadata must survive resume and bind creation review."""

import copy
import json
import unittest

from agents.checkpoint import capture_owner_facts, confirm_selection, normalize_checkpoint
from agents.platform_conditions import CREATE_TOOL, prepare_review, validate_create
from tests.test_platform_conditions import platform_fixture, precheck_fixture


def array_review_fixture():
    _, source, _, arguments, checkpoint = platform_fixture()
    source["fields"][2].update({
        "element_type": "string", "native_type": "array",
        "path": ["members"], "nullable": True,
    })
    arguments["config"]["source"].pop("query")
    arguments["config"]["transforms"][0]["fields"] = [
        {"source": "_id", "target": "id", "target_type": "string"},
        {"source": "members", "target": "members", "target_type": "json"},
    ]
    capture_owner_facts("resource.facts.get", source, checkpoint)
    precheck_fixture(arguments, checkpoint)
    identity = prepare_review(arguments, checkpoint)
    confirm_selection(checkpoint, {
        "value": identity["fingerprint"], "candidate": {"operation_review": identity},
    })
    return source, arguments, checkpoint, identity


class ResourceReviewBindingTests(unittest.TestCase):
    def test_array_metadata_and_confirmation_survive_json_checkpoint_round_trip(self):
        source, arguments, checkpoint, identity = array_review_fixture()
        restored = normalize_checkpoint(json.loads(json.dumps(checkpoint)))
        fact = restored["observed"]["resources"][source["locator"]]
        self.assertEqual(fact["fields"][2], source["fields"][2])
        self.assertEqual(validate_create(arguments, restored), identity)

        # Identical formal refresh is not a semantic change.
        self.assertEqual(capture_owner_facts("resource.facts.get", source, restored), {})
        self.assertEqual(validate_create(arguments, restored), identity)

        # The checkpoint owns its snapshot, not the mutable Tool response.
        source["fields"][2]["path"].append("not_observed")
        self.assertEqual(fact["fields"][2]["path"], ["members"])
        self.assertEqual(validate_create(arguments, restored), identity)

    def test_formal_metadata_refresh_requires_new_review_before_creation(self):
        for key, value in (
            ("element_type", "json"), ("native_type", "object_array"),
            ("path", ["payload", "members"]), ("nullable", False),
            ("element_type", None), ("schema_coverage", "unknown"),
        ):
            with self.subTest(metadata=key, value=value):
                source, arguments, checkpoint, old_identity = array_review_fixture()
                refreshed = copy.deepcopy(source)
                if key == "schema_coverage":
                    refreshed[key] = value
                elif value is None:
                    refreshed["fields"][2].pop(key)
                else:
                    refreshed["fields"][2][key] = value
                delta = capture_owner_facts("resource.facts.get", refreshed, checkpoint)
                self.assertEqual(len(delta["resources"]), 1)
                checkpoint = normalize_checkpoint(json.loads(json.dumps(checkpoint)))
                current = checkpoint["observed"]["resources"][source["locator"]]
                self.assertEqual(current["fields"][2], refreshed["fields"][2])
                self.assertEqual(current["schema_coverage"], refreshed["schema_coverage"])
                with self.assertRaisesRegex(ValueError, "platform_condition_unsatisfied:user_review"):
                    validate_create(arguments, checkpoint)

                new_identity = prepare_review(arguments, checkpoint)
                self.assertNotEqual(new_identity["fingerprint"], old_identity["fingerprint"])
                self.assertNotIn(CREATE_TOOL, checkpoint["confirmed"]["operation_reviews"])
                with self.assertRaisesRegex(ValueError, "platform_condition_unsatisfied:user_review"):
                    validate_create(arguments, checkpoint)
                confirm_selection(checkpoint, {
                    "value": new_identity["fingerprint"],
                    "candidate": {"operation_review": new_identity},
                })
                # Only configuration review is established, not type executability.
                self.assertEqual(validate_create(arguments, checkpoint), new_identity)

    def test_preview_cannot_replace_array_metadata_but_formal_field_removal_is_binding(self):
        source, arguments, checkpoint, identity = array_review_fixture()
        preview = {
            "preview_type": "table", "metadata": {"locator": source["locator"]},
            "data": {"column_metadata": [{
                "column_name": "members", "type": "array", "element_type": "json",
            }], "rows": [{"private_sample": "must_not_persist"}]},
        }
        before = copy.deepcopy(checkpoint)
        self.assertEqual(capture_owner_facts("data.preview", preview, checkpoint), {})
        self.assertEqual(checkpoint, before)
        self.assertEqual(validate_create(arguments, checkpoint), identity)

        refreshed = copy.deepcopy(source)
        refreshed["fields"] = [field for field in refreshed["fields"] if field["name"] != "members"]
        capture_owner_facts("resource.facts.get", refreshed, checkpoint)
        current = checkpoint["observed"]["resources"][source["locator"]]
        self.assertNotIn("members", {field["name"] for field in current["fields"]})
        with self.assertRaisesRegex(ValueError, "platform_condition_unsatisfied:confirmed_row_grain_and_fields"):
            prepare_review(arguments, checkpoint)
        with self.assertRaisesRegex(ValueError, "platform_condition_unsatisfied:confirmed_row_grain_and_fields"):
            validate_create(arguments, checkpoint)

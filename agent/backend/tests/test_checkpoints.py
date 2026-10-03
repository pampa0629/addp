import json
import unittest

from agents.checkpoint import (
    CHECKPOINT_MAX_BYTES,
    CHECKPOINT_SCHEMA,
    canonicalize_clarification_options,
    capture_owner_facts,
    checkpoint_prompt,
    confirm_selection,
    new_checkpoint,
    normalize_checkpoint,
    validate_checkpoint_size,
)


class AgentCheckpointTests(unittest.TestCase):
    def test_owner_facts_round_trip_without_raw_result(self):
        checkpoint = new_checkpoint()
        engine_delta = capture_owner_facts(
            "engine.list",
            {
                "engines": [
                    {
                        "id": 20,
                        "name": "GeoPython Workflow",
                        "engine_type": "geopython_workflow",
                        "lifecycle_state": "active",
                        "connection_status": "online",
                        "connection_info": {"password": "must-not-persist"},
                    }
                ]
            },
            checkpoint,
        )
        locator = "addp://engine/8/path/public/railway?type=table&item_id=60"
        resource_delta = capture_owner_facts(
            "data.search",
            {
                "results": [
                    {
                        "name": "railway",
                        "asset_type": "table",
                        "location": {
                            "locator": locator,
                            "engine_id": 8,
                            "full_name": "public.railway",
                        },
                        "content": "large search content must not persist",
                    }
                ]
            },
            checkpoint,
        )

        restored = normalize_checkpoint(checkpoint)
        self.assertEqual(restored["schema"], CHECKPOINT_SCHEMA)
        self.assertEqual(restored["observed"]["workflow_engines"]["20"]["id"], 20)
        self.assertEqual(restored["observed"]["workflow_engines"]["20"]["lifecycle_state"], "active")
        self.assertNotIn("connection_info", restored["observed"]["workflow_engines"]["20"])
        self.assertEqual(restored["observed"]["resources"][locator]["full_name"], "public.railway")
        self.assertNotIn("content", restored["observed"]["resources"][locator])
        self.assertEqual(engine_delta["workflow_engines"][0]["id"], 20)
        self.assertEqual(resource_delta["resources"][0]["locator"], locator)

    def test_confirmed_selection_must_use_observed_canonical_fact(self):
        checkpoint = new_checkpoint()
        locator = "addp://engine/8/path/public/farmland?type=table&item_id=55"
        capture_owner_facts(
            "data.search",
            {
                "results": [
                    {
                        "name": "farmland",
                        "asset_type": "table",
                        "location": {
                            "locator": locator,
                            "engine_id": 8,
                            "full_name": "public.farmland",
                        },
                    }
                ]
            },
            checkpoint,
        )
        options = canonicalize_clarification_options(
            "data_source_ambiguous",
            [{"label": "untrusted", "value": locator, "candidate": {"locator": locator}}],
            checkpoint,
        )
        confirm_selection(checkpoint, options[0])

        confirmed = checkpoint["confirmed"]["resources"][locator]
        self.assertEqual(confirmed["full_name"], "public.farmland")
        self.assertIn("public.farmland", checkpoint_prompt(checkpoint))

    def test_unknown_checkpoint_schema_starts_clean(self):
        checkpoint = normalize_checkpoint({"schema": "unknown", "observed": {"resources": {"x": {}}}})

        self.assertEqual(checkpoint, new_checkpoint())

    def test_target_directory_options_only_use_parent_and_direct_children(self):
        checkpoint = new_checkpoint()
        parent = "addp://engine/12/path"
        child = "addp://engine/12/path/public"
        descendant = "addp://engine/12/path/public/outdoor"
        hidden = "addp://engine/99/path/hidden"
        capture_owner_facts(
            "resource.children.list",
            {
                "locator": parent, "label": "业务 PostgreSQL", "type": "engine",
                "metadata": {"locator": hidden, "password": "must-not-persist"},
                "children": [{
                    "locator": child, "label": "public", "type": "schema",
                    "children": [{"locator": descendant, "label": "outdoor"}],
                }],
            },
            checkpoint,
        )
        restored = normalize_checkpoint(checkpoint)
        self.assertEqual(set(restored["observed"]["resources"]), {parent, child})
        options = canonicalize_clarification_options(
            "target_parent_ambiguous",
            [{"label": "untrusted", "value": child}], restored,
        )
        self.assertEqual(options[0]["label"], "public")
        self.assertEqual(options[0]["candidate"]["item_type"], "schema")
        confirm_selection(restored, options[0])
        self.assertEqual(restored["confirmed"]["resources"][child]["name"], "public")
        for locator in (descendant, hidden):
            with self.assertRaisesRegex(ValueError, "未由 owner Tool 返回"):
                canonicalize_clarification_options(
                    "target_parent_ambiguous", [{"value": locator}], restored,
                )
        self.assertNotIn("must-not-persist", json.dumps(restored))

    def test_candidate_locator_cannot_bypass_observation_with_generic_reason(self):
        locator = "addp://engine/99/path/hidden"
        with self.assertRaisesRegex(ValueError, "未由 owner Tool 返回"):
            canonicalize_clarification_options(
                "missing_input",
                [{"label": "fabricated", "value": "choice", "candidate": {"locator": locator}}],
                new_checkpoint(),
            )

    def test_formal_resource_facts_survive_resume_without_rows_or_expressions(self):
        checkpoint = new_checkpoint()
        locator = "addp://engine/9/path/outdoor/routes?type=collection&item_id=66"
        capture_owner_facts(
            "data.search", {"results": [{"name": "routes", "location": {"locator": locator}}]},
            checkpoint,
        )
        result = {
            "locator": locator, "engine_id": 9, "engine_name": "业务 MongoDB",
            "source_engine_type": "mongodb", "item_id": 66, "item_type": "collection",
            "data_type": "table", "full_name": "outdoor.routes", "item_fingerprint": "fp",
            "scanned_depth": "deep", "schema_coverage": "sampled",
            "query_names": {"database": "outdoor", "collection": "routes"},
            "fields": [{
                "name": "distance", "path": ["metrics", "distance"], "type": "float",
                "native_type": "double", "nullable": True, "primary_key": False,
                "precision": 10, "scale": 2,
                "default_expression": "must-not-persist-default",
                "generation_expression": "must-not-persist-expression",
                "sample": {"secret": "must-not-persist-sample"},
            }],
            "rows": [{"secret": "must-not-persist-row"}],
            "connection_info": {"password": "must-not-persist-password"},
            "metadata": {"locator": "addp://engine/99/path/hidden"},
        }
        delta = capture_owner_facts("resource.facts.get", result, checkpoint)
        restored = normalize_checkpoint(checkpoint)
        fact = restored["observed"]["resources"][locator]
        self.assertEqual(fact["name"], "routes")
        self.assertEqual(fact["source_engine_type"], "mongodb")
        self.assertEqual(fact["query_names"], {"database": "outdoor", "collection": "routes"})
        self.assertEqual(fact["fields"], [{
            "name": "distance", "path": ["metrics", "distance"], "type": "float",
            "native_type": "double", "nullable": True, "primary_key": False,
            "precision": 10, "scale": 2,
        }])
        self.assertEqual(delta["resources"], [fact])
        self.assertEqual(set(restored["observed"]["resources"]), {locator})
        self.assertNotIn("must-not-persist", checkpoint_prompt(restored))
        result["fields"][0]["path"].append("not-observed")
        self.assertEqual(fact["fields"][0]["path"], ["metrics", "distance"])

    def test_failed_resource_tools_do_not_capture_partial_facts(self):
        for tool in ("resource.children.list", "resource.facts.get"):
            checkpoint = new_checkpoint()
            delta = capture_owner_facts(tool, {
                "error": {"code": "invalid_owner_response"},
                "locator": "addp://engine/99/path/hidden", "children": [], "fields": [],
            }, checkpoint)
            self.assertEqual(delta, {})
            self.assertEqual(checkpoint, new_checkpoint())

    def test_preview_checkpoint_keeps_schema_facts_without_rows(self):
        checkpoint = new_checkpoint()
        locator = "addp://engine/8/path/public/railway?type=table&item_id=60"
        capture_owner_facts(
            "data.preview",
            {
                "preview_type": "table",
                "metadata": {"locator": locator, "full_name": "public.railway", "engine_id": 8},
                "data": {
                    "column_metadata": [{"column_name": "geom", "type": "GEOMETRY(LineString, 32650)"}],
                    "geometry_column": "geom",
                    "source_srid": 32650,
                    "source_crs": "EPSG:32650",
                    "total": 166,
                    "rows": [{"secret_sample": "must-not-persist"}],
                },
            },
            checkpoint,
        )

        fact = checkpoint["observed"]["resources"][locator]
        self.assertEqual(fact["geometry_column"], "geom")
        self.assertEqual(fact["source_crs"], "EPSG:32650")
        self.assertNotIn("rows", fact)

    def test_checkpoint_rejects_total_payload_over_byte_limit(self):
        checkpoint = new_checkpoint()
        checkpoint["observed"]["resources"]["addp://engine/1/path/large"] = {
            "locator": "addp://engine/1/path/large",
            "name": "x" * CHECKPOINT_MAX_BYTES,
        }

        with self.assertRaisesRegex(ValueError, "agent checkpoint exceeds"):
            validate_checkpoint_size(checkpoint)


if __name__ == "__main__":
    unittest.main()

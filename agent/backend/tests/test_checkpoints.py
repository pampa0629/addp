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
                        "name": "GeoPython 工作流引擎",
                        "engine_type": "geopython_workflow",
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

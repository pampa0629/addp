import unittest

from agents.result_refs import build_result_ref


class ResultRefTests(unittest.TestCase):
    def test_workflow_execution_result_uses_manifest_owner_and_schema(self):
        result_ref = build_result_ref("workflow.run", {"execution_id": "execution-123"})

        self.assertEqual(
            result_ref,
            {
                "schema": "addp.result-ref/v1",
                "owner_module": "develop",
                "kind": "execution",
                "ref": "execution:execution-123",
            },
        )

    def test_non_singular_locator_search_is_not_promoted_to_result_ref(self):
        self.assertIsNone(
            build_result_ref(
                "data.search",
                {"results": [{"locator": "addp://engine/8/path/public/railway"}]},
            )
        )

    def test_execution_ref_requires_owner_execution_id(self):
        self.assertIsNone(build_result_ref("workflow.run", {"status": "submitted"}))


if __name__ == "__main__":
    unittest.main()

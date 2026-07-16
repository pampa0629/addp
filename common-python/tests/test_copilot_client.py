import json
import unittest

import httpx

from addp_common.client import CopilotClient


class CopilotClientTests(unittest.IsolatedAsyncioTestCase):
    async def test_generate_workflow_uses_canonical_request(self):
        async def handler(request):
            self.assertEqual(request.url.path, "/api/v1/copilot/workflow/generate")
            self.assertEqual(request.headers["Authorization"], "Bearer user-token")
            self.assertEqual(json.loads(request.content), {
                "query": "分析铁路",
                "workflow_engine_id": 12,
                "resources": [
                    {
                        "role": "railway",
                        "locator": "addp://engine/8/path/public/railway?type=table&item_id=60",
                        "geometry_column": "geom",
                        "crs": "EPSG:32650",
                    }
                ],
            })
            return httpx.Response(200, json={"status": "need_clarification"})

        client = CopilotClient("http://copilot", user_token="user-token")
        await client._client.aclose()
        client._client = httpx.AsyncClient(
            base_url="http://copilot",
            headers={"Authorization": "Bearer user-token"},
            transport=httpx.MockTransport(handler),
        )

        try:
            result = await client.generate_workflow(
                "分析铁路",
                workflow_engine_id=12,
                resources=[
                    {
                        "role": "railway",
                        "locator": "addp://engine/8/path/public/railway?type=table&item_id=60",
                        "geometry_column": "geom",
                        "crs": "EPSG:32650",
                    }
                ],
            )
        finally:
            await client.close()

        self.assertEqual(result, {"status": "need_clarification"})


if __name__ == "__main__":
    unittest.main()

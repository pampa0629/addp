import json
import unittest

import httpx

from addp_common.client import CopilotClient


class CopilotClientTests(unittest.IsolatedAsyncioTestCase):
    async def test_generate_notebook_uses_session_scoped_facts(self):
        async def handler(request):
            self.assertEqual(request.url.path, "/api/v1/copilot/notebook/generate")
            self.assertEqual(json.loads(request.content), {
                "query": "计算铁路缓冲区占用的耕地面积",
                "kernel": "python3",
                "candidates": [],
                "resources": [{
                    "role": "铁路",
                    "candidate_id": "railway-1",
                    "engine_id": 8,
                    "engine_name": "Business PostgreSQL",
                    "engine_type": "postgresql",
                    "name": "railway",
                    "term": "table",
                    "kind": "table",
                    "path": {"version": "catalog.path/v1", "engine_id": 8, "segments": []},
                    "path_names": ["public", "railway"],
                    "path_segments": [],
                }],
            })
            return httpx.Response(200, json={"status": "success", "code": "from addp_common.notebook import engines"})

        client = CopilotClient("http://copilot", user_token="user-token")
        await client._client.aclose()
        client._client = httpx.AsyncClient(
            base_url="http://copilot",
            headers={"Authorization": "Bearer user-token"},
            transport=httpx.MockTransport(handler),
        )
        try:
            result = await client.generate_notebook(
                "计算铁路缓冲区占用的耕地面积",
                resources=[{
                    "role": "铁路",
                    "candidate_id": "railway-1",
                    "engine_id": 8,
                    "engine_name": "Business PostgreSQL",
                    "engine_type": "postgresql",
                    "name": "railway",
                    "term": "table",
                    "kind": "table",
                    "path": {"version": "catalog.path/v1", "engine_id": 8, "segments": []},
                    "path_names": ["public", "railway"],
                    "path_segments": [],
                }],
            )
        finally:
            await client.close()
        self.assertEqual(result["status"], "success")

    async def test_generate_query_uses_canonical_request(self):
        engine_context = {
            "id": 8,
            "name": "Business PostgreSQL",
            "engine_type": "postgresql",
            "capabilities": {"compute": {"query": {"supported": True, "languages": ["sql"]}}},
        }

        async def handler(request):
            self.assertEqual(request.url.path, "/api/v1/copilot/query/generate")
            self.assertEqual(json.loads(request.content), {
                "query": "计算铁路缓冲区占用的耕地面积",
                "engine_id": 8,
                "query_language": "sql",
                "resources": [{
                    "role": "铁路",
                    "locator": "addp://engine/8/path/public/railway?type=table&item_id=60",
                    "engine_id": 8,
                }],
                "engine_context": engine_context,
            })
            return httpx.Response(200, json={"status": "success", "query_language": "sql", "query": "SELECT 1"})

        client = CopilotClient("http://copilot", user_token="user-token")
        await client._client.aclose()
        client._client = httpx.AsyncClient(
            base_url="http://copilot",
            headers={"Authorization": "Bearer user-token"},
            transport=httpx.MockTransport(handler),
        )
        try:
            result = await client.generate_query(
                "计算铁路缓冲区占用的耕地面积",
                engine_id=8,
                query_language="sql",
                resources=[{
                    "role": "铁路",
                    "locator": "addp://engine/8/path/public/railway?type=table&item_id=60",
                    "engine_id": 8,
                }],
                engine_context=engine_context,
            )
        finally:
            await client.close()
        self.assertEqual(result["query"], "SELECT 1")

    async def test_generate_query_forwards_optional_current_query(self):
        engine_context = {
            "id": 11,
            "engine_type": "mongodb",
            "capabilities": {"compute": {"query": {"supported": True, "languages": ["mql"]}}},
        }

        async def handler(request):
            self.assertEqual(json.loads(request.content), {
                "query": "只保留成年人",
                "engine_id": 11,
                "query_language": "mql",
                "resources": [],
                "engine_context": engine_context,
                "current_query": '{"find":"Persons","filter":{},"limit":10}',
            })
            return httpx.Response(200, json={
                "status": "success",
                "query_language": "mql",
                "query": '{"find":"Persons","filter":{"age":{"$gte":18}},"limit":10}',
            })

        client = CopilotClient("http://copilot", user_token="user-token")
        await client._client.aclose()
        client._client = httpx.AsyncClient(
            base_url="http://copilot",
            headers={"Authorization": "Bearer user-token"},
            transport=httpx.MockTransport(handler),
        )
        try:
            result = await client.generate_query(
                "只保留成年人",
                engine_id=11,
                query_language="mql",
                resources=[],
                engine_context=engine_context,
                current_query='{"find":"Persons","filter":{},"limit":10}',
            )
        finally:
            await client.close()
        self.assertEqual(result["query_language"], "mql")

    async def test_generate_query_forwards_optional_resource_scope(self):
        engine_context = {
            "id": 11,
            "engine_type": "mongodb",
            "capabilities": {"compute": {"query": {"supported": True, "languages": ["mql"]}}},
        }
        scope_locator = "addp://engine/11/path/Outdoor?type=database&node_id=276"

        async def handler(request):
            self.assertEqual(json.loads(request.content), {
                "query": "查询用户参加的活动",
                "engine_id": 11,
                "query_language": "mql",
                "resources": [],
                "engine_context": engine_context,
                "resource_scope_locator": scope_locator,
            })
            return httpx.Response(200, json={
                "status": "need_clarification",
                "query_language": "mql",
                "data_source_candidates": [],
            })

        client = CopilotClient("http://copilot", user_token="user-token")
        await client._client.aclose()
        client._client = httpx.AsyncClient(
            base_url="http://copilot",
            headers={"Authorization": "Bearer user-token"},
            transport=httpx.MockTransport(handler),
        )
        try:
            result = await client.generate_query(
                "查询用户参加的活动",
                engine_id=11,
                query_language="mql",
                resources=[],
                engine_context=engine_context,
                resource_scope_locator=scope_locator,
            )
        finally:
            await client.close()
        self.assertEqual(result["status"], "need_clarification")

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

    async def test_generate_transfer_uses_canonical_request(self):
        async def handler(request):
            self.assertEqual(request.url.path, "/api/v1/copilot/transfer/generate")
            self.assertEqual(json.loads(request.content), {
                "query": "把道路传到归档表",
                "resources": [{
                    "role": "道路",
                    "engine_id": 8,
                    "locator": "addp://engine/8/path/public/roads?type=table&item_id=60",
                }],
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
            result = await client.generate_transfer(
                "把道路传到归档表",
                resources=[{
                    "role": "道路",
                    "engine_id": 8,
                    "locator": "addp://engine/8/path/public/roads?type=table&item_id=60",
                }],
            )
        finally:
            await client.close()
        self.assertEqual(result, {"status": "need_clarification"})

    async def test_generate_transfer_scopes_initial_discovery_to_source_engine(self):
        async def handler(request):
            self.assertEqual(json.loads(request.content), {
                "query": "从 pg 到 mysql，同步 farmland",
                "resources": [],
                "source_engine_id": 8,
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
            result = await client.generate_transfer(
                "从 pg 到 mysql，同步 farmland",
                source_engine_id=8,
            )
        finally:
            await client.close()
        self.assertEqual(result, {"status": "need_clarification"})


if __name__ == "__main__":
    unittest.main()

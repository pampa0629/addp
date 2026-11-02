import os
from copy import deepcopy
from datetime import datetime, timedelta, timezone
import unittest
from unittest.mock import patch

from system_client import get_engine, get_execution_engine_access


class FakeResponse:
    def __init__(self, status_code, payload=None, text=""):
        self.status_code = status_code
        self._payload = payload or {}
        self.text = text

    def json(self):
        return self._payload


class FakeTokenSource:
    def __init__(self, tokens):
        self.tokens = list(tokens)
        self.tenant_ids = []
        self.invalidations = []

    def token(self, tenant_id):
        self.tenant_ids.append(tenant_id)
        return self.tokens.pop(0)

    def invalidate(self, tenant_id, token):
        self.invalidations.append((tenant_id, token))


class RuntimeRegistrationTest(unittest.TestCase):
    def test_registration_publishes_runtime_owned_storage_access(self):
        from api_server import register_to_system
        from storage_adapters import STORAGE_ACCESS
        with patch("addp_common.client.register_runtime_engine", return_value=(202, {})) as register:
            self.assertTrue(register_to_system())
        payload = register.call_args.args[3]
        self.assertEqual(payload["capabilities"]["compute"]["workflow"]["storage_access"], STORAGE_ACCESS)
        self.assertEqual(set(STORAGE_ACCESS["write_engine_types"]), {"postgresql", "mysql", "doris", "minio"})
        self.assertTrue({"elasticsearch", "hdfs"}.issubset(STORAGE_ACCESS["read_engine_types"]))
        self.assertNotIn("nfs", STORAGE_ACCESS["read_engine_types"])
        self.assertNotIn("redis", STORAGE_ACCESS["read_engine_types"])


class SystemClientTest(unittest.TestCase):
    def tearDown(self):
        os.environ.pop("SYSTEM_URL", None)

    @patch("system_client._service_token_source")
    @patch("system_client.requests.get")
    def test_get_engine_uses_tenant_service_access_token(self, mock_get, mock_token_source):
        os.environ["SYSTEM_URL"] = "http://system-backend:8180/"
        source = FakeTokenSource(["addp_at_test"])
        mock_token_source.return_value = source
        mock_get.return_value = FakeResponse(200, {"id": 34, "engine_type": "spark"})

        engine = get_engine(34, 7)

        self.assertEqual(engine["id"], 34)
        self.assertEqual(source.tenant_ids, [7])
        mock_get.assert_called_once_with(
            "http://system-backend:8180/api/v1/system/engines/34",
            headers={"Authorization": "Bearer addp_at_test"},
            timeout=10,
        )

    @patch("system_client._service_token_source")
    @patch("system_client.requests.get")
    def test_get_engine_retries_once_after_unauthorized(self, mock_get, mock_token_source):
        source = FakeTokenSource(["addp_at_expired", "addp_at_refreshed"])
        mock_token_source.return_value = source
        mock_get.side_effect = [
            FakeResponse(401, text="expired"),
            FakeResponse(200, {"id": 34, "engine_type": "spark"}),
        ]

        engine = get_engine(34, 7)

        self.assertEqual(engine["id"], 34)
        self.assertEqual(source.tenant_ids, [7, 7])
        self.assertEqual(source.invalidations, [(7, "addp_at_expired")])
        self.assertEqual(mock_get.call_count, 2)

    @patch("system_client._service_token_source")
    @patch("system_client.requests.get")
    def test_get_engine_raises_for_non_success_response(self, mock_get, mock_token_source):
        os.environ["SYSTEM_URL"] = "http://localhost:8180"
        mock_token_source.return_value = FakeTokenSource(["addp_at_test"])
        mock_get.return_value = FakeResponse(404, text="not found")

        with self.assertRaisesRegex(ValueError, "Failed to get engine 99"):
            get_engine(99, 7)

    def test_get_engine_rejects_missing_tenant_context(self):
        with self.assertRaisesRegex(ValueError, "tenant_id must be a positive integer"):
            get_engine(34, 0)

    def test_execution_access_is_bound_to_exact_execution_source_and_consumer(self):
        execution = '9a21ab1a-2900-42a5-ae91-821339b3fcdd'
        payload = {'authorization_id': '8', 'execution_id': execution, 'engine_id': '34',
                   'audience': 'spark', 'effects': ['read'],
                   'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat(),
                   'engine': {'id': 34, 'tenant_id': 7, 'connection_info': {'password': 'private'}}}
        mutations = [({}, True), ({'audience': 'duckdb'}, False), ({'authorization_id': '9'}, False),
                     ({'execution_id': 'other'}, False), ({'engine_id': '35'}, False),
                     ({'effects': ['read', 'write']}, False), ({'engine': {'id': 35, 'tenant_id': 7}}, False),
                     ({'engine': {'id': 34, 'tenant_id': 8}}, False),
                     ({'expires_at': '2000-01-01T00:00:00Z'}, False),
                     ({'expires_at': '2099-01-01T00:00:00'}, False)]
        for change, allowed in mutations:
            with self.subTest(change=change), patch('system_client._service_token_source',
                    return_value=FakeTokenSource(['addp_at_fixture'])), patch('system_client.requests.post') as post:
                body = deepcopy(payload)
                body.update(change)
                post.return_value = FakeResponse(200, body)
                if allowed:
                    self.assertEqual(get_execution_engine_access('8', execution, 34, 7), body)
                else:
                    with self.assertRaisesRegex(ValueError, 'invalid Spark execution'):
                        get_execution_engine_access('8', execution, 34, 7)
                request = post.call_args.kwargs['json']
                self.assertEqual(request, {'execution_id': execution, 'engine_id': '34', 'required_effects': ['read']})
                self.assertNotIn('connection_info', request)

    def test_execution_access_refreshes_only_on_401_and_never_echoes_server_body(self):
        execution = '9a21ab1a-2900-42a5-ae91-821339b3fcdd'
        source = FakeTokenSource(['old', 'new'])
        with patch('system_client._service_token_source', return_value=source), \
             patch('system_client.requests.post', side_effect=[FakeResponse(401), FakeResponse(403, text='private-secret')]) as post:
            with self.assertRaises(ValueError) as error:
                get_execution_engine_access('8', execution, 34, 7)
            self.assertNotIn('private-secret', str(error.exception))
            self.assertEqual(source.invalidations, [(7, 'old')])
            self.assertEqual(post.call_count, 2)

    def test_invalid_execution_binding_does_not_contact_system(self):
        with patch('system_client.requests.post') as post:
            for auth, execution, engine, tenant in [('08', 'bad', 34, 7), ('8', 'bad', 34, 7),
                    ('8', '9a21ab1a-2900-42a5-ae91-821339b3fcdd', True, 7)]:
                with self.assertRaises(ValueError):
                    get_execution_engine_access(auth, execution, engine, tenant)
            post.assert_not_called()




class ClusterMasterConnectionTest(unittest.TestCase):
    def test_uses_explicit_registered_master_port_and_ipv6(self):
        from spark_connector import cluster_master_url
        self.assertEqual(cluster_master_url({'host': 'cluster.example', 'port': 17077, 'web_ui_url': 'http://cluster.example:18088'}), 'spark://cluster.example:17077')
        self.assertEqual(cluster_master_url({'host': '::1', 'port': 7077, 'web_ui_url': 'http://[::1]:18088'}), 'spark://[::1]:7077')

    def test_never_accepts_missing_port_or_legacy_cluster_fields(self):
        from spark_connector import cluster_master_url
        for connection in ({'host': 'cluster', 'web_ui_url': 'http://cluster:8080'},
                           {'host': 'cluster', 'port': 10000, 'master_port': 7077},
                           {'host': 'cluster:7077', 'port': 7077, 'web_ui_url': 'http://cluster:8080'},
                           {'host': 'cluster', 'port': True, 'web_ui_url': 'http://cluster:8080'}):
            with self.assertRaises(ValueError):
                cluster_master_url(connection)

    def test_workflow_session_submits_to_registered_compute_port(self):
        from unittest.mock import MagicMock
        import spark_connector
        builder = MagicMock()
        builder.appName.return_value = builder
        builder.config.return_value = builder
        builder.master.return_value = builder
        builder.getOrCreate.return_value.sparkContext.master = 'spark://cluster.example:17077'
        with patch.object(spark_connector, 'configure_spark_dependencies', return_value=builder), \
             patch.object(spark_connector.SparkSession, '_instantiatedSession', None):
            with spark_connector.invocation_session(19, 7, {'host': 'cluster.example', 'port': 17077, 'web_ui_url': 'http://cluster.example:18088'}) as session:
                self.assertIs(spark_connector.current_session(19, 7), session)
                with self.assertRaises(ValueError):
                    spark_connector.current_session(20, 7)
        builder.getOrCreate.return_value.stop.assert_called_once()
        builder.master.assert_called_once_with('spark://cluster.example:17077')


if __name__ == "__main__":
    unittest.main()

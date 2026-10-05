import copy
import io
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch
from urllib.error import HTTPError

from elasticsearch_adapter import ElasticsearchAdapter
from spark_dependencies import ensure_elasticsearch_jar, configure_spark_dependencies
from storage_adapters import StorageAdapter


class ElasticsearchAdapterTest(unittest.TestCase):
    def setUp(self):
        self.params = {'source_type': 'index', 'index': 'orders.v1', 'array_fields': ['tags'],
                       'connection_info': {'engine_type': 'elasticsearch', 'endpoint': 'http://es:9200',
                                           'user': 'reader', 'password': 'secret'}}
        self.resolved = {'indices': [{'name': 'orders.v1', 'attributes': ['open']}], 'aliases': [], 'data_streams': []}
        self.mapping = {'orders.v1': {'mappings': {'properties': {
            'tags': {'type': 'keyword'}, 'items': {'type': 'nested', 'properties': {'sku': {'type': 'keyword'}}},
            'description': {'type': 'text', 'fields': {'keyword': {'type': 'keyword'}}}}}}}

    def test_literal_index_uses_reader_options_and_does_not_mutate_source(self):
        spark = MagicMock()
        before = copy.deepcopy(self.params)
        with patch.object(ElasticsearchAdapter, '_get', side_effect=[self.resolved, self.mapping]):
            StorageAdapter.load(spark, self.params)
        reader = spark.read.format.return_value
        options = reader.options.call_args.kwargs
        self.assertEqual(options['es.nodes.wan.only'], 'true')
        self.assertEqual(options['es.read.field.as.array.include'], 'tags')
        self.assertEqual(options['es.net.http.auth.pass'], 'secret')
        self.assertEqual(options['es.index.read.allow.red.status'], 'false')
        reader.options.return_value.load.assert_called_once_with('orders.v1')
        self.assertEqual(before, self.params)

    def test_unsafe_endpoints_and_https_fail_before_network_or_spark(self):
        for endpoint in ('https://es:9200', 'http://reader:secret@es:9200', 'http://es/x',
                         'http://es?x=1', 'http://es#frag', 'http://es:0', 'http://es:99999', 'http://es\n'):
            params = copy.deepcopy(self.params); params['connection_info']['endpoint'] = endpoint
            with self.subTest(endpoint=endpoint), patch.object(ElasticsearchAdapter, '_get') as request:
                with self.assertRaises(ValueError): ElasticsearchAdapter.load(MagicMock(), params)
                request.assert_not_called()

    def test_loopback_requires_shared_host_and_does_not_modify_registered_endpoint(self):
        self.params['connection_info']['endpoint'] = 'http://localhost:9200'
        with patch.dict('os.environ', {}, clear=True):
            with self.assertRaises(ValueError): ElasticsearchAdapter._connection(self.params)
        with patch.dict('os.environ', SPARK_WORKFLOW_SHARED_HOST='10.1.2.3'):
            connection = ElasticsearchAdapter._connection(self.params)
        self.assertEqual(connection[:3], ('http://10.1.2.3:9200', '10.1.2.3', 9200))
        self.assertEqual(self.params['connection_info']['endpoint'], 'http://localhost:9200')

    def test_globs_aliases_streams_hidden_closed_and_mapping_mismatch_are_rejected(self):
        for index in ('orders*', 'orders,private', 'orders/_search', '.private', 'Orders', 'orders%2fprivate'):
            with self.subTest(index=index), patch.object(ElasticsearchAdapter, '_get') as request:
                with self.assertRaises(ValueError): ElasticsearchAdapter.load(MagicMock(), dict(self.params, index=index))
                request.assert_not_called()
        for response in (
            {'indices': [{'name': 'other', 'attributes': ['open']}], 'aliases': [{'name': 'orders.v1'}]},
            {'indices': [], 'data_streams': [{'name': 'orders.v1'}]},
            {'indices': [{'name': 'orders.v1', 'attributes': ['open', 'hidden']}]},
            {'indices': [{'name': 'orders.v1', 'attributes': ['closed']}]},
        ):
            with patch.object(ElasticsearchAdapter, '_get', return_value=response):
                with self.assertRaises(ValueError): ElasticsearchAdapter.load(MagicMock(), self.params)
        with patch.object(ElasticsearchAdapter, '_get', side_effect=[self.resolved, {'other': {}}]):
            with self.assertRaises(ValueError): ElasticsearchAdapter.load(MagicMock(), self.params)

    def test_arrays_are_declared_against_source_mapping_without_sampling(self):
        for fields in ('tags', ['missing'], ['tags', 'tags'], ['tags*'], ['items'], ['description.keyword'], [1]):
            with self.subTest(fields=fields), patch.object(ElasticsearchAdapter, '_get', side_effect=[self.resolved, self.mapping]):
                with self.assertRaises(ValueError): ElasticsearchAdapter.load(MagicMock(), dict(self.params, array_fields=fields))

    def test_http_preflight_does_not_forward_credentials_on_redirects(self):
        from elasticsearch_adapter import _NoRedirect
        self.assertIsNone(_NoRedirect().redirect_request(None, None, 302, '', {}, 'http://other'))
        with patch('elasticsearch_adapter.build_opener') as build:
            build.return_value.open.side_effect = HTTPError('http://es', 302, '', {}, None)
            with self.assertRaisesRegex(ValueError, 'HTTP 302'):
                ElasticsearchAdapter._get('http://es', '/_mapping', 'reader', 'secret')


class PinnedConnectorTest(unittest.TestCase):
    def test_dependencies_merge_at_submit_without_overwriting_resolved_jars(self):
        import shlex
        for inherited in (
            'pyspark-shell',
            '--driver-memory 1g --jars "/custom/a jar.jar" pyspark-shell',
            '--jars=/custom/a.jar --conf spark.jars=/custom/b.jar pyspark-shell',
        ):
            with self.subTest(inherited=inherited), patch.dict('os.environ', {'PYSPARK_SUBMIT_ARGS': inherited}), \
                    patch('spark_dependencies.ensure_elasticsearch_jar', return_value=Path('/cache/es connector.jar')):
                builder = MagicMock()
                builder.config.return_value = builder
                self.assertIs(configure_spark_dependencies(builder), builder)
                first = os.environ['PYSPARK_SUBMIT_ARGS']
                configure_spark_dependencies(builder)
                self.assertEqual(os.environ['PYSPARK_SUBMIT_ARGS'], first)
                arguments = shlex.split(first)
                jars = arguments[arguments.index('--jars') + 1].split(',')
                self.assertEqual(jars.count('/cache/es connector.jar'), 1)
                if '/custom/' in inherited:
                    self.assertIn('/custom/a jar.jar' if 'a jar' in inherited else '/custom/a.jar', jars)
                if '/custom/b.jar' in inherited:
                    self.assertIn('/custom/b.jar', jars)
                self.assertEqual(arguments[-1], 'pyspark-shell')
                config = dict(call.args for call in builder.config.call_args_list)
                self.assertNotIn('spark.jars', config)
                self.assertIn('org.apache.sedona:', config['spark.jars.packages'])
                self.assertEqual(config['spark.kryo.registrator'], 'org.apache.sedona.core.serde.SedonaKryoRegistrator')


    def test_verified_artifact_is_cached_and_tampering_never_loads(self):
        import hashlib
        data = b'fixture-complete-jar'
        with tempfile.TemporaryDirectory() as temporary, patch('spark_dependencies.ES_JAR_SHA256', hashlib.sha256(data).hexdigest()), patch('spark_dependencies.urlopen', return_value=io.BytesIO(data)) as download:
            artifact = ensure_elasticsearch_jar(temporary)
            self.assertEqual(artifact, ensure_elasticsearch_jar(temporary))
            download.assert_called_once()
            artifact.write_bytes(b'tampered')
            with self.assertRaisesRegex(ValueError, 'checksum'): ensure_elasticsearch_jar(temporary)
            download.assert_called_once()

    def test_unverified_download_never_creates_a_jar(self):
        with tempfile.TemporaryDirectory() as temporary, patch('spark_dependencies.urlopen', return_value=io.BytesIO(b'wrong')):
            with self.assertRaisesRegex(ValueError, 'checksum'): ensure_elasticsearch_jar(temporary)
            self.assertEqual(list(Path(temporary).iterdir()), [])

"""HTTP authorization/dispatch component tests; real JDBC remains in native T2."""
from copy import deepcopy
from datetime import datetime, timedelta, timezone
from decimal import Decimal
from pathlib import Path
import json
import os
import time
import threading
import unittest
from unittest.mock import AsyncMock, Mock, patch

from addp_common.auth import AuthorizationContext

import api_server
import query_http
from query_process import QueryDriverError
from system_client import ExecutionAccessError


class QueryHTTPTest(unittest.TestCase):
    def setUp(self):
        self.client = api_server.app.test_client()
        self.body = {'target_engine_id': 4,
                     'execution_id': '9a21ab1a-2900-42a5-ae91-821339b3fcdd',
                     'execution_authorization_id': '8', 'source_engine_ids': [11],
                     'query': 'SELECT id FROM Business_PostgreSQL.public.orders',
                     'language': 'sql', 'options': {'read_only': True, 'limit': 10, 'args': [1]}}
        self.context = AuthorizationContext(principal_id=19, principal_type='service_principal',
            token_type='service_access_token', client_id='addp-develop', tenant_id=7)
        self.engines = {
            4: {'id': 4, 'tenant_id': 7, 'engine_type': 'spark', 'name': 'Business Spark',
                'connection_info': {'host': 'cluster', 'port': 7077, 'web_ui_url': 'http://cluster:8080'}},
            11: {'id': 11, 'tenant_id': 7, 'engine_type': 'postgresql', 'name': 'Business PostgreSQL',
                 'connection_info': {'host': 'localhost', 'port': 5433, 'database': 'data',
                                     'user': 'reader', 'password': 'private-source-password'}},
        }
        self.result = {'columns': ['id'], 'fields': [{'name': 'id', 'type': 'bigint', 'nullable': False}],
                       'rows': [{'id': '9007199254740993'}]}
        self.auth = patch.object(query_http, 'resolve_authorization_context', new=AsyncMock(return_value=self.context)).start()
        self.access = patch.object(query_http, 'get_execution_engine_access', side_effect=self.grant).start()
        self.output = deepcopy(self.result)
        self.driver = patch.object(query_http, 'PreparedDriver', side_effect=self.new_driver).start()
        self.addCleanup(self.release_queries)
        self.environment = patch.dict(os.environ, SPARK_WORKFLOW_SHARED_HOST='host.docker.internal').start()
        self.addCleanup(patch.stopall)

    def new_driver(self, *args, **kwargs):
        driver = Mock()
        driver.deadline = time.monotonic() + kwargs['timeout']
        driver.facts = {'engine_read_scope': {'engine_ids': [11]}, 'read_set': {'paths': []}}
        driver.execute.side_effect = lambda: deepcopy(self.output)
        return driver

    def release_queries(self):
        for query_id in list(query_http._queries):
            query_http._release(query_id)

    def grant(self, authorization, execution, engine, tenant, **kwargs):
        self.assertEqual((authorization, execution, tenant), ('8', self.body['execution_id'], 7))
        self.assertGreater(kwargs['timeout'], 0)
        self.assertLessEqual(kwargs['timeout'], 10)
        return {'engine': deepcopy(self.engines[engine]),
                'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat()}

    def post(self, body=None, prepare_only=False, **kwargs):
        headers = {'Authorization': 'Bearer private-caller-token'}
        headers.update(kwargs.pop('headers', {}))
        response = self.client.post('/api/v1/queries', json=self.body if body is None else body,
                                    headers=headers, **kwargs)
        if response.status_code == 201 and not prepare_only:
            return self.client.post('/api/v1/queries/' + response.json['query_id'] + '/execute',
                                    data='{}', content_type='application/json', headers=headers)
        return response

    def test_dispatches_only_authorized_connections_and_rechecks_before_returning_rows(self):
        with patch('system_client.get_engine', side_effect=AssertionError('no engine-wide fallback')):
            response = self.post()
        self.assertEqual(response.status_code, 200, response.json)
        self.assertEqual(response.json, self.result)
        self.assertEqual([call.args[2] for call in self.access.call_args_list], [4, 11, 4, 11, 4, 11, 4, 11])
        command, internal = self.driver.call_args.args
        self.assertTrue(command[1].endswith('/query_driver.py'))
        self.assertNotIn('private-source-password', repr(command))
        self.assertNotIn('private-caller-token', repr(self.driver.call_args))
        self.assertEqual(internal['master_url'], 'spark://cluster:7077')
        self.assertEqual(internal['catalogs'], [{'engine_id': 11, 'engine_type': 'postgresql', 'alias': 'Business_PostgreSQL',
            'url': 'jdbc:postgresql://host.docker.internal:5433/data',
            'driver': 'org.postgresql.Driver', 'user': 'reader', 'password': 'private-source-password'}])

    def test_unauthenticated_user_other_service_and_platform_context_cannot_dispatch(self):
        response = self.client.post('/api/v1/queries', json=self.body)
        self.assertEqual(response.status_code, 401)
        for change in ({'principal_type': 'user'}, {'client_id': 'addp-service'},
                       {'context_type': 'platform'}, {'tenant_id': None}, {'principal_id': None}, {'principal_id': True}, {'token_type': 'oauth_access_token'}):
            self.auth.return_value = AuthorizationContext(**(dict(self.context.__dict__) | change))
            with self.subTest(change=change):
                self.assertEqual(self.post().status_code, 403)
        self.access.assert_not_called()
        self.driver.assert_not_called()

    def test_unknown_fields_connection_injection_writes_and_unadvertised_options_are_rejected(self):
        for change in ({'tenant_id': 7}, {'connection_info': {'password': 'injected'}},
                       {'source_engine_ids': [4]}, {'source_engine_ids': [True]},
                       {'source_engine_ids': [11, 11]}, {'target_engine_id': True},
                       {'object_tables': {'injected': {'table': 'path'}}},
                       {'execution_authorization_id': '08'}, {'language': 'python'},
                       {'options': {'read_only': False}}, {'options': {'read_only': True, 'spatial': True}},
                       {'options': {'read_only': True, 'args': [{'connection_info': {}}]}}):
            with self.subTest(change=change):
                response = self.post(dict(self.body, **change))
                self.assertEqual(response.status_code, 400, response.json)
        self.access.assert_not_called()
        self.driver.assert_not_called()

    def test_revocation_or_changed_connection_discards_all_driver_results(self):
        original = self.grant
        count = 0
        def revoked(*args, **kwargs):
            nonlocal count
            count += 1
            if count > 2:
                raise ExecutionAccessError('private-upstream-content', 403)
            return original(*args, **kwargs)
        self.access.side_effect = revoked
        response = self.post()
        self.assertEqual(response.status_code, 403)
        self.assertNotIn('rows', response.json)
        self.assertNotIn('private-upstream-content', response.get_data(as_text=True))
        self.driver.assert_called_once()

    def test_source_credentials_change_during_driver_execution_discards_rows(self):
        def changed(*args, **kwargs):
            driver = self.new_driver(*args, **kwargs)
            def execute():
                self.engines[11]['connection_info']['password'] = 'rotated-private-password'
                return deepcopy(self.result)
            driver.execute.side_effect = execute
            return driver
        self.driver.side_effect = changed
        response = self.post()
        self.assertEqual(response.status_code, 403)
        self.assertNotIn('rows', response.json)
        self.assertNotIn('rotated-private-password', response.get_data(as_text=True))

    def test_mysql_uses_canonical_user_and_cannot_take_an_ungranted_username(self):
        self.engines[11]['engine_type'] = 'mysql'
        self.engines[11]['connection_info']['username'] = 'unrelated-name'
        response = self.post()
        self.assertEqual(response.status_code, 200, response.json)
        source = self.driver.call_args.args[1]['catalogs'][0]
        self.assertEqual(source['user'], 'reader')
        self.assertTrue(source['url'].startswith('jdbc:mysql://host.docker.internal:'))
        del self.engines[11]['connection_info']['user']
        self.driver.reset_mock()
        self.assertEqual(self.post().status_code, 400)
        self.driver.assert_not_called()

    def test_expired_grants_never_create_a_driver(self):
        original = self.grant
        def expired(*args, **kwargs):
            result = original(*args, **kwargs)
            result['expires_at'] = (datetime.now(timezone.utc) - timedelta(seconds=1)).isoformat()
            return result
        self.access.side_effect = expired
        self.assertEqual(self.post().status_code, 403)
        self.driver.assert_not_called()

    def test_floating_numbers_stay_numbers_while_exact_decimal_and_bigint_stay_strings(self):
        self.output = {'columns': ['ratio', 'amount', 'id'], 'fields': [
            {'name': 'ratio', 'type': 'double', 'nullable': True},
            {'name': 'amount', 'type': 'decimal', 'nullable': True, 'precision': 38, 'scale': 18},
            {'name': 'id', 'type': 'bigint', 'nullable': True}],
            'rows': [{'ratio': Decimal('0.125'), 'amount': '100.123456789012345678', 'id': '9007199254740993'}]}
        response = self.post()
        self.assertEqual(response.status_code, 200, response.json)
        self.assertIs(type(response.json['rows'][0]['ratio']), float)
        self.assertEqual(response.json['rows'][0], {'ratio': 0.125, 'amount': '100.123456789012345678', 'id': '9007199254740993'})
        for malformed in ({'ratio': Decimal('NaN'), 'amount': '1', 'id': '1'}, {'ratio': '0.125', 'amount': '1', 'id': '1'}, {'unexpected': 1}):
            self.output['rows'] = [malformed]
            self.assertEqual(self.post().status_code, 502)

    def test_nested_output_keeps_numeric_wire_types_and_exact_values(self):
        self.output = {'columns': ['nested'], 'fields': [{'name': 'nested', 'type': 'json', 'nullable': True, 'fields': [
            {'name': 'ratios', 'type': 'array', 'nullable': True, 'element': {'name': '', 'type': 'double', 'nullable': True}},
            {'name': 'ids', 'type': 'array', 'nullable': True, 'element': {'name': '', 'type': 'bigint', 'nullable': True}}]}],
            'rows': [{'nested': {'ratios': [Decimal('0.125'), None], 'ids': ['9007199254740993', None]}}]}
        response = self.post()
        self.assertEqual(response.status_code, 200, response.json)
        nested = response.json['rows'][0]['nested']
        self.assertEqual(nested, {'ratios': [0.125, None], 'ids': ['9007199254740993', None]})
        self.assertIs(type(nested['ratios'][0]), float)
        self.assertIs(type(nested['ids'][0]), str)

    def test_query_openapi_describes_the_actual_service_route_and_bounds(self):
        schema = json.loads(Path(__file__).with_name('query_openapi.json').read_text())
        self.assertEqual(set(schema['paths']), {'/api/v1/queries', '/api/v1/queries/{query_id}', '/api/v1/queries/{query_id}/execute'})
        operation = schema['paths']['/api/v1/queries']['post']
        self.assertEqual(operation['x-addp-auth-mode'], 'internal')
        self.assertEqual(set(operation['responses']), {'201', '400', '401', '403', '413', '415', '502', '503', '504'})
        request = schema['components']['schemas']['QueryRequest']
        self.assertFalse(request['additionalProperties'])
        self.assertEqual(set(request['required']), set(self.body))
        self.assertEqual(request['properties']['source_engine_ids']['maxItems'], 16)
        options = request['properties']['options']['properties']
        self.assertEqual(options['read_only']['enum'], [True])
        self.assertEqual(options['limit']['maximum'], 10000)
        result = schema['components']['schemas']['QueryResult']
        self.assertEqual(set(result['required']), set(self.result))
        actual = {(rule.rule.replace('<query_id>', '{query_id}'), method)
                  for rule in api_server.app.url_map.iter_rules() if rule.rule.startswith('/api/v1/queries')
                  for method in rule.methods if method not in {'HEAD', 'OPTIONS'}}
        documented = {(path, method.upper()) for path, methods in schema['paths'].items() for method in methods}
        self.assertEqual(actual, documented)
        for methods in schema['paths'].values():
            for method in methods.values():
                self.assertEqual(method['x-addp-auth-mode'], 'internal')
                self.assertEqual(method['security'], [{'serviceBearer': []}])

    def test_mismatched_target_and_duplicate_source_alias_never_start_driver(self):
        self.engines[4]['engine_type'] = 'duckdb'
        self.assertEqual(self.post().status_code, 400)
        self.driver.assert_not_called()
        self.engines[4]['engine_type'] = 'spark'
        self.engines[12] = dict(deepcopy(self.engines[11]), id=12, name='Business-PostgreSQL')
        self.body['source_engine_ids'].append(12)
        self.assertEqual(self.post().status_code, 400)
        self.driver.assert_not_called()

    def test_timeout_native_rejection_and_busy_are_closed_bilingual_errors(self):
        self.driver.side_effect = QueryDriverError('query_timeout')
        response = self.post(headers={'Accept-Language': 'en-US'})
        self.assertEqual(response.status_code, 504)
        self.assertEqual(response.json, {'error_code': 'query_timeout',
                                        'error': 'The query timed out without returning results.'})
        self.driver.side_effect = self.new_driver
        self.output = {'error': 'query_function_not_supported'}
        self.assertEqual(self.post().json['error_code'], 'query_function_not_supported')
        slots = Mock()
        slots.acquire.return_value = False
        with patch.object(query_http, '_slots', slots):
            response = self.post()
        self.assertEqual(response.status_code, 503)
        slots.release.assert_not_called()

    def test_preparation_returns_only_facts_and_delete_never_executes(self):
        response = self.post(prepare_only=True)
        self.assertEqual(response.status_code, 201, response.json)
        self.assertNotIn('rows', response.json)
        key = response.json['query_id']
        driver = query_http._queries[key]['driver']
        driver.execute.assert_not_called()
        response = self.client.delete('/api/v1/queries/' + key, headers={'Authorization': 'Bearer fixture'})
        self.assertEqual(response.status_code, 204)
        driver.execute.assert_not_called()
        driver.close.assert_called_once()
        self.assertNotIn(key, query_http._queries)
        self.assertEqual(self.client.delete('/api/v1/queries/' + key, headers={'Authorization': 'Bearer fixture'}).status_code, 204)

    def test_missing_column_diagnostic_requires_current_execution_access(self):
        self.output = {'error': 'query_column_not_found'}
        response = self.post(headers={'Accept-Language': 'en'})
        self.assertEqual(response.status_code, 400)
        self.assertEqual(response.json, {'error_code': 'query_column_not_found',
                                         'error': 'A referenced column could not be resolved.'})

        query_id = self.post(prepare_only=True).json['query_id']
        driver = query_http._queries[query_id]['driver']

        def change_source_identity():
            self.engines[11]['connection_info']['port'] = 5434
            return self.output

        driver.execute.side_effect = change_source_identity
        denied = self.client.post(f'/api/v1/queries/{query_id}/execute', data='{}', content_type='application/json',
                                  headers={'Authorization': 'Bearer fixture'})
        self.assertEqual(denied.status_code, 403)
        self.assertEqual(denied.json['error_code'], 'query_permission_denied')
        self.assertNotIn(query_id, query_http._queries)

    def test_retained_plan_is_one_shot_and_bound_to_principal_and_tenant(self):
        key = self.post(prepare_only=True).json['query_id']
        path = '/api/v1/queries/' + key + '/execute'
        self.auth.return_value = AuthorizationContext(**(dict(self.context.__dict__) | {'principal_id': 20}))
        self.assertEqual(self.client.post(path, data='{}', content_type='application/json', headers={'Authorization': 'Bearer fixture'}).status_code, 403)
        self.auth.return_value = self.context
        self.assertEqual(self.client.post(path, data='{}', content_type='application/json', headers={'Authorization': 'Bearer fixture'}).status_code, 200)
        self.assertEqual(self.client.post(path, data='{}', content_type='application/json', headers={'Authorization': 'Bearer fixture'}).status_code, 403)

    def test_expired_waiting_preparation_is_released_without_execute(self):
        closed = threading.Event()
        def expiring(*args, **kwargs):
            driver = self.new_driver(*args, **kwargs)
            driver.deadline = time.monotonic() + .05
            driver.close.side_effect = closed.set
            return driver
        self.driver.side_effect = expiring
        response = self.post(prepare_only=True)
        self.assertEqual(response.status_code, 201)
        self.assertTrue(closed.wait(2), 'retained Driver expiry did not release it')
        self.assertNotIn(response.json['query_id'], query_http._queries)

    def test_native_prepare_rejection_keeps_closed_diagnostic_and_no_retained_session(self):
        before = set(query_http._queries)
        for code in ('query_source_not_supported', 'query_not_read_only', 'query_source_not_authorized'):
            self.driver.side_effect = QueryDriverError(code)
            response = self.post(prepare_only=True)
            self.assertEqual(response.status_code, 400)
            self.assertEqual(response.json['error_code'], code)
            self.assertNotIn('rows', response.json)
            self.assertEqual(set(query_http._queries), before)

    def test_duplicate_json_keys_are_rejected_before_authority_consumption(self):
        response = self.client.post('/api/v1/queries', data='{"target_engine_id":4,"target_engine_id":5}',
            content_type='application/json', headers={'Authorization': 'Bearer test-only'})
        self.assertEqual(response.status_code, 400)
        self.access.assert_not_called()
        self.driver.assert_not_called()


if __name__ == '__main__':
    unittest.main()

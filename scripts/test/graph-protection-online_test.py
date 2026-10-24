import copy
import importlib
import json
import threading
import tempfile
import unittest
from contextlib import redirect_stdout
from http.server import BaseHTTPRequestHandler, HTTPServer
from io import StringIO
from pathlib import Path
from unittest.mock import patch

SUITE = importlib.import_module('scripts.test.graph-protection-online')


def native_edges(ids):
    return [dict(id='link-12', source=ids['p1'], target=ids['p2'], type='Link'),
            dict(id='link-23', source=ids['p2'], target=ids['p3'], type='Link')]


def exploration_fixture(expected):
    ids = expected['native_ids']
    edges = expected['native_edges']
    stages = []
    labels = [('dagre', '分层'), ('circular', '环形'), ('radial', '辐射'), ('force', '力导向')]
    for index, phase in enumerate(['expand-1', 'expand-2', *['layout-'+key for key, _ in labels]]):
        n = 2 if index == 0 else 3
        stage = dict(phase=phase, native_ids={key: ids[key] for key in list(ids)[:n]}, edges=edges[:n-1],
            frame={'width': 800, 'height': 600, 'color': [123, 0, 255], 'fillColor': [123, 0, 255], 'stableFrames': 10,
                   'nodes': [{'x': i*100+index*10, 'y': 100, 'box': [i*100+index*10-10, 90, i*100+index*10+10, 110], 'fillBox': [i*100+index*10-8, 92, i*100+index*10+8, 108], 'pixelCount': 100, 'fillPixelCount': 100} for i in range(1, n+1)]},
            hits=[{'source_id': 'p'+str(i), 'native_id': ids['p'+str(i)], 'x': i*100+index*10, 'y': 100} for i in range(1, n+1)])
        if index < 2:
            stage.update(http_status=200, request={'target': {'kind': 'entity', 'id': ids['p'+str(index+1)]}, 'depth': 1, 'node_limit': 200, 'relationship_limit': 400})
        else: stage.update(layout=labels[index-2][0], selected_label=labels[index-2][1])
        stages.append(stage)
    return dict(expected, schema_version='addp.graph-exploration-stable-frame/v2', stages=stages, native_edges=edges,
        requests=[{'method': 'POST', 'path': '/api/v1/graph/graphs/'+str(expected['graph_id'])+'/expand', 'body': stage['request']} for stage in stages[:2]])


def dense_evidence():
    labels, relations = SUITE.dense_fixture_contract()
    ids = {key: 'native-'+key for key in labels}
    nodes = [{'id': ids[key], 'labels': sorted(value), 'properties': {'id': key, 'name': 'Dense '+key[1:]}} for key, value in labels.items()]
    edges = sorted([dict(id='edge-'+str(index), source=ids[source], target=ids[target], type=kind) for index, (source, target, kind) in enumerate(relations)], key=lambda edge: edge['id'])
    shapes = [dict(name='+'.join(value), labels=list(value)) for value in sorted({tuple(sorted(value)) for value in labels.values()})]
    expected = dict(run_id='run', tenant_id='2', principal_id='4', engine_type='falkordb', engine_id=14, graph_id=8, nodes=nodes, edges=edges, shapes=shapes)
    phases = ['dense-search', 'dense-search-cleared', 'dense-expand', 'dense-filter', 'dense-filter-restored'] + ['dense-layout-'+key for key in ('dagre', 'circular', 'radial', 'force')]
    stages = []
    for index, phase in enumerate(phases):
        members = [node for node in nodes if index != 3 or node['labels'] == ['Employee', 'Person']]
        wanted = {node['id'] for node in members}
        stage = dict(phase=phase, nodes=members, edges=[] if index < 2 else [edge for edge in edges if edge['source'] in wanted and edge['target'] in wanted], groups=[])
        stage['relationship_pixels'] = [dict(type=kind,color=[10+color_index*40,30,80],pixelCount=100,stableFrames=10) for color_index,kind in enumerate(sorted({edge['type'] for edge in stage['edges']}))]
        for color_index, shape in enumerate(shapes):
            group_nodes = [node for node in members if node['labels'] == shape['labels']]
            if not group_nodes: continue
            rgb = [60+color_index*40, 30, 50]
            pixels, hits = [], []
            for position, node in enumerate(group_nodes):
                x, y = 100+position*100+index, 100+color_index*100
                pixels.append(dict(x=x, y=y, box=[x-12,y-12,x+12,y+12], fillBox=[x-10,y-10,x+10,y+10], pixelCount=100, fillPixelCount=100))
                hits.append(dict(source_id=node['properties']['id'], native_id=node['id'], x=x, y=y))
            stage['groups'].append(dict(name=shape['name'], labels=shape['labels'], frame=dict(width=800, height=600,
                color=[250,0,250] if index == 0 else rgb, fillColor=rgb, stableFrames=10, nodes=pixels), hits=hits))
        if index == 0: stage.update(http_status=200, request=dict(query='Dense', limit=30))
        if index == 2: stage.update(http_status=200, request=dict(target=dict(kind='entity',id=ids['n01']),depth=2,node_limit=200,relationship_limit=400))
        if index in (3,4): stage['selected_shapes'] = sorted(group['name'] for group in stage['groups'])
        if index >= 5:
            stage.update(layout=['dagre','circular','radial','force'][index-5], selected_label=['分层','环形','辐射','力导向'][index-5])
        stages.append(stage)
    visual = dict(expected, schema_version='addp.graph-dense-stable-frame/v1', stages=stages,
        requests=[dict(method='POST',path='/api/v1/graph/graphs/8/'+operation,body=stages[index]['request']) for operation,index in [('search',0),('expand',2)]])
    return expected, visual


class GraphProtectionOnlineTest(unittest.TestCase):
    def setUp(self):
        environment = patch.dict(SUITE.os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb'})
        environment.start()
        self.addCleanup(environment.stop)

    def test_service_graph_requires_exact_native_rows_and_no_output_on_protection_failure(self):
        good = {'rows_count': 3, 'columns': ['id', 'name'], 'rows': [
            {'id': 'p1', 'name': 'Fixture one'}, {'id': 'p2', 'name': 'Fixture two'}, {'id': 'p3', 'name': 'Fixture three'}]}
        SUITE.verify_service_graph_result(good)
        for field, value in [('rows_count', 0), ('rows', good['rows'][:2]), ('columns', ['other'])]:
            with self.subTest(field=field), self.assertRaises(SUITE.SuiteError):
                SUITE.verify_service_graph_result(dict(good, **{field: value}))
        denied = {'error': 'service data protection gate is required: protected resource read set is unresolved'}
        SUITE.verify_service_graph_denial(denied)
        for bad in ({'error': 'connection failed'}, dict(denied, rows=[]), dict(denied, graph_data={}), {}):
            with self.subTest(bad=bad), self.assertRaises(SUITE.SuiteError): SUITE.verify_service_graph_denial(bad)

    def test_service_browser_requires_current_identity_original_address_and_actual_evidence(self):
        definition = {'id': 31, 'service_name': 'service_graph_run', 'endpoints': {'execute': 'http://127.0.0.1:8000/api/gquery/service_graph_run'}}
        with tempfile.TemporaryDirectory() as folder:
            environment = {'ADDP_ONLINE_ARTIFACT_DIR': folder, 'ADDP_ONLINE_TEST_RUN_ID': 'graph-run',
                           'GATEWAY_URL': 'http://127.0.0.1:8000', 'CONSOLE_URL': 'http://127.0.0.1:18080',
                           'ADDP_ONLINE_GRAPH_SERVICE_ACCESS_TOKEN': 'service-secret', 'ADDP_ONLINE_GRAPH_ENGINE': 'neo4j'}
            user = {'principal_id': '5', 'permissions': ['service.definition.read'], 'administrator': False}
            def launch(command, **kwargs):
                self.assertIn('ordinary Service graph reader', command)
                self.assertFalse(any(key.endswith('_ACCESS_TOKEN') for key in kwargs['env']))
                expected = json.loads(kwargs['env']['ADDP_ONLINE_SERVICE_GRAPH_EXPECTATIONS'])
                payload = dict(expected, schema_version='addp.service-graph-browser/v1', result='passed',
                               permissions=['service.definition.read'], page_origin=environment['CONSOLE_URL'],
                               page_path='/api/gquery/service_graph_run', dto_address_unchanged=True, user_bearer=True,
                               exact_request=True, result_visible=phase == 'unmanaged', protection_refused=phase == 'protected',
                               browser_errors=0, unexpected_writes=0, release_assets=True, public_https_verified=False)
                payload.update(mutation)
                Path(kwargs['env']['ADDP_ONLINE_SERVICE_GRAPH_REPORT']).write_text(json.dumps(payload))
                if screenshot: Path(folder, f'service-graph-{phase}-console.png').write_bytes(b'test-image')
                return type('Process', (), {'returncode': 0})()
            for phase in ('unmanaged', 'protected'):
                for mutation, screenshot in [({}, True), ({'principal_id': '4'}, True), ({'dto_address_unchanged': False}, True),
                                             ({'public_https_verified': True}, True), ({'exact_request': 1}, True),
                                             ({'protection_refused': phase != 'protected'}, True), ({}, False)]:
                    report = {'identities': {'reader': {'principal_id': '4'}}}
                    with self.subTest(phase=phase, mutation=mutation, screenshot=screenshot), patch.dict(SUITE.os.environ, environment), \
                         patch.object(SUITE, 'identity', return_value=user), patch.object(SUITE.subprocess, 'run', side_effect=launch):
                        if mutation or not screenshot:
                            with self.assertRaises(SUITE.SuiteError): SUITE.run_service_graph_browser(report, definition, 2, phase)
                        else: self.assertTrue(SUITE.run_service_graph_browser(report, definition, 2, phase)['user_bearer'])

    def test_service_graph_phase_checks_auth_state_aliases_and_unmodified_dto_address(self):
        definition = {'id': 31, 'service_name': 'service_graph_run', 'public_access': False, 'endpoints': {'execute': 'http://127.0.0.1:8000/api/gquery/service_graph_run'}}
        rows = {'rows_count': 3, 'columns': ['id', 'name'], 'rows': [
            {'id': 'p1', 'name': 'Fixture one'}, {'id': 'p2', 'name': 'Fixture two'}, {'id': 'p3', 'name': 'Fixture three'}]}
        for phase in ('unmanaged', 'protected'):
            calls = []
            class Client:
                def __init__(self, base_url='', token='', timeout=30): self.token = token
                def request(self, method, path, expected, body=None, **kwargs):
                    calls.append((method, path, expected, body, kwargs))
                    value = definition if method == 'GET' else {}
                    if method == 'PUT': value = dict(definition, public_access=body.get('public_access', False))
                    if method == 'POST' and expected in ((200,), (500,)):
                        value = rows if phase == 'unmanaged' else {'error': 'service data protection gate is required: protected resource read set is unresolved'}
                    return SUITE.SUPPORT.Response(expected[0], value)
            environment = {'GATEWAY_URL': 'http://127.0.0.1:8000', 'SERVICE_URL': 'http://127.0.0.1:8086',
                           'ADDP_ONLINE_GRAPH_SERVICE_ACCESS_TOKEN': 'service-secret'}
            def foreign(report, dto, tenant, body, public):
                return [dict(entry=entry, public_access=public, management_status=404, execution_status=404, result_absent=True)
                        for entry in ('dto_gateway', 'console_http')]
            with patch.dict(SUITE.os.environ, environment), patch.object(SUITE, 'Client', Client), patch.object(SUITE, 'run_service_graph_browser', return_value={}), \
                 patch.object(SUITE, 'verify_service_graph_foreign', side_effect=foreign) as cross_tenant:
                result = SUITE.run_service_graph_phase(Client(), Client(), {}, definition, 2, phase)
                self.assertEqual(result['http_status'], 200 if phase == 'unmanaged' else 500)
                self.assertIs(result['cross_tenant_runtime_verified'], True)
                self.assertEqual(len(result['foreign_checks']), 4)
                self.assertEqual([call.args[-1] for call in cross_tenant.call_args_list], [False, True])
                if phase == 'unmanaged':
                    self.assertEqual([call[2][0] for call in calls if call[0] == 'POST'], [200, 401, 401, 404, 404, 400, 200])
                    self.assertTrue(all(call[4].get('raw_response') for call in calls if call[2] == (404,)))
                with patch.dict(definition['endpoints'], execute='http://wrong.test/api/gquery/service_graph_run'), self.assertRaises(SUITE.SuiteError):
                    SUITE.run_service_graph_phase(Client(), Client(), {}, definition, 2, phase)
                with patch.object(SUITE, 'verify_service_graph_foreign', return_value=[]), self.assertRaises(SUITE.SuiteError):
                    SUITE.run_service_graph_phase(Client(), Client(), {}, definition, 2, phase)
                with patch.dict(definition, public_access=True), self.assertRaises(SUITE.SuiteError):
                    SUITE.run_service_graph_phase(Client(), Client(), {}, definition, 2, phase)

    def test_service_foreign_checks_require_valid_separate_identity_and_both_exact_http_boundaries(self):
        definition = {'id': 31, 'service_name': 'service_graph_run'}
        environment = {'GATEWAY_URL': 'http://127.0.0.1:8000', 'CONSOLE_URL': 'http://127.0.0.1:18080',
                       'ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_TENANT_ID': '3', 'ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_ACCESS_TOKEN': 'foreign-token'}
        user = {'principal_id': '6', 'permissions': ['service.definition.read'], 'administrator': False}
        for public in (False, True):
            for mutation in ('none', '200', '401', '403', '500', 'rows', 'dto', 'unrelated-error'):
                calls = []
                class Client:
                    def __init__(self, origin, token, timeout): self.origin, self.token = origin, token
                    def request(self, method, path, expected, body=None):
                        calls.append((self.origin, self.token, method, path, expected, body))
                        payload = {'error': 'Service not found' if method == 'GET' else "service 'service_graph_run' not found"}
                        if mutation == 'rows': payload['rows'] = []
                        if mutation == 'dto': payload['endpoints'] = {'execute': '/api/gquery/service_graph_run'}
                        if mutation == 'unrelated-error': payload['error'] = 'proxy target not found'
                        return SUITE.SUPPORT.Response(int(mutation) if mutation.isdigit() else 404, payload)
                report = {'identities': {'service': {'principal_id': '5'}}}
                with self.subTest(public=public, mutation=mutation), patch.dict(SUITE.os.environ, environment), \
                     patch.object(SUITE, 'Client', Client), patch.object(SUITE, 'identity', return_value=dict(user)) as identity:
                    if mutation != 'none':
                        with self.assertRaises(SUITE.SuiteError): SUITE.verify_service_graph_foreign(report, definition, 2, {'parameters': {}}, public)
                    else:
                        result = SUITE.verify_service_graph_foreign(report, definition, 2, {'parameters': {}}, public)
                        self.assertEqual(identity.call_args.args[1:], (3, SUITE.SERVICE_READER_PERMISSIONS))
                        self.assertEqual([r['entry'] for r in result], ['dto_gateway', 'console_http'])
                        self.assertTrue(all(r['public_access'] is public and r['result_absent'] is True for r in result))
                        self.assertEqual([c[:5] for c in calls], [
                            (origin, 'foreign-token', method, path, (404,))
                            for origin in ('http://127.0.0.1:8000', 'http://127.0.0.1:18080')
                            for method, path in [('GET', '/api/v1/service/graph/31'), ('POST', '/api/gquery/service_graph_run')]])
                        self.assertEqual(report['identities']['service_foreign']['tenant_id'], '3')
        for value in ('1', '2', '0', 'invalid'):
            with patch.dict(SUITE.os.environ, dict(environment, ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_TENANT_ID=value)), patch.object(SUITE, 'Client') as client:
                with self.assertRaises(SUITE.SuiteError): SUITE.verify_service_graph_foreign({'identities': {}}, definition, 2, {}, False)
                client.assert_not_called()
        for principal, previous in [('5', None), ('7', dict(user, tenant_id='3'))]:
            report = {'identities': {'service': {'principal_id': '5'}}}
            if previous: report['identities']['service_foreign'] = previous
            with patch.dict(SUITE.os.environ, environment), patch.object(SUITE, 'Client') as client, \
                 patch.object(SUITE, 'identity', return_value=dict(user, principal_id=principal)):
                with self.assertRaises(SUITE.SuiteError): SUITE.verify_service_graph_foreign(report, definition, 2, {}, False)
                client.return_value.request.assert_not_called()

    def test_service_browser_failure_still_deletes_the_service_definition(self):
        deleted = []
        class Client:
            next_graph = 0
            def request(self, method, path, expected, body=None):
                if method == 'POST' and path.endswith('/graphs'):
                    self.next_graph += 1
                    return SUITE.SUPPORT.Response(201, {'id': self.next_graph, 'is_public': False})
                if method == 'GET' and path.endswith('/entities/Person'):
                    return SUITE.SUPPORT.Response(200, {'data': [{'id': str(i), 'properties': {'id': 'p'+str(i+1)}} for i in range(3)]})
                if method == 'POST' and path.endswith('/paths'):
                    return SUITE.SUPPORT.Response(200, {'nodes': [{'id': str(i), 'properties': {'id': 'p'+str(i+1)}} for i in range(3)], 'edges': native_edges({'p1':'0','p2':'1','p3':'2'})})
                if method == 'DELETE': deleted.append(path)
                return SUITE.SUPPORT.Response(expected[0], {'id': 31} if path == '/api/v1/service/graph' else {})
        report = {}
        with patch.dict(SUITE.os.environ, ADDP_ONLINE_GRAPH_ENGINE='neo4j', ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID='14', ADDP_ONLINE_TEST_RUN_ID='service-graph-run'), \
             patch.object(SUITE, 'identity', side_effect=[{'principal_id': str(i)} for i in range(3)]), \
             patch.object(SUITE, 'create_ontology', return_value=9), patch.object(SUITE, 'create_schema'), \
             patch.object(SUITE, 'probe'), patch.object(SUITE, 'set_public'), patch.object(SUITE.SUPPORT, 'wait_for_scan'), \
             patch.object(SUITE, 'run_browser', return_value={}), patch.object(SUITE, 'run_develop_browser', return_value={}), \
             patch.object(SUITE, 'run_service_graph_phase', side_effect=SUITE.SuiteError('Service browser failed')):
            with self.assertRaisesRegex(SUITE.SuiteError, 'Service browser failed'):
                SUITE.run(*([Client()] * 7), 13, 2, 0, report)
        self.assertIn('/api/v1/service/graph/31', deleted)
        self.assertEqual(report['cleanup']['domain_resources_remaining'], 0)

    def test_dense_native_contract_rejects_missing_labels_edges_and_direction(self):
        expected, _ = dense_evidence()
        self.assertEqual(len(SUITE.verify_dense_reference(expected['nodes'], expected['edges'])), 12)
        for change in ('label', 'parallel', 'reverse', 'self', 'duplicate'):
            nodes, edges = copy.deepcopy(expected['nodes']), copy.deepcopy(expected['edges'])
            if change == 'label': nodes[3]['labels'] = ['Person']
            elif change == 'parallel': edges = [edge for edge in edges if edge['type'] != 'Parallel']
            elif change == 'reverse': edges[0]['source'], edges[0]['target'] = edges[0]['target'], edges[0]['source']
            elif change == 'self': edges = [edge for edge in edges if edge['source'] != edge['target']]
            else: edges[0]['id'] = edges[1]['id']
            with self.subTest(change=change), self.assertRaises(SUITE.SuiteError): SUITE.verify_dense_reference(nodes, edges)

    def test_dense_raw_frames_require_all_groups_native_hits_and_exact_requests(self):
        expected, visual = dense_evidence()
        SUITE.verify_dense(visual, expected)
        for change in ('identity', 'group', 'hit', 'frames', 'bounds', 'filter', 'edge', 'request', 'layout', 'color', 'relation-pixels', 'stage'):
            raw = copy.deepcopy(visual)
            if change == 'identity': raw['principal_id'] = '99'
            elif change == 'group': raw['stages'][0]['groups'].pop()
            elif change == 'hit': raw['stages'][0]['groups'][0]['hits'][0]['native_id'] = 'wrong'
            elif change == 'frames': raw['stages'][0]['groups'][0]['frame']['stableFrames'] = True
            elif change == 'bounds': raw['stages'][0]['groups'][0]['frame']['nodes'][0]['box'][0] = -10
            elif change == 'filter': raw['stages'][3]['selected_shapes'] = []
            elif change == 'edge': raw['stages'][2]['edges'].pop()
            elif change == 'request': raw['stages'][2]['request']['depth'] = 1
            elif change == 'layout': raw['stages'][-1]['selected_label'] = '环形'
            elif change == 'relation-pixels': raw['stages'][2]['relationship_pixels'][0]['pixelCount'] = True
            elif change == 'color': raw['stages'][0]['groups'][1]['frame']['fillColor'] = raw['stages'][0]['groups'][0]['frame']['fillColor']
            else: raw['stages'].pop()
            with self.subTest(change=change), self.assertRaises(SUITE.SuiteError): SUITE.verify_dense(raw, expected)

    def test_dense_phase_uses_reader_browser_and_cleans_after_success_or_failure(self):
        expected, visual = dense_evidence()
        for failure in ("none", "browser", "identity"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as folder:
                deleted = []
                class Client:
                    def request(self, method, path, status, body=None):
                        if method == 'DELETE': deleted.append(path)
                        if method == 'POST' and path == '/api/v1/graph/ontologies': value = {'id': 9}
                        elif method == 'POST' and path == '/api/v1/graph/graphs': value = {'id': 8, 'is_public': False}
                        elif '/search?' in path: value = dict(total=12, data=expected['nodes'])
                        elif path.endswith('/subgraph'): value = dict(nodes=expected['nodes'], edges=expected['edges'])
                        elif path.endswith('/browse-snapshot'): value = dict(stats=dict(node_count=12,relationship_count=72,by_label=dict(Person=6,Employee=3,Organization=6,Partner=3)),schema=dict(node_shapes=[dict(shape,count=3) for shape in expected['shapes']]))
                        else: value = {}
                        return SUITE.SUPPORT.Response(status[0], value)
                def subprocess_run(command, **kwargs):
                    if 'exploration' in command:
                        self.assertEqual(command[-3:], ['exploration', '--previous-control-graph-id', '2'])
                        return type('Result', (), {'returncode': 0, 'stdout': 'synthetic fixture ready'})()
                    self.assertIn('--config=playwright.online.config.js', command)
                    self.assertFalse(any(key.endswith('_ACCESS_TOKEN') for key in kwargs['env']))
                    actual = json.loads(kwargs['env']['ADDP_ONLINE_GRAPH_DENSE_EXPECTATIONS'])
                    self.assertEqual(actual, expected)
                    Path(folder, 'graph-dense-stable-frame.json').write_text(json.dumps(visual))
                    Path(folder, 'graph-dense-browser-diagnostics.json').write_text(json.dumps(dict(schema_version='addp.graph-browser-diagnostics/v1',entries=[])))
                    for stage in visual['stages']: Path(folder, stage['phase']+'-console.png').write_bytes(b'unit-test')
                    return type('Result', (), {'returncode': 1 if failure == "browser" else 0})()
                environment = dict(ADDP_ONLINE_TEST_RUN_ID='run',ADDP_ONLINE_ARTIFACT_DIR=folder,ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN='secret',ADDP_ONLINE_TEST_USER_USERNAME='reader',ADDP_ONLINE_TEST_USER_PASSWORD='reader-password')
                report = {'identities': {'reader': {'principal_id': '4'}}}
                with patch.dict(SUITE.os.environ, environment), patch.object(SUITE, 'identity', return_value=dict(principal_id='5' if failure == 'identity' else '4')), patch.object(SUITE.subprocess, 'run', side_effect=subprocess_run):
                    if failure == "identity":
                        with self.assertRaisesRegex(SUITE.SuiteError, "original reader"): SUITE.run_dense(Client(), Client(), 2, dict(engine_id=14,catalog_path={}), report, 2)
                    elif failure == "browser":
                        with self.assertRaisesRegex(SUITE.SuiteError, 'Dense Console'): SUITE.run_dense(Client(), Client(), 2, dict(engine_id=14,catalog_path={}), report, 2)
                    else:
                        SUITE.run_dense(Client(), Client(), 2, dict(engine_id=14,catalog_path={}), report, 2)
                        self.assertTrue(report['dense']['passed'])
                self.assertEqual(deleted, ['/api/v1/graph/graphs/8', '/api/v1/graph/ontologies/9'])
                self.assertEqual(report['dense_cleanup']['domain_resources_remaining'], 0)

    def test_native_targets_preserve_catalog_and_reject_shared_neo4j_instance(self):
        for kind, control_id in (("falkordb", 13), ("neo4j", 14)):
            with self.subTest(engine=kind), patch.dict(SUITE.os.environ, {
                'ADDP_ONLINE_GRAPH_ENGINE': kind, 'ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID': str(control_id)
            }):
                target, control = SUITE.target_paths(13)
                self.assertEqual((target['engine_id'], control['engine_id']), (13, control_id))
                self.assertEqual(target['names'], ['neo4j', 'graph'] if kind == 'neo4j' else ['graph_protection_fixture'])
                self.assertEqual(target['catalog_path']['segments'][-1]['name'], target['names'][-1])
        with patch.dict(SUITE.os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'neo4j', 'ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID': '13'}):
            with self.assertRaisesRegex(SUITE.SuiteError, 'independent'): SUITE.target_paths(13)
        with patch.dict(SUITE.os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'unknown'}):
            with self.assertRaises(SUITE.SuiteError): SUITE.target_paths(13)

    def test_develop_browser_requires_its_own_five_permissions_and_both_protection_phases(self):
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            environment = {'ADDP_ONLINE_ARTIFACT_DIR': directory, 'ADDP_ONLINE_TEST_RUN_ID': 'develop-run',
                           'ADDP_ONLINE_GRAPH_DEVELOP_USERNAME': 'developer', 'ADDP_ONLINE_GRAPH_DEVELOP_PASSWORD': 'private-password',
                           'ADDP_ONLINE_GRAPH_DEVELOP_ACCESS_TOKEN': 'develop-secret', 'GATEWAY_URL': 'http://127.0.0.1:8000',
                           'ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN': 'preparer-secret',
                           'ADDP_ONLINE_TEST_USER_PASSWORD': 'graph-password'}
            def launch(command, **kwargs):
                self.assertIn('e2e/online/develop-graph-query.spec.js', command)
                self.assertFalse(any(key.endswith('_ACCESS_TOKEN') for key in kwargs['env']))
                self.assertNotIn('ADDP_ONLINE_TEST_USER_PASSWORD', kwargs['env'])
                self.assertEqual(kwargs['env']['ADDP_ONLINE_GRAPH_DEVELOP_USERNAME'], 'developer')
                expected = json.loads(kwargs['env']['ADDP_ONLINE_DEVELOP_GRAPH_EXPECTATIONS'])
                self.assertEqual(expected, dict(run_id='develop-run', tenant_id='2', principal_id='5', engine_id=13, phase=phase, engine_type=kind, targets=SUITE.target_paths(13)))
                payload = dict(expected, schema_version='addp.develop-graph-query-browser/v1', result='passed',
                               permissions=sorted(SUITE.DEVELOP_PERMISSIONS), graph_nodes=3, graph_relationships=2, graph_rows=2,
                               graph_canvas=True, browser_errors=0, failed_business_responses=0, unexpected_writes=0,
                               anonymous_refresh_401=1, execution_ids=['71a09155-b1ec-4ae4-af2f-6689237a62f9', 'e2429cf6-a201-4ba2-8a0b-cfeff00365ca'])
                names = ('graph', 'table', *('theme-' + mode for mode in ('light', 'dark', 'blue', 'purple'))) if phase == 'unmanaged' else ('protected', 'control')
                if phase == 'unmanaged': payload.update(theme_modes=["light", "dark", "blue", "purple"], theme_canvas_pixels=True,
                    theme_editor_colors=True, theme_completion_colors=True, typed_parameters=True, table_rows=1, table_visible=True, graph_table_visible=True)
                else: payload.update(protection_required=True, protected_result_absent=True, protection_stage='preparation',
                    protection_reason='protection_not_ready' if expected['engine_type'] == 'falkordb' else 'protection_lineage_unverified', control_readable=True)
                payload.update({key: value for key, value in mutation.items() if key != '_omit'})
                for key in mutation.get('_omit', []): payload.pop(key, None)
                Path(kwargs['env']['ADDP_ONLINE_DEVELOP_GRAPH_REPORT']).write_text(json.dumps(payload))
                if screenshots:
                    for name in names: (artifacts / f'develop-graph-{name}-console.png').write_bytes(b'unit-test-screenshot')
                return type('Process', (), {'returncode': 0})()
            for kind, phase in (('falkordb', 'unmanaged'), ('falkordb', 'protected'), ('neo4j', 'unmanaged'), ('neo4j', 'protected')):
                environment.update(ADDP_ONLINE_GRAPH_ENGINE=kind, ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID='14')
                negative = {'typed_parameters': False} if phase == 'unmanaged' else {'protected_result_absent': False}
                cases = [({}, True), ({'principal_id': '4'}, True),
                        ({'permissions': ['graph.graph.read']}, True), ({'browser_errors': 1}, True),
                        (negative, True), ({'execution_ids': ['same', 'same']}, True), ({}, False)]
                if phase == 'protected':
                    cases.extend([({'_omit': ['protection_reason']}, True), ({'protection_reason': 'data_protection_required'}, True),
                                  ({'protection_reason': 'protection_lineage_unverified' if kind == 'falkordb' else 'protection_not_ready'}, True),
                                  ({'protection_reason': 'protection_operation_unsupported'}, True)])
                if phase == 'unmanaged':
                    for key in ('theme_editor_colors', 'theme_completion_colors'):
                        cases.extend([({'_omit': [key]}, True), ({key: False}, True), ({key: None}, True), ({key: 1}, True)])
                for mutation, screenshots in cases:
                    report = {'identities': {'reader': {'principal_id': '4'}}}
                    for path in artifacts.glob('*.png'): path.unlink()
                    with self.subTest(engine=kind, phase=phase, mutation=mutation, screenshots=screenshots), patch.dict(SUITE.os.environ, environment), \
                         patch.object(SUITE, 'identity', return_value={'principal_id': '5', 'permissions': sorted(SUITE.DEVELOP_PERMISSIONS), 'administrator': False}) as identity, \
                         patch.object(SUITE.subprocess, 'run', side_effect=launch):
                        if mutation or not screenshots:
                            with self.assertRaises(SUITE.SuiteError): SUITE.run_develop_browser(report, 13, 2, phase)
                        else:
                            self.assertEqual(SUITE.run_develop_browser(report, 13, 2, phase)['principal_id'], '5')
                            self.assertEqual(identity.call_args.args[1:], (2, SUITE.DEVELOP_PERMISSIONS))

    def test_develop_does_not_reuse_the_graph_reader_or_change_between_phases(self):
        environment = {'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_GRAPH_DEVELOP_ACCESS_TOKEN': 'develop-secret'}
        for previous, current in ((None, '4'), ('5', '6')):
            report = {'identities': {'reader': {'principal_id': '4'}}}
            if previous: report['identities']['develop'] = {'principal_id': previous}
            with self.subTest(previous=previous, current=current), patch.dict(SUITE.os.environ, environment), \
                 patch.object(SUITE, 'identity', return_value={'principal_id': current}), patch.object(SUITE.subprocess, 'run') as launch:
                with self.assertRaises(SUITE.SuiteError): SUITE.run_develop_browser(report, 13, 2, 'protected')
                launch.assert_not_called()

    def test_browser_requires_same_reader_real_ui_evidence_and_clean_diagnostics(self):
        api_report = {'identities': {'reader': {'principal_id': '4'}}}
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            environment = {'ADDP_ONLINE_ARTIFACT_DIR': directory, 'ADDP_ONLINE_TEST_RUN_ID': 'graph-run',
                           'ADDP_ONLINE_TEST_USER_USERNAME': 'reader', 'ADDP_ONLINE_TEST_USER_PASSWORD': 'private-password',
                           'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'reader-secret',
                           'ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN': 'preparer-secret',
                           'ADDP_ONLINE_GRAPH_OUTSIDER_ACCESS_TOKEN': 'outsider-secret'}
            def launch(command, **kwargs):
                self.assertIn('e2e/online/graph-protection.spec.js', command)
                self.assertEqual(kwargs['cwd'].name, 'frontend')
                self.assertFalse(any(key.endswith('_ACCESS_TOKEN') for key in kwargs['env']))
                expected = json.loads(kwargs['env']['ADDP_ONLINE_GRAPH_BROWSER_EXPECTATIONS'])
                self.assertEqual(expected, {'run_id': 'graph-run', 'tenant_id': '2', 'principal_id': '4', 'engine_id': 13, 'graph_id': 17, 'engine_type': 'falkordb', 'native_ids': {'p1': 'native-1', 'p2': 'native-2', 'p3': 'native-3'}, 'native_edges': native_edges({'p1': 'native-1', 'p2': 'native-2', 'p3': 'native-3'})})
                payload = dict(expected, schema_version='addp.graph-protection-browser/v7', result='passed',
                               permissions=['graph.graph.read'], node_count=3, relationship_count=2,
                               browse_snapshot=True, canvas_visible=True, native_search=True, knowledge_search=True, search_stable_visible=True, search_node_hits=3,
                               expansion_stable_visible=True, layout_stable_visible=True, layout_modes=["dagre", "circular", "radial", "force"],
                               public_switch_disabled=True, graph_writes=0, failed_business_responses=0,
                               browser_errors=0, anonymous_refresh_401=1, release_assets=True, deep_link_reload=True,
                               theme_modes=['light', 'dark', 'blue', 'purple'], theme_canvas_pixels=True, theme_typography=True)
                payload.update({key: value for key, value in mutation.items() if not key.startswith('_')})
                for key in mutation.get('_omit', []): payload.pop(key, None)
                visual = dict(expected, schema_version='addp.graph-search-stable-frame/v2',
                    frame={'width': 800, 'height': 600, 'color': [245, 108, 108], 'fillColor': [84, 141, 255], 'stableFrames': 10,
                           'nodes': [{'x': x, 'y': 100, 'box': [x-10, 90, x+10, 110], 'fillBox': [x-8, 92, x+8, 108], 'pixelCount': 100, 'fillPixelCount': 100} for x in (100, 200, 300)]},
                    hits=[{'source_id': 'p'+str(i), 'native_id': 'native-'+str(i), 'x': i*100, 'y': 100} for i in (1, 2, 3)])
                visual.update(mutation.get('_visual', {}))
                visual['frame'].update(mutation.get('_frame', {}))
                for node in visual['frame'].get('nodes', []): node.update(mutation.get('_node', {}))
                for hit in visual.get('hits', []): hit.update(mutation.get('_hit', {}))
                if not mutation.get('_missing_visual'):
                    (artifacts / 'graph-search-stable-frame.json').write_text(json.dumps(visual))
                exploration = exploration_fixture(expected)
                exploration.update(mutation.get('_exploration', {}))
                if not mutation.get('_missing_exploration'):
                    (artifacts / 'graph-exploration-stable-frame.json').write_text(json.dumps(exploration))
                Path(kwargs['env']['ADDP_ONLINE_GRAPH_BROWSER_REPORT']).write_text(json.dumps(payload))
                diagnostics = mutation.get('_diagnostics', {'schema_version': 'addp.graph-browser-diagnostics/v1', 'entries': []})
                if diagnostics is not None:
                    (artifacts / 'graph-browser-diagnostics.json').write_text(json.dumps(diagnostics))
                if screenshots:
                    for name in ('graph-private-browse-console.png', 'graph-private-knowledge-console.png'):
                        (artifacts / name).write_bytes(b'unit-test-screenshot')
                    for mode in (('light', 'dark', 'blue') if screenshots == 'partial' else ('light', 'dark', 'blue', 'purple')):
                        (artifacts / f'graph-private-theme-{mode}.png').write_bytes(b'unit-test-screenshot')
                    for phase in ('expand-1', 'expand-2', 'layout-dagre', 'layout-circular', 'layout-radial', 'layout-force'):
                        if phase == 'layout-force' and mutation.get('_missing_layout_png'): continue
                        (artifacts / f'graph-{phase}-console.png').write_bytes(b'unit-test-screenshot')
                return type('Process', (), {'returncode': 0})()
            for mutation, screenshots in (({}, True), ({'principal_id': 'other'}, True),
                    ({'_missing_visual': True}, True), ({'_missing_exploration': True}, True), ({'_missing_layout_png': True}, True),
                    ({'expansion_stable_visible': 1}, True), ({'layout_stable_visible': False}, True), ({'_omit': ['expansion_stable_visible']}, True),
                    ({'layout_modes': ['force']}, True), ({'schema_version': 'addp.graph-protection-browser/v4'}, True), ({'schema_version': 'addp.graph-protection-browser/v5'}, True), ({'schema_version': 'addp.graph-protection-browser/v6'}, True), ({'_omit': ['native_edges']}, True), ({'_visual': {'native_edges': []}}, True), ({'_exploration': {'native_edges': []}}, True), ({'_exploration': {'stages': []}}, True), ({'_omit': ['search_stable_visible']}, True),
                    ({'search_stable_visible': False}, True), ({'search_stable_visible': 1}, True), ({'search_node_hits': 2}, True), ({'_visual': {'principal_id': 'other'}}, True),
                    ({'_visual': {'hits': []}}, True), ({'_frame': {'stableFrames': 1}}, True),
                    ({'_frame': {'stableFrames': True}}, True), ({'_frame': {'width': 0}}, True),
                    ({'_node': {'fillBox': [0, 0, 1, 1]}}, True), ({'_node': {'fillBox': [95, 99, 105, 101]}}, True), ({'_node': {'fillPixelCount': 0}}, True), ({'_node': {'fillPixelCount': True}}, True), ({'_frame': {'fillColor': [245, 108, 108]}}, True), ({'_frame': {'fillColor': None}}, True), ({'_visual': {'schema_version': 'addp.graph-search-stable-frame/v1'}}, True), ({'_exploration': {'schema_version': 'addp.graph-exploration-stable-frame/v1'}}, True),
                    ({'_node': {'box': [-20, 90, 110, 110]}}, True), ({'_node': {'pixelCount': 0}}, True),
                    ({'_hit': {'native_id': 'wrong-node'}}, True), ({'_hit': {'source_id': 'p1'}}, True), ({'_visual': {'frame': {}}}, True),
                    ({'permissions': ['graph.graph.read', 'graph.graph.update']}, True),
                    ({'public_switch_disabled': False}, True), ({'browser_errors': 1}, True),
                    ({'release_assets': False}, True), ({'deep_link_reload': False}, True),
                    ({'theme_modes': ['dark']}, True), ({'theme_canvas_pixels': False}, True), ({'theme_typography': False}, True),
                    ({'schema_version': 'addp.graph-protection-browser/v1'}, True),
                    ({'schema_version': 'addp.graph-protection-browser/v2'}, True),
                    ({'schema_version': 'addp.graph-protection-browser/v3'}, True),
                    ({'_diagnostics': None}, True), ({'_diagnostics': {'schema_version': 'addp.graph-browser-diagnostics/v1', 'entries': [{'type': 'warning'}]}}, True),
                    ({}, False), ({}, 'partial')):
                for path in artifacts.glob('*.png'): path.unlink()
                with self.subTest(mutation=mutation, screenshots=screenshots), patch.dict(SUITE.os.environ, environment), \
                     patch.object(SUITE.subprocess, 'run', side_effect=launch):
                    if mutation or screenshots is not True:
                        with self.assertRaises(SUITE.SuiteError): SUITE.run_browser(api_report, 17, 13, 2, {'p1': 'native-1', 'p2': 'native-2', 'p3': 'native-3'}, native_edges({'p1': 'native-1', 'p2': 'native-2', 'p3': 'native-3'}))
                    else:
                        self.assertEqual(SUITE.run_browser(api_report, 17, 13, 2, {'p1': 'native-1', 'p2': 'native-2', 'p3': 'native-3'}, native_edges({'p1': 'native-1', 'p2': 'native-2', 'p3': 'native-3'}))['principal_id'], '4')

    def test_native_path_reference_rejects_aggregate_edges_and_changed_native_identity(self):
        ids = {'p1': '4:node:1', 'p2': '4:node:2', 'p3': '4:node:3'}
        original = {'nodes': [{'id': value, 'properties': {'id': key}} for key, value in ids.items()],
                    'edges': native_edges(ids)}
        class Reader:
            def request(reader, method, path, statuses, body):
                self.assertEqual((method, path, statuses), ('POST', '/api/v1/graph/kg/17/paths', (200,)))
                self.assertEqual(body, {'source_node_id': ids['p1'], 'target_node_id': ids['p3'], 'max_hops': 3})
                return SUITE.SUPPORT.Response(200, candidate)
        mutations = [lambda v: v.update(edges=[]), lambda v: v['edges'].pop(),
            lambda v: v['edges'][0].update(source='aggregate-person'),
            lambda v: v['edges'][0].update(type='Other'), lambda v: v['edges'][0].update(id=''),
            lambda v: v['edges'][0].update(id=v['edges'][1]['id']),
            lambda v: v['nodes'][0].update(id='another-native-node')]
        candidate = copy.deepcopy(original)
        self.assertEqual(SUITE.native_path_edges(Reader(), 17, ids), native_edges(ids))
        for mutate in mutations:
            candidate = copy.deepcopy(original); mutate(candidate)
            with self.subTest(mutate=mutate), self.assertRaises(SUITE.SuiteError):
                SUITE.native_path_edges(Reader(), 17, ids)

    def test_exploration_rejects_wrong_geometry_ids_requests_and_incomplete_layouts(self):
        expected = {'run_id': 'r', 'tenant_id': '2', 'principal_id': '4', 'graph_id': 17, 'engine_id': 13, 'engine_type': 'neo4j',
                    'native_ids': {'p1': '4:node:1', 'p2': '4:node:2', 'p3': '4:node:3'}}
        expected['native_edges'] = native_edges(expected['native_ids'])
        valid = exploration_fixture(expected)
        SUITE.verify_exploration(valid, expected)
        mutations = [lambda v: v.update(principal_id='other'), lambda v: v.update(stages=v['stages'][:-1]),
            lambda v: v['stages'][0]['request'].update(depth=2), lambda v: v['stages'][0].update(http_status=500),
            lambda v: v['stages'][2].update(selected_label='环形'), lambda v: v['stages'][2].update(layout='force'),
            lambda v: v['stages'][2]['frame'].update(stableFrames=True),
            lambda v: v['stages'][2]['frame'].update(fillColor=[0, 0, 0]),
            lambda v: v['stages'][2]['frame']['nodes'][0].update(fillPixelCount=0),
            lambda v: v['stages'][2]['frame']['nodes'][0].update(box=[0, 90, 110, 110]),
            lambda v: v['stages'][2]['hits'][0].update(native_id='wrong'),
            lambda v: v['stages'][0]['native_ids'].update(p3=expected['native_ids']['p3']),
            lambda v: v['native_edges'][0].update(target=expected['native_ids']['p3']),
            lambda v: v['native_edges'][0].update(id='changed-relation'),
            lambda v: v['requests'].append({'method': 'GET', 'path': '/extra', 'body': None}),
            lambda v: v['stages'][3].update(frame=copy.deepcopy(v['stages'][2]['frame']), hits=copy.deepcopy(v['stages'][2]['hits']))]
        for mutate in mutations:
            candidate = copy.deepcopy(valid); mutate(candidate)
            with self.subTest(mutate=mutate), self.assertRaises(SUITE.SuiteError): SUITE.verify_exploration(candidate, expected)

    def test_main_emits_the_standard_dispatcher_report_without_credentials(self):
        dispatcher = importlib.import_module('scripts.test.online-gate')
        with tempfile.TemporaryDirectory() as directory:
            environment = {'GITHUB_ACTIONS': 'true', 'ADDP_ONLINE_HOSTED': '1',
                           'ADDP_ONLINE_ARTIFACT_DIR': directory, 'GATEWAY_URL': 'http://127.0.0.1:8000',
                           'GRAPH_URL': 'http://127.0.0.1:8186', 'ADDP_ONLINE_TEST_ENGINE_ID': '13',
                           'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'reader-secret',
                           'ADDP_ONLINE_GRAPH_OUTSIDER_ACCESS_TOKEN': 'outsider-secret',
                           'ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN': 'preparer-secret'}
            for failure in (False, True):
                stdout = StringIO()
                with self.subTest(failure=failure), patch.dict(SUITE.os.environ, environment), \
                     patch.object(SUITE, 'run', side_effect=SUITE.SuiteError('expected failure') if failure else None), redirect_stdout(stdout):
                    self.assertEqual(SUITE.main(), 1 if failure else 0)
                report = dispatcher._parse_stage_report('scenario', stdout.getvalue())
                self.assertEqual(report['passed'], not failure)
                self.assertEqual(report, json.loads((Path(directory) / 'graph-protection.json').read_text()))
                for secret in ('reader-secret', 'outsider-secret', 'preparer-secret'):
                    self.assertNotIn(secret, stdout.getvalue())

    def test_browser_failure_still_closes_and_deletes_both_synthetic_graphs(self):
        deleted, closed = [], []
        class Client:
            next_graph = 0
            def request(self, method, path, expected, body=None):
                if method == 'POST' and path.endswith('/graphs'):
                    self.next_graph += 1
                    return SUITE.SUPPORT.Response(201, {'id': self.next_graph, 'is_public': False})
                if method == 'GET' and path.endswith('/entities/Person'):
                    return SUITE.SUPPORT.Response(200, {'data': [
                        {'id': str(i), 'properties': {'id': 'p' + str(i + 1)}} for i in range(3)]})
                if method == 'POST' and path.endswith('/paths'):
                    return SUITE.SUPPORT.Response(200, {'nodes': [{'id': str(i), 'properties': {'id': 'p'+str(i+1)}} for i in range(3)],
                        'edges': native_edges({'p1': '0', 'p2': '1', 'p3': '2'})})
                if method == 'DELETE': deleted.append(path)
                return SUITE.SUPPORT.Response(expected[0], {})
        report = {}
        identities = [{'principal_id': str(i)} for i in range(3)]
        with patch.object(SUITE, 'identity', side_effect=identities), patch.object(SUITE, 'create_ontology', return_value=9), \
             patch.object(SUITE, 'create_schema'), patch.object(SUITE, 'probe'), \
             patch.object(SUITE, 'set_public', side_effect=lambda client, gid, public: closed.append((gid, public))), \
             patch.object(SUITE, 'run_browser', side_effect=SUITE.SuiteError('browser failed')):
            with self.assertRaisesRegex(SUITE.SuiteError, 'browser failed'):
                SUITE.run(*([Client()] * 7), 13, 2, 0, report)
        self.assertEqual(closed, [(1, False), (2, False)])
        self.assertEqual(deleted, ['/api/v1/graph/graphs/1', '/api/v1/graph/graphs/2', '/api/v1/graph/ontologies/9'])
        self.assertEqual(report['cleanup']['domain_resources_remaining'], 0)

    def test_develop_browser_failure_still_closes_and_deletes_all_graph_domain_resources(self):
        deleted, closed = [], []
        class Client:
            next_graph = 0
            def request(self, method, path, expected, body=None):
                if method == 'POST' and path.endswith('/graphs'):
                    self.next_graph += 1
                    return SUITE.SUPPORT.Response(201, {'id': self.next_graph, 'is_public': False})
                if method == 'GET' and path.endswith('/entities/Person'):
                    return SUITE.SUPPORT.Response(200, {'data': [
                        {'id': str(i), 'properties': {'id': 'p' + str(i + 1)}} for i in range(3)]})
                if method == 'POST' and path.endswith('/paths'):
                    return SUITE.SUPPORT.Response(200, {'nodes': [{'id': str(i), 'properties': {'id': 'p'+str(i+1)}} for i in range(3)],
                        'edges': native_edges({'p1': '0', 'p2': '1', 'p3': '2'})})
                if method == 'DELETE': deleted.append(path)
                return SUITE.SUPPORT.Response(expected[0], {})
        report = {}
        with patch.object(SUITE, 'identity', side_effect=[{'principal_id': str(i)} for i in range(3)]), \
             patch.object(SUITE, 'create_ontology', return_value=9), patch.object(SUITE, 'create_schema'), \
             patch.object(SUITE, 'probe'), patch.object(SUITE.SUPPORT, 'wait_for_scan'), \
             patch.object(SUITE, 'set_public', side_effect=lambda client, gid, public: closed.append((gid, public))), \
             patch.object(SUITE, 'run_dense', return_value=None), patch.object(SUITE, 'run_browser', return_value={}), \
             patch.object(SUITE, 'run_develop_browser', side_effect=SUITE.SuiteError('Develop browser failed')):
            with self.assertRaisesRegex(SUITE.SuiteError, 'Develop browser failed'):
                SUITE.run(*([Client()] * 7), 13, 2, 0, report)
        self.assertEqual(closed, [(1, False), (2, False)])
        self.assertEqual(deleted, ['/api/v1/graph/graphs/1', '/api/v1/graph/graphs/2', '/api/v1/graph/ontologies/9'])
        self.assertEqual(report['cleanup']['domain_resources_remaining'], 0)

    def progress(self):
        return {'state': 'enrolling', 'owner_progress': [
            {'consumer_owner': owner, 'projection_id': owner, 'revision': '00000000000000000001',
             'published_cursor': owner + '-cursor', 'acknowledged': True, 'validity': 'valid'}
            for owner in sorted(SUITE.OWNERS)]}

    def test_five_owner_acknowledgements_are_mandatory(self):
        self.assertTrue(SUITE.converged(self.progress()))
        for owner in SUITE.OWNERS:
            value = self.progress()
            next(p for p in value['owner_progress'] if p['consumer_owner'] == owner)['acknowledged'] = False
            self.assertFalse(SUITE.converged(value))
        for change in ('missing', 'duplicate', 'expired', 'released'):
            value = self.progress()
            if change == 'missing': value['owner_progress'].pop()
            if change == 'duplicate': value['owner_progress'][-1] = value['owner_progress'][0]
            if change == 'expired': value['owner_progress'][0]['validity'] = 'expired'
            if change == 'released': value['state'] = 'released'
            if change in {'expired'}:
                self.assertFalse(SUITE.converged(value))
            else:
                with self.assertRaises(SUITE.SuiteError): SUITE.converged(value)

    def test_activating_waits_for_the_formal_security_installation_barrier(self):
        activating = self.progress(); activating['state'] = 'activating'
        self.assertFalse(SUITE.converged(activating))
        class Client:
            def __init__(self): self.responses = iter([activating, activating, self_ready])
            def request(self, *args): return SUITE.SUPPORT.Response(200, next(self.responses))
        self_ready = self.progress()
        with patch.object(SUITE.time, 'monotonic', return_value=0), patch.object(SUITE.time, 'sleep') as sleep:
            self.assertEqual(SUITE.wait_owners(Client(), 'enrollment', 10), self_ready)
        self.assertEqual(sleep.call_count, 2)

    def test_cleanup_error_never_hides_the_original_operation_failure(self):
        class Client:
            def request(self, method, path, expected, body=None):
                if method == 'POST': return SUITE.SUPPORT.Response(201, {'id': 9})
                raise SUITE.SuiteError('ontology cleanup failed')
        report = {}
        with patch.object(SUITE, 'identity', side_effect=[{'principal_id': str(i)} for i in range(3)]), \
             patch.object(SUITE, 'create_schema', side_effect=SUITE.SuiteError('initial operation failed')):
            with self.assertRaisesRegex(SUITE.SuiteError, 'initial operation failed'):
                SUITE.run(*([Client()] * 7), 13, 2, 0, report)
        self.assertEqual(report['failure'], {'error_type': 'SuiteError', 'message': 'initial operation failed'})
        self.assertEqual(report['cleanup']['errors'], ['delete synthetic ontology'])
        self.assertIsNone(report['cleanup']['domain_resources_remaining'])

    def test_release_requires_all_owners_and_released_validity(self):
        value = self.progress(); value['state'] = 'released'
        self.assertFalse(SUITE.converged(value, True))
        for p in value['owner_progress']: p['validity'] = 'released'
        self.assertTrue(SUITE.converged(value, True))
        value['owner_progress'][0]['acknowledged'] = False
        self.assertFalse(SUITE.converged(value, True))

    def test_exact_minimum_user_permissions_reject_admin_or_extra_authority(self):
        context = {'principal': {'id': '4', 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                   'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                       {'role_key': 'online.graph_reader', 'permissions': ['graph.graph.read']}]}}
        class Client:
            def request(self, *args): return SUITE.SUPPORT.Response(200, context)
        self.assertFalse(SUITE.identity(Client(), 2, SUITE.READER_PERMISSIONS)['administrator'])
        for mutation in ('extra', 'admin', 'tenant', 'service'):
            saved = copy.deepcopy(context)
            if mutation == 'extra': context['authorization']['role_assignments'][0]['permissions'].append('graph.graph.update')
            if mutation == 'admin': context['authorization']['role_assignments'][0]['role_key'] = 'tenant.administrator'
            if mutation == 'tenant': context['context']['tenant_id'] = '1'
            if mutation == 'service': context['principal']['type'] = 'service'
            with self.assertRaises(SUITE.SuiteError): SUITE.identity(Client(), 2, SUITE.READER_PERMISSIONS)
            context.clear(); context.update(saved)

    def test_anonymous_transport_does_not_send_empty_bearer(self):
        observed = []
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                observed.append(self.headers.get('Authorization'))
                self.send_response(200); self.end_headers(); self.wfile.write(b'{}')
            def log_message(self, *args): pass
        server = HTTPServer(('127.0.0.1', 0), Handler)
        self.addCleanup(server.server_close)
        worker = threading.Thread(target=server.serve_forever); worker.start()
        try:
            for token in ('', 'fixture-token'):
                SUITE.Client(f'http://127.0.0.1:{server.server_port}', token, 5).request('GET', '/', (200,))
        finally:
            server.shutdown(); worker.join()
        self.assertEqual(observed, [None, 'Bearer fixture-token'])

    def test_denial_must_identify_security_and_never_include_data(self):
        ids = {'p1': '0', 'p3': '2'}
        class Client:
            payload = {'error': 'data protection required'}
            def request(self, *args): return SUITE.SUPPORT.Response(403, self.payload)
        client = Client()
        self.assertEqual(len(SUITE.probe(client, 1, ids, 403, 'protection')), 8)
        for payload in ({'error': 'permission denied'}, {'error': 'data protection required', 'data': []},
                        {'error': 'data protection required', 'nodes': []}):
            client.payload = payload
            with self.assertRaises(SUITE.SuiteError): SUITE.probe(client, 1, ids, 403, 'protection')

    def test_successful_status_does_not_hide_empty_or_wrong_native_data(self):
        class Client:
            payload = {}
            def request(self, *args): return SUITE.SUPPORT.Response(200, self.payload)
        client = Client()
        for payload in ({}, {'data': [], 'total': 0}, {'data': [1, 2], 'total': 3}):
            client.payload = payload
            with self.assertRaises(SUITE.SuiteError): SUITE.probe(client, 1, {'p1': '0', 'p3': '2'}, 200)

    def test_cleanup_still_deletes_ontology_after_schema_preparation_failure(self):
        from unittest.mock import patch
        deleted = []
        class Client:
            def request(self, method, path, expected, body=None):
                if method == 'POST' and path.endswith('/ontologies'): return SUITE.SUPPORT.Response(201, {'id': 9})
                if method == 'POST': raise SUITE.SuiteError('schema failed')
                if method == 'DELETE': deleted.append(path); return SUITE.SUPPORT.Response(200, {})
                return SUITE.SUPPORT.Response(404, {})
        report = {}
        identities = [{'principal_id': str(i)} for i in range(3)]
        with patch.object(SUITE, 'identity', side_effect=identities):
            with self.assertRaisesRegex(SUITE.SuiteError, 'schema failed'):
                SUITE.run(*([Client()] * 7), 13, 2, 0, report)
        self.assertEqual(deleted, ['/api/v1/graph/ontologies/9'])
        self.assertEqual(report['cleanup']['domain_resources_remaining'], 0)


if __name__ == '__main__': unittest.main()

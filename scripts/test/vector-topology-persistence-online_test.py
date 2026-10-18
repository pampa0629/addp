import copy
import importlib
from pathlib import Path
from types import SimpleNamespace
import unittest

m = importlib.import_module('scripts.test.vector-topology-persistence-online')
base = importlib.import_module('scripts.test.raster-contour-persistence-online_test')


def execution():
    source = 'addp://engine/3/path/source.geojson?type=file&item_id=10'
    outputs = {key: f'addp://engine/3/path/outputs/{name}?type=file' for key, name in zip(('faces','diagnostics'), m.fixture.TARGETS)}
    facts = {'schema_version': 'addp.lineage-facts/v1', 'inputs': [{'locator': source}], 'outputs': [{'locator': uri} for uri in outputs.values()]}
    return {'status': 'success', 'outputs': {'save_'+key: {'resource': {'locator': uri, 'type': 'file'}} for key, uri in outputs.items()},
        'metadata': {'lineage_facts': facts, 'result': {'meta_scan_runs': [{'status': 'submitted', 'target_locator': uri, 'execution_id': 'scan-'+key} for key, uri in outputs.items()]}}}


class Client(base.Client):
    def __init__(self):
        super().__init__()
        self.source.update(full_name='source.geojson', size_bytes=500)
        for identifier, name in enumerate(m.fixture.TARGETS, 20):
            self.targets[identifier] = {'id': identifier, 'fingerprint': name, 'item_type': 'file', 'full_name': 'outputs/'+name,
                'size_bytes': 1000, 'scanned_at': '2026-10-18T00:00:00Z', 'attributes': {'item': {'format': 'geojson'}}}
        self.permissions = sorted(m.PERMISSIONS)
        self.executions['execution'] = execution()
        self.scan_tenant = 2

    def request(self, method, path, expected, body=None):
        if path.endswith('/auth/context'):
            data = {'principal': {'id': 33, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'}, 'token': {'type': 'first_party_access_token'},
                'authorization': {'role_assignments': [{'role_key': 'topology-consumer', 'permissions': self.permissions}]}}
        elif path.endswith('/scan/run/manual'):
            self.calls.append((method,path,body))
            if body['targets'] != ['addp://engine/3/path/source.geojson?type=file']: raise AssertionError(body)
            data = {'execution_id': 'source-scan'}
        elif path == '/api/v1/develop/executions' and method == 'POST':
            self.calls.append((method,path,body)); data = {'execution_id': 'execution'}
        elif '/meta/executions/' in path:
            data = {'status': 'success', 'tenant_id': self.scan_tenant, 'triggered_by': self.actor}
        elif '/meta/lineage/graph?' in path:
            identifier = int(path.split('item_id=')[1].split('&')[0])
            data = {'truncated': False, 'edges': [{'source': {'item_id': 10}, 'target': {'item_id': identifier}, 'status': 'active', 'relation_kind': 'derive', 'evidence': {'execution_id': 'execution'}}]}
        else:
            return super().request(method, path, expected, body)
        return SimpleNamespace(payload=copy.deepcopy(data))


def physical(*args):
    return {'source': {'sha256': 'source', 'size_bytes': 500}, 'exact_inventory_verified': True,
            'targets': {name: {'sha256': name, 'size_bytes': 1000} for name in m.fixture.TARGETS}}


ENV = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_RUN_ID': 'topology-1-1'}


class TopologyOnlineTest(unittest.TestCase):
    def test_public_dag_selects_default_faces_and_named_diagnostics(self):
        tasks = m.definition('source','parent')['tasks']
        self.assertEqual([node['operator'] for node in tasks], ['load','polygonize','save','save'])
        self.assertEqual(tasks[2]['params']['input_df'], {'$ref': 'build'})
        self.assertEqual(tasks[3]['params']['input_df'], {'$ref': 'build', 'port': 'diagnostics'})
        self.assertTrue(all(node['params']['mode'] == 'fail' for node in tasks[2:]))

    def test_exact_two_outputs_and_scans_are_mandatory(self):
        value = execution(); source = value['metadata']['lineage_facts']['inputs'][0]['locator']
        targets = {key: value['outputs']['save_'+key]['resource']['locator'] for key in ('faces','diagnostics')}
        m.validate_success(value, source, targets)
        for mutate in (lambda v: v['outputs'].pop('save_diagnostics'), lambda v: v['metadata']['result']['meta_scan_runs'].pop(),
            lambda v: v['metadata']['result']['meta_scan_runs'][1].update(execution_id='scan-faces'),
            lambda v: v['metadata']['result']['meta_scan_runs'][0].update(target_locator='parent'),
            lambda v: v['metadata']['lineage_facts']['outputs'].pop()):
            changed = copy.deepcopy(value); mutate(changed)
            with self.subTest(mutate=mutate), self.assertRaises(m.SuiteError): m.validate_success(changed, source, targets)

    def test_complete_chain_preserves_same_user_and_does_not_manually_scan_outputs(self):
        client = Client()
        report = m.run_scenario(Path('.'), ENV, client, physical, lambda repo, env, evidence: evidence)
        self.assertEqual(report['result'], 'passed'); self.assertEqual(len(report['outputs']), 2)
        self.assertEqual(len(report['identity']['permissions_verified']), 11)
        self.assertEqual(len([call for call in client.calls if call[0] == 'POST' and '/scan/' in call[1]]), 1)

    def test_extra_permission_or_wrong_scan_actor_blocks_acceptance(self):
        for change in ('permission','actor'):
            client = Client()
            if change == 'permission': client.permissions.append('develop.task.create')
            else: client.actor = 44
            with self.subTest(change=change), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'), ENV, client, lambda *args: self.fail('must stop before physical acceptance'))

    def test_scan_identity_requires_public_integer_user_and_tenant(self):
        for field, values in (('actor', (None, True, 0, -1, '33', 33.)),
                              ('scan_tenant', (None, True, 0, -1, 3, '2', 2.))):
            for value in values:
                client = Client(); setattr(client, field, value)
                with self.subTest(field=field, value=value), self.assertRaisesRegex(m.SuiteError, 'User or Tenant'):
                    m.run_scenario(Path('.'), ENV, client, lambda *args: self.fail('must stop before physical acceptance'))

    def test_private_actor_cannot_replace_public_initiator(self):
        class PrivateOnlyClient(Client):
            def request(self, method, path, expected, body=None):
                if '/meta/executions/' in path:
                    return SimpleNamespace(payload={'status': 'success', 'tenant_id': 2, 'actor_principal_id': 33})
                return super().request(method, path, expected, body)
        with self.assertRaisesRegex(m.SuiteError, 'User or Tenant'):
            m.run_scenario(Path('.'), ENV, PrivateOnlyClient(), lambda *args: self.fail('private actor must not be consumed'))

    def test_invalid_size_or_browser_modification_cannot_pass(self):
        for size in (True, 0, 1000., '1000', 999):
            item = Client().targets[20]; item['size_bytes'] = size
            with self.subTest(size=size), self.assertRaises(m.SuiteError): m.validate_item(item, physical()['targets']['faces.geojson'], 'faces.geojson')
        calls = []
        def changed(*args):
            value = physical(); calls.append(1)
            if len(calls) > 1: value['source']['sha256'] = 'modified'
            return value
        with self.assertRaisesRegex(m.SuiteError, 'browser reads changed'):
            m.run_scenario(Path('.'), ENV, Client(), changed, lambda repo, env, evidence: evidence)

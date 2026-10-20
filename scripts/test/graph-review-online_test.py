import copy
import importlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

REVIEW = importlib.import_module('scripts.test.graph-review-online')
EXTRACTOR = importlib.import_module('scripts.test.graph-review-extractor')
SUITE = importlib.import_module('scripts.test.graph-protection-online')


def reviewed_items():
    return [dict(id=index + 1, source_text=source, status=status,
                 reviewed_by=None if status == 'pending' else 17,
                 reviewed_at=None if status == 'pending' else '2026-10-20T00:00:00Z',
                 element_id='' if status in {'pending', 'rejected'} else 'native-' + source,
                 final_content={'properties': {'name': 'Modified fixture'}} if status == 'modified' else None)
            for index, (source, status) in enumerate(REVIEW.EXPECTED_STATUS.items())]


class GraphReviewOnlineTest(unittest.TestCase):
    def test_controlled_extraction_produces_unique_entities_and_native_failure_after_valid_item(self):
        data = EXTRACTOR.extraction({'text': REVIEW.MATERIAL})
        self.assertEqual([e['properties']['id'] for e in data['entities']], ['p1', 'p2', *EXTRACTOR.REVIEW_IDS])
        self.assertEqual([e['confidence'] for e in data['entities']], [0.99, 0.99, *([0.4] * 6)])
        self.assertIsInstance(data['entities'][-2]['properties']['unsupported'], dict)
        self.assertNotIn('unsupported', data['entities'][-1]['properties'])
        self.assertEqual(data['relations'][0]['source_text'], 'review-link')
        with self.assertRaises(ValueError): EXTRACTOR.extraction({'text': 'business material'})

    def test_native_review_result_cannot_hide_lost_success_wrong_owner_or_unsupported_write(self):
        items = reviewed_items()
        self.assertEqual(len(REVIEW.verify_items(SUITE, items, '17')), 7)
        mutations = [lambda rows: rows.pop(), lambda rows: rows.append(copy.deepcopy(rows[0])),
                     lambda rows: rows[0].update(element_id=''), lambda rows: rows[0].update(reviewed_by=99),
                     lambda rows: rows[0].update(reviewed_at=None), lambda rows: rows[1].update(element_id='unexpected'),
                     lambda rows: rows[2]['final_content']['properties'].update(name='old'),
                     lambda rows: rows[4].update(status='approved'), lambda rows: rows[4].update(reviewed_by=17),
                     lambda rows: rows[5].update(status='pending')]
        for mutate in mutations:
            changed = copy.deepcopy(items)
            mutate(changed)
            with self.subTest(mutation=mutate), self.assertRaises(SUITE.SuiteError): REVIEW.verify_items(SUITE, changed, '17')

    def test_all_review_states_are_explicit_complete_and_not_the_default_pending_queue(self):
        rows = reviewed_items()
        calls = []
        class Client:
            def request(self, method, path, expected):
                calls.append((method, path, expected))
                status = parse_qs(urlsplit(path).query).get('status', ['pending'])[0]
                selected = [item for item in rows if item['status'] == status]
                page = {'data': copy.deepcopy(selected), 'total': len(selected)}
                if mutation == 'missing': page['data'].pop()
                elif mutation == 'wrong-status': page['data'][0]['status'] = 'unknown'
                elif mutation == 'wrong-total': page['total'] += 1
                return SUITE.SUPPORT.Response(200, page)
        for mutation in ('none', 'missing', 'wrong-status', 'wrong-total'):
            calls.clear()
            with self.subTest(mutation=mutation):
                if mutation == 'none':
                    all_items = REVIEW.read_all_review_items(SUITE, Client(), '/review')
                    self.assertEqual(len(REVIEW.verify_items(SUITE, all_items, '17')), 7)
                    self.assertEqual([path for _, path, _ in calls], [f'/review?status={status}&page_size=100'
                                     for status in ('approved', 'rejected', 'modified', 'pending')])
                else:
                    with self.assertRaises(SUITE.SuiteError): REVIEW.read_all_review_items(SUITE, Client(), '/review')

    def test_review_native_target_is_always_separate(self):
        with patch.dict(REVIEW.os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb'}):
            target = REVIEW.target_path(SUITE, 13)
            self.assertEqual(target['engine_id'], 13)
            self.assertEqual(target['segments'][-1]['name'], 'graph_review_fixture')
            self.assertEqual(SUITE.target_paths(13)[0]['names'], ['graph_protection_fixture'])
        for review_id in ('13', '14', '15'):
            with patch.dict(REVIEW.os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'neo4j', 'ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID': '14', 'ADDP_ONLINE_GRAPH_REVIEW_ENGINE_ID': review_id}):
                if review_id != '15':
                    with self.assertRaises(SUITE.SuiteError): REVIEW.target_path(SUITE, 13)
                else: self.assertEqual(REVIEW.target_path(SUITE, 13)['engine_id'], 15)

    def test_missing_or_forged_browser_evidence_never_passes_and_no_api_tokens_enter_browser(self):
        expected = dict(run_id='review-run', tenant_id='2', principal_id='17', engine_type='falkordb', graph_id=19, items={'approve': 23})
        with tempfile.TemporaryDirectory() as directory:
            environment = {'ADDP_ONLINE_ARTIFACT_DIR': directory, 'ADDP_ONLINE_GRAPH_REVIEW_USERNAME': 'reviewer',
                           'ADDP_ONLINE_GRAPH_REVIEW_PASSWORD': 'review-password', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'reader-secret',
                           'ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN': 'preparer-secret', 'ADDP_ONLINE_TEST_USER_PASSWORD': 'reader-password'}
            for mutation, screenshots in (({}, True), ({'principal_id': 'other'}, True), ({'real_model': True}, True), ({'real_model': 0}, True),
                    ({'batch_continued': False}, True), ({'batch_status': 200}, True), ({'refreshed': False}, True),
                    ({'selection_cleared': False}, True), ({'browser_errors': 1}, True), ({}, False)):
                def launch(command, **kwargs):
                    self.assertIn('e2e/online/graph-review.spec.js', command)
                    self.assertFalse(any(k.endswith('_ACCESS_TOKEN') for k in kwargs['env']))
                    self.assertNotIn('ADDP_ONLINE_TEST_USER_PASSWORD', kwargs['env'])
                    self.assertEqual(kwargs['env']['ADDP_ONLINE_GRAPH_REVIEW_PASSWORD'], 'review-password')
                    evidence = dict(expected, schema_version='addp.graph-review-browser/v1', result='passed', permissions=sorted(REVIEW.PERMISSIONS),
                                    single_approve=True, single_reject=True, modify=True, relation_approve=True,
                                    batch_status=500, batch_continued=True, selection_cleared=True, refreshed=True,
                                    unexpected_http_failures=0, browser_errors=0, controlled_extraction=True, real_model=False)
                    evidence.update(mutation)
                    Path(kwargs['env']['ADDP_ONLINE_GRAPH_REVIEW_REPORT']).write_text(json.dumps(evidence))
                    if screenshots:
                        for phase in ('pending', 'modified', 'partial-failure'):
                            (Path(directory) / f'graph-review-{phase}-console.png').write_bytes(b'test screenshot')
                    return type('Process', (), {'returncode': 0})()
                with self.subTest(mutation=mutation, screenshots=screenshots), patch.dict(REVIEW.os.environ, environment), patch.object(REVIEW.subprocess, 'run', side_effect=launch):
                    if mutation or not screenshots:
                        with self.assertRaises(SUITE.SuiteError): REVIEW.browser(SUITE, expected)
                    else: self.assertEqual(REVIEW.browser(SUITE, expected)['result'], 'passed')

    def test_complete_review_flow_checks_native_results_and_cleans_its_own_resources(self):
        calls, finished = [], [False]
        rows = reviewed_items()
        class Client:
            def request(self, method, path, expected, body=None):
                calls.append((method, path, body))
                data = {}
                if path.endswith('/graphs') and method == 'POST': data = {'id': 19, 'is_public': False}
                elif path.endswith('/build/tasks') and method == 'POST': data = {'id': 23}
                elif path.endswith('/build/tasks/23') and method == 'GET' and expected == (200,):
                    data = {'status': 'success', 'execution_id': 'execution', 'stats': {'auto_written': 2, 'pending_review': 7}}
                elif path.endswith('/materials') and method == 'GET':
                    data = [{'id': 29, 'status': 'completed', 'total_chunks': 1, 'processed_chunks': 1}]
                elif '/review?' in path:
                    status = parse_qs(urlsplit(path).query).get('status', ['pending'])[0]
                    queue = rows if finished[0] else [dict(row, status='pending', content={}) for row in rows]
                    selected = [item for item in queue if item['status'] == status]
                    data = {'total': len(selected), 'data': selected}
                elif path.endswith('/review') and method == 'GET': data = {'total': 0}
                elif path.endswith('/entities/Person'):
                    data = {'total': 6, 'data': [{'id': 'native-' + key, 'properties': {'id': key, 'name': 'Modified fixture' if key == 'modify' else key}}
                                                for key in ('p1', 'p2', 'approve', 'modify', 'batch-before', 'batch-after')]}
                elif path.endswith('/stats'): data = {'node_count': 6, 'relationship_count': 1}
                elif path.endswith('/neighbors'): data = {'neighbors': [{'node': {'id': 'native-p2'}}]}
                elif path.endswith('/expand'): data = {'edges': [{'id': 'native-review-link', 'source': 'native-p1', 'target': 'native-p2'}]}
                return SUITE.SUPPORT.Response(expected[0], data)
        def browser(*args):
            finished[0] = True
            return {'result': 'passed'}
        report = {'identities': {'reader': {'principal_id': '4'}}}
        with patch.dict(REVIEW.os.environ, {'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_GRAPH_REVIEW_ACCESS_TOKEN': 'review-secret',
                'ADDP_ONLINE_TEST_RUN_ID': 'review-run', 'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb'}), \
             patch.object(SUITE, 'Client', return_value=Client()), patch.object(SUITE, 'identity', return_value={'principal_id': '17'}), \
             patch.object(REVIEW, 'upload', return_value=29), patch.object(REVIEW, 'browser', side_effect=browser):
            REVIEW.run(SUITE, Client(), Client(), 13, 2, 9, 11, report)
        self.assertTrue(report['review']['passed'])
        self.assertTrue(all('?status=' in path for method, path, _ in calls if method == 'GET' and '/review?' in path))
        self.assertEqual(report['review']['cleanup'], {'errors': [], 'domain_resources_remaining': 0})
        self.assertIn(('PUT', '/api/v1/graph/graphs/11/review/3', {'final_content': {}}), calls)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19/build/tasks/23/materials/29', None), calls)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19/build/tasks/23', None), calls)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19', None), calls)
        self.assertFalse(any(method == 'DELETE' and '/graphs/11' in path for method, path, _ in calls))

    def test_material_failure_still_deletes_task_queue_and_graph(self):
        calls = []
        class Client:
            def request(self, method, path, expected, body=None):
                calls.append((method, path))
                data = {'id': 19, 'is_public': False} if path.endswith('/graphs') else {'id': 23}
                if '/review' in path: data = {'total': 0}
                return SUITE.SUPPORT.Response(expected[0], data)
        report = {'identities': {'reader': {'principal_id': '4'}}}
        with patch.dict(REVIEW.os.environ, {'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_GRAPH_REVIEW_ACCESS_TOKEN': 'review-secret',
                'ADDP_ONLINE_TEST_RUN_ID': 'review-run', 'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb'}), \
             patch.object(SUITE, 'Client', return_value=Client()), patch.object(SUITE, 'identity', return_value={'principal_id': '17'}), \
             patch.object(REVIEW, 'upload', side_effect=SUITE.SuiteError('upload failed')):
            with self.assertRaisesRegex(SUITE.SuiteError, 'upload failed'):
                REVIEW.run(SUITE, Client(), Client(), 13, 2, 9, 11, report)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19/build/tasks/23'), calls)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19'), calls)
        self.assertEqual(report['review']['cleanup']['domain_resources_remaining'], 0)


if __name__ == '__main__': unittest.main()

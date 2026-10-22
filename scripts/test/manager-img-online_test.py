import copy
import hashlib
import importlib
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from urllib.parse import parse_qs, urlsplit

IMG = importlib.import_module('scripts.test.manager-img-online')
OWNER = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')


def detail(spec, index=1):
    attrs = {'item': {'data_type': 'media', 'format': 'img', 'layout': 'single'},
             'type_info': {'media': {'kind': 'image', 'encoding': 'HFA', 'width': 256, 'height': 256}},
             'format_info': {'img': {'scan_complete': True, 'driver': 'HFA', 'band_count': 3}}}
    if spec['source_srid']:
        attrs['capabilities'] = {'spatial': {'srid': spec['source_srid'], 'extent': list(spec['extent'])}}
    return {'id': index, 'item_type': 'object', 'fingerprint': str(index) * 64,
            'full_name': 'bucket/' + spec['object'], 'size_bytes': 4, 'attributes': attrs}


def browser_report(evidence):
    cases = []
    for source in evidence['cases']:
        case = {key: source[key] for key in ('case', 'item_id', 'locator', 'sha256', 'source_srid')}
        case.update(download_sha256=source['sha256'], metadata_verified=True)
        if source['source_srid']:
            case.update(preview_url=source['preview_url'], range_loaded=True, map_loaded=True,
                        reopened=True, reused_result=True, pixels={'cyan': 128, 'magenta': 128, 'yellow': 128})
        else:
            case.update(generation_unavailable=True, map_absent=True)
        cases.append(case)
    return {'schema_version': 'addp.manager-img-browser/v1', 'result': 'passed', 'run_id': evidence['run_id'],
            'identity': evidence['identity'], 'cases': cases, 'generation_requests': 0,
            'failed_business_responses': 0, 'warnings_errors': 0}


class ManagerIMGTests(unittest.TestCase):
    def test_browser_locators_match_actual_chinese_and_english_labels(self):
        import re
        root=Path(__file__).resolve().parents[2]
        script=(root/'console/frontend/e2e/online/manager-img.spec.js').read_text()
        locators=re.findall(r"getByRole\('(tab|button)', \{ name: (.*?), exact: true \}\)",script)
        for locale in ('zh-cn','en'):
            labels=json.loads((root/f'manager/frontend/src/i18n/{locale}.json').read_text())['manager']
            expected={'tab':[labels['explorer']['attributesTab'],labels['explorer']['previewTab']],
                      'button':[labels['spatialPreview']['generateRasterCOG'],labels['vectorTile']['fitExtent'],labels['explorer']['downloadPage']]}
            for role,names in expected.items():
                for name in names:
                    patterns=[value for kind,value in locators if kind==role]
                    matches=[re.fullmatch(value[1:-1],name) if value.startswith('/') else value.strip("'\"")==name for value in patterns]
                    self.assertTrue(any(matches),(locale,role,name,patterns))

    def test_meta_accepts_located_and_unknown_facts_but_rejects_false_claims(self):
        for spec in IMG.SOURCES:
            good = detail(spec)
            IMG.validate_meta(OWNER, good, spec)
            mutations = [lambda d: d['attributes']['format_info']['img'].update(scan_complete=False),
                         lambda d: d['attributes']['type_info']['media'].update(encoding='GTiff'),
                         lambda d: d['attributes']['item'].update(format='tiff')]
            if spec['source_srid']:
                mutations.append(lambda d: d['attributes']['capabilities']['spatial'].update(srid=3857))
            else:
                mutations.append(lambda d: d['attributes'].update(capabilities={'spatial': {'srid': 4326}}))
            for mutate in mutations:
                bad = copy.deepcopy(good); mutate(bad)
                with self.assertRaises(OWNER.SuiteError): IMG.validate_meta(OWNER, bad, spec)

    def test_browser_requires_each_source_real_colors_range_reopen_and_no_generation(self):
        evidence = {'run_id': 'run', 'identity': {'principal_id': '3'},
                    'cases': [{**spec, 'item_id': i, 'locator': f'item:{i}', 'sha256': 'a' * 64,
                               'preview_url': '/api/v1/manager/raster_cog/1/content'} for i, spec in enumerate(IMG.SOURCES, 1)]}
        report = browser_report(evidence)
        IMG.validate_browser(OWNER, report, evidence)
        for mutate in (
            lambda r: r.update(generation_requests=1), lambda r: r.update(schema_version='old'),
            lambda r: r['cases'][0].update(range_loaded=False), lambda r: r['cases'][1].update(reused_result=False),
            lambda r: r['cases'][1]['pixels'].update(cyan=0), lambda r: r['cases'][2].update(map_absent=False),
            lambda r: r['cases'][0].update(download_sha256='wrong'), lambda r: r.update(cases=r['cases'][:2]),
        ):
            bad = copy.deepcopy(report); mutate(bad)
            with self.assertRaises(OWNER.SuiteError): IMG.validate_browser(OWNER, bad, evidence)

    def scenario(self, root, *, fail_browser=False, fail_delete=False, wrong_task_format=False):
        items = [detail(spec, i) for i, spec in enumerate(IMG.SOURCES, 1)]
        seed = {'source_unchanged': True, 'img_sources': [{**{k: s[k] for k in ('case', 'object', 'source_srid')},
                 'size_bytes': 4, 'sha256': hashlib.sha256(b'test').hexdigest()} for s in IMG.SOURCES]}
        generated, cleaned = [], []
        def request(method, path, expected, body=None):
            parsed = urlsplit(path)
            if parsed.path == '/api/v1/meta/engines/2/items': payload = items
            elif parsed.path.startswith('/api/v1/meta/items/'): payload = items[int(parsed.path.split('/')[-1]) - 1]
            elif parsed.path == '/api/v1/manager/downloads/file': return OWNER.Response(200, {}, {}, b'test')
            elif parsed.path == '/api/v1/manager/quick-view/capability': payload = {'available_actions': [], 'can_use_quick_view': False}
            elif parsed.path.startswith('/api/v1/manager/tasks/'):
                payload = {'config': {'raster': {'source_format': 'tiff' if wrong_task_format else 'img'}}}
            elif parsed.path == '/api/v1/manager/raster_cog':
                task = int(parse_qs(parsed.query)['task_id'][0]);payload = {'total': 1, 'data': [{'id': task + 10, 'last_execution_id': f'exec-{task}'}]}
            else: raise AssertionError((method, path))
            return OWNER.Response(200, copy.deepcopy(payload), {})
        client = SimpleNamespace(request=request)
        def generate(_client, fixture, tenant, timeout, physical, source_facts=None):
            generated.append(fixture.format);fixture.task_id = fixture.item['id'];fixture.result_id = fixture.task_id + 10
            fixture.execution_id = f'exec-{fixture.task_id}'
            fixture.artifact = {'preview_url': f'/api/v1/manager/raster_cog/{fixture.result_id}/content',
                                'physical_request': {'source_case': source_facts['case'], 'locator': 'owned'}}
        def clean(_client, fixture, *args):
            cleaned.append(fixture.format)
            if fail_delete: raise OWNER.SuiteError('owned deletion failed')
            fixture.cleanup.update(result_deleted=True, task_deleted=True, content_unavailable=True, preview_mode_restored=True, residual_resources=0)
        def browser(evidence):
            if fail_browser: raise OWNER.SuiteError('original browser assertion')
            return browser_report(evidence)
        def physical(action, request):
            return copy.deepcopy(seed) if action == 'img-source' else {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}
        owner = SimpleNamespace(**{key: getattr(OWNER, key) for key in ('SuiteError', '_object', 'positive_int', 'find_fixture_item', 'ArtifactFixture', 'build_item_locator', 'RASTER_TASK_TYPE')},
            GatewayClient=lambda *args: client, validate_user_identity=lambda *args: {'principal_id': '3'},
            generate_raster_cog=generate, cleanup_managed_artifact=clean)
        env = {'ADDP_ONLINE_TEST_TENANT_ID': '4', 'ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID': '2', 'GATEWAY_URL': 'http://127.0.0.1',
               'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'synthetic-unit-token', 'ADDP_ONLINE_TEST_RUN_ID': 'run',
               'ADDP_ONLINE_MANAGER_MINIO_BUCKET': 'bucket', 'ADDP_ONLINE_ARTIFACT_DIR': str(root)}
        return lambda: IMG.run(Path('/repository'), env, owner, browser_runner=browser, physical_runner=physical), generated, cleaned

    def test_full_orchestration_generates_exactly_two_and_cleans_both(self):
        with tempfile.TemporaryDirectory() as root:
            run, generated, cleaned = self.scenario(root)
            report = run()
            self.assertEqual(report['result'], 'passed')
            self.assertEqual(generated, ['img-wgs84', 'img-utm50n'])
            self.assertEqual(cleaned, list(reversed(generated)))
            self.assertEqual(len(report['cases']), 3)
            self.assertTrue(report['source_unchanged'])

    def test_original_failure_survives_cleanup_failure_and_evidence_is_saved(self):
        with tempfile.TemporaryDirectory() as root:
            run, generated, cleaned = self.scenario(root, fail_browser=True, fail_delete=True)
            with self.assertRaisesRegex(OWNER.SuiteError, 'original browser assertion; IMG cleanup failed:'):
                run()
            self.assertEqual(cleaned, list(reversed(generated)))
            self.assertEqual(json.loads((Path(root) / 'manager-img.json').read_text())['result'], 'failed')

    def test_task_source_format_must_still_be_img_and_failure_cleans_partial_work(self):
        with tempfile.TemporaryDirectory() as root:
            run, generated, cleaned = self.scenario(root, wrong_task_format=True)
            with self.assertRaisesRegex(OWNER.SuiteError, 'canonical source_format'): run()
            self.assertEqual(generated, ['img-wgs84'])
            self.assertEqual(cleaned, generated)


if __name__ == '__main__':
    unittest.main()

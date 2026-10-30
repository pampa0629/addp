import copy
import io
import zipfile
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
    attrs = {'item': {'data_type': 'media', 'format': spec['source_format'], 'layout': 'single'},
             'type_info': {'media': {'kind': 'image', 'encoding': spec['driver'], 'width': spec.get('width',256), 'height': spec.get('height',256)}},
             'format_info': {spec['source_format']: {'scan_complete': True, 'driver': spec['driver'], 'band_count': spec['band_count'], **spec.get('grid_facts',{})}}}
    if spec['source_format']=='tiff':
        attrs['item'].update(layout='multi',file_count=1)
        attrs['type_info']['media']['encoding']='tiff'
        attrs['format_info']['tiff']={'profile':'cog','is_cloud_optimized':True,'cog_check_level':'heuristic',
            'is_tiled':True,'has_overviews':False,'big_tiff':False,'page_count':1,'page_summary_status':'parsed',
            'pages':[{'ifd_index':0,'width':240,'height':120}]}
    if spec['source_srid']:
        attrs['capabilities'] = {'spatial': {'srid': spec['source_srid'], 'extent': list(spec['extent'])}}
    if spec['source_format']=='grd':
        attrs['format_info']['grd']['bands']=[{'dtype':'Float32' if spec['driver']=='GSBG' else 'Float64',
            'nodata':spec['grid_facts']['blank_value'],'nodata_is_nan':False}]
    return {'id': index, 'item_type': 'object', 'fingerprint': str(index) * 64,'scanned_at':'2026-10-30T00:00:00Z',
            'full_name': 'bucket/' + spec['object'], 'size_bytes': 4, 'attributes': attrs}


def browser_report(evidence):
    cases = []
    for source in evidence['cases']:
        case = {key: source[key] for key in ('case', 'item_id', 'locator', 'sha256', 'source_srid')}
        case.update(download_sha256=source['sha256'], metadata_verified=True, source_preview_verified=True)
        if source['source_srid']:
            case.update(preview_url=source['preview_url'], range_loaded=True, map_loaded=True,
                        reopened=True, reused_result=True, pixels={'cyan': 128, 'magenta': 128, 'yellow': 128})
        elif evidence['source_format']=='tiff':
            case.update(map_absent=True,source_image_absent=True,unsupported_message_visible=True,
                tiff_pages_verified=True,cog_metadata_visible=True,zero_overviews_verified=True,
                refreshed_metadata_verified=True,reopened=True,scanned_at=source['refresh']['scanned_at_after'],
                download_bundle={'members':source['download_members'],'size_bytes':110,'sha256':'a'*64})
        else:
            case.update(generation_unavailable=True, map_absent=True, source_image_absent=True, unsupported_message_visible=True)
        cases.append(case)
    return {'schema_version': 'addp.manager-raster-media-browser/v4', 'result': 'passed', 'run_id': evidence['run_id'],
            'source_format':evidence['source_format'], 'identity': evidence['identity'], 'cases': cases, 'generation_requests': 0,
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

    def test_real_preview_response_matcher_accepts_grd_without_capability_request(self):
        import re,subprocess
        root=Path(__file__).resolve().parents[2]
        script=(root/'console/frontend/e2e/online/manager-img.spec.js').read_text()
        function=re.search(r'export function matchesSourcePreviewResponse\(response, locator\) \{.*?\n\}',script,re.S).group(0)
        program="import assert from 'node:assert/strict';\n"+function.replace('export ','',1)+"""
const locator='addp://engine/5/path/bucket/raster/grd-v1.grd?type=object&item_id=71'
const response=(path,source=locator,method='GET')=>({
  url:()=>`http://gateway${path}?page=1&locator=${encodeURIComponent(source)}&page_size=20`,
  request:()=>({method:()=>method})})
// GRD's actual UI emits a preview response, but has no quick-view target.
assert.equal(matchesSourcePreviewResponse(response('/api/v1/manager/preview'),locator),true)
for(const candidate of [response('/api/v1/manager/quick-view/capability'),
 response('/api/v1/meta/items/9'),response('/api/v1/manager/preview','other'),
 response('/api/v1/manager/preview',locator.replace('item_id=71','item_id=72')),
 response('/api/v1/manager/preview',locator,'POST')])
 assert.equal(matchesSourcePreviewResponse(candidate,locator),false)
for(const extension of ['img','jp2','j2k']) {
 const source=locator.replace('grd-v1.grd','source.'+extension)
 assert.equal(matchesSourcePreviewResponse(response('/api/v1/manager/preview',source),source),true)
}
"""
        result=subprocess.run(['node','--input-type=module','-e',program],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn('page.waitForResponse(response => matchesSourcePreviewResponse(response, source.locator))',script)

    def test_meta_accepts_located_and_unknown_facts_but_rejects_false_claims(self):
        for spec in (*IMG.SOURCES, *IMG.JPEG2000_SOURCES, *IMG.GRD_SOURCES):
            good = detail(spec)
            IMG.validate_meta(OWNER, good, spec)
            mutations = [lambda d: d['attributes']['format_info'][spec['source_format']].update(scan_complete=False),
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
        evidence = {'source_format':'img', 'run_id': 'run', 'identity': {'principal_id': '3'},
                    'cases': [{**spec, 'item_id': i, 'locator': f'item:{i}', 'sha256': 'a' * 64,
                               'preview_url': '/api/v1/manager/raster_cog/1/content'} for i, spec in enumerate(IMG.SOURCES, 1)]}
        report = browser_report(evidence)
        IMG.validate_browser(OWNER, report, evidence)
        for mutate in (
            lambda r: r.update(generation_requests=1), lambda r: r['cases'][0].pop('source_preview_verified'),
            lambda r: r['cases'][0].update(source_preview_verified=False),
            lambda r: r.update(schema_version='addp.manager-raster-media-browser/v2'), lambda r: r.update(schema_version='old'),
            lambda r: r['cases'][0].update(range_loaded=False), lambda r: r['cases'][1].update(reused_result=False),
            lambda r: r['cases'][1]['pixels'].update(cyan=0), lambda r: r['cases'][2].update(map_absent=False),
            lambda r: r['cases'][2].update(source_image_absent=False), lambda r: r['cases'][2].pop('unsupported_message_visible'),
            lambda r: r['cases'][0].update(download_sha256='wrong'), lambda r: r.update(cases=r['cases'][:2]),
        ):
            bad = copy.deepcopy(report); mutate(bad)
            with self.assertRaises(OWNER.SuiteError): IMG.validate_browser(OWNER, bad, evidence)

    def test_unlocated_jp2_and_j2k_require_unsupported_renderer_evidence(self):
        evidence = {'source_format': 'jpeg2000', 'run_id': 'run', 'identity': {},
                    'cases': [{**spec, 'item_id': i, 'locator': f'item:{i}', 'sha256': 'a' * 64,
                               'preview_url': '/api/v1/manager/raster_cog/1/content'} for i, spec in enumerate(IMG.JPEG2000_SOURCES)]}
        good = browser_report(evidence)
        IMG.validate_browser(OWNER, good, evidence)
        for index in (2, 3):
            for field in ('source_image_absent', 'unsupported_message_visible'):
                for missing in (False, True):
                    bad = copy.deepcopy(good)
                    if missing: bad['cases'][index].pop(field)
                    else: bad['cases'][index][field] = False
                    with self.assertRaises(OWNER.SuiteError): IMG.validate_browser(OWNER, bad, evidence)
        bad = copy.deepcopy(good); bad['schema_version'] = 'addp.manager-raster-media-browser/v1'
        with self.assertRaises(OWNER.SuiteError): IMG.validate_browser(OWNER, bad, evidence)

    def scenario(self, root, *, fail_browser=False, fail_delete=False, wrong_task_format=False, source_format="img"):
        sources = {"img": IMG.SOURCES, "jpeg2000": IMG.JPEG2000_SOURCES, "grd":IMG.GRD_SOURCES,'tiff':IMG.TIFF_SOURCES}[source_format]
        items = [detail(spec, i) for i, spec in enumerate(sources, 1)]
        seed = {'source_unchanged': True, source_format+'_sources': [{**{k: s[k] for k in ('case', 'object', 'source_srid')},
                 'size_bytes': 4, 'sha256': hashlib.sha256(b'test').hexdigest()} for s in sources]}
        if source_format=='tiff':seed['tiff_sources'][0].update(driver='GTiff',width=240,height=120,band_count=1,
            dtype='Float64',nodata_is_nan=True,overview_count=0,block_size=[512,512],cog_valid=True,
            cog_errors=0,cog_warnings=0,pixels_verified=28800,valid_pixels=28765,complete_mask_verified=True)
        generated, cleaned = [], []
        def request(method, path, expected, body=None):
            parsed = urlsplit(path)
            if parsed.path == '/api/v1/meta/engines/2/items': payload = items
            elif parsed.path=='/api/v1/manager/engines/2/items/refresh':
                self.assertEqual(method,'POST');self.assertIn('item_id=1',parse_qs(parsed.query)['locator'][0])
                items[0]['scanned_at']='2026-10-30T00:00:01Z'
                items[0]['attributes']['item']['full_name']=items[0]['full_name']
                payload={'status':'success','items_scanned':1,'started_at':'2026-10-30T00:00:00.5Z'}
            elif parsed.path.startswith('/api/v1/meta/items/'): payload = items[int(parsed.path.split('/')[-1]) - 1]
            elif parsed.path == '/api/v1/manager/downloads/file':
                if source_format=='tiff':
                    buf=io.BytesIO()
                    with zipfile.ZipFile(buf,'w')as z:z.writestr('tiff-unlocated-small-cog.tif',b'test')
                    return OWNER.Response(200,{}, {'Content-Type':'application/zip'},buf.getvalue())
                return OWNER.Response(200, {}, {}, b'test')
            elif parsed.path == '/api/v1/manager/quick-view/capability': payload = {'available_actions': [], 'can_use_quick_view': False}
            elif parsed.path.startswith('/api/v1/manager/tasks/'):
                payload = {'config': {'raster': {'source_format': 'tiff' if wrong_task_format else source_format}}}
            elif parsed.path.endswith('/content'): return OWNER.Response(200, {}, {}, b'test')
            elif parsed.path == '/api/v1/manager/raster_cog':
                task = int(parse_qs(parsed.query)['task_id'][0]);payload = {'total': 1, 'data': [{'id': task + 10, 'last_execution_id': f'exec-{task}'}]}
            else: raise AssertionError((method, path))
            return OWNER.Response(200, copy.deepcopy(payload), {})
        client = SimpleNamespace(request=request)
        def generate(_client, fixture, tenant, timeout, physical, source_facts=None):
            generated.append(fixture.format);fixture.task_id = fixture.item['id'];fixture.result_id = fixture.task_id + 10
            fixture.execution_id = f'exec-{fixture.task_id}'
            fixture.artifact = {'size_bytes':4, 'sha256':hashlib.sha256(b'test').hexdigest(), 'preview_url': f'/api/v1/manager/raster_cog/{fixture.result_id}/content',
                                'physical_request': {'source_case': source_facts['case'], 'locator': 'owned'}}
        def clean(_client, fixture, *args):
            cleaned.append(fixture.format)
            if fail_delete: raise OWNER.SuiteError('owned deletion failed')
            fixture.cleanup.update(result_deleted=True, task_deleted=True, content_unavailable=True, preview_mode_restored=True, residual_resources=0)
        def browser(evidence):
            if fail_browser: raise OWNER.SuiteError('original browser assertion')
            return browser_report(evidence)
        def physical(action, request):
            return copy.deepcopy(seed) if action == source_format+'-source' else {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}
        owner = SimpleNamespace(**{key: getattr(OWNER, key) for key in ('SuiteError', '_object', 'positive_int', 'find_fixture_item', 'ArtifactFixture', 'build_item_locator', 'RASTER_TASK_TYPE','validate_model_bundle')},
            GatewayClient=lambda *args: client, validate_user_identity=lambda *args: {'principal_id': '3'},
            generate_raster_cog=generate, cleanup_managed_artifact=clean)
        env = {'ADDP_ONLINE_TEST_TENANT_ID': '4', 'ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID': '2', 'GATEWAY_URL': 'http://127.0.0.1',
               'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'synthetic-unit-token', 'ADDP_ONLINE_TEST_RUN_ID': 'run',
               'ADDP_ONLINE_MANAGER_MINIO_BUCKET': 'bucket', 'ADDP_ONLINE_ARTIFACT_DIR': str(root)}
        return lambda: IMG.run(Path('/repository'), env, owner, browser_runner=browser, physical_runner=physical, source_format=source_format), generated, cleaned

    def test_grd_reads_all_profiles_without_generation_and_preserves_sources(self):
        with tempfile.TemporaryDirectory() as root:
            run, generated, cleaned = self.scenario(root, source_format='grd')
            report=run()
            self.assertEqual(report['result'],'passed')
            self.assertEqual([c['case'] for c in report['cases']],['v1','v2','surfer6','surfer6-ascii'])
            self.assertEqual(generated,[])
            self.assertEqual(cleaned,[])
            self.assertTrue(report['source_unchanged'])
        for spec in IMG.GRD_SOURCES:
            good=detail(spec)
            for key in spec['grid_facts']:
                bad=copy.deepcopy(good)
                bad['attributes']['format_info']['grd'].pop(key)
                with self.assertRaises(OWNER.SuiteError):IMG.validate_meta(OWNER,bad,spec)
            for key,value in (('dtype','Float64' if spec['driver']=='GSBG' else 'Float32'),('nodata',None),('nodata_is_nan',True)):
                bad=copy.deepcopy(good);bad['attributes']['format_info']['grd']['bands'][0][key]=value
                with self.assertRaises(OWNER.SuiteError):IMG.validate_meta(OWNER,bad,spec)
        bad=detail(IMG.GRD_SOURCES[-1]);bad['attributes']['format_info']['grd']['version']=6
        with self.assertRaises(OWNER.SuiteError):IMG.validate_meta(OWNER,bad,IMG.GRD_SOURCES[-1])

    def test_full_orchestration_generates_exactly_two_and_cleans_both(self):
        with tempfile.TemporaryDirectory() as root:
            run, generated, cleaned = self.scenario(root, source_format='jpeg2000')
            report = run()
            self.assertEqual(report['result'], 'passed')
            self.assertEqual(generated, ['jpeg2000-wgs84', 'jpeg2000-utm50n'])
            self.assertEqual(cleaned, list(reversed(generated)))
            self.assertEqual(len(report['cases']), 4)
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

    def test_tiff_exact_refresh_bundle_download_and_browser_do_not_generate(self):
        with tempfile.TemporaryDirectory()as root:
            run,generated,cleaned=self.scenario(root,source_format='tiff');report=run()
            self.assertEqual(report['result'],'passed');self.assertEqual(generated,[]);self.assertEqual(cleaned,[])
            self.assertTrue(report['source_unchanged']);case=report['cases'][0]
            self.assertEqual(case['refresh']['response']['items_scanned'],1)
            self.assertEqual(case['refresh']['scanned_at_after'],'2026-10-30T00:00:01Z')
            self.assertEqual(case['download_members']['tiff-unlocated-small-cog.tif']['sha256'],hashlib.sha256(b'test').hexdigest())
            self.assertEqual(Path(root,'raster-source-tiff-unlocated-small-cog.tif').read_bytes(),b'test')
            evidence={'source_format':'tiff','run_id':'run','identity':report['identity'],'cases':report['cases']}
            for fault in ['map_absent','source_image_absent','unsupported_message_visible','tiff_pages_verified',
                          'cog_metadata_visible','zero_overviews_verified','refreshed_metadata_verified','reopened']:
                bad=copy.deepcopy(report['browser']);bad['cases'][0][fault]=False
                with self.subTest(fault=fault),self.assertRaises(OWNER.SuiteError):IMG.validate_browser(OWNER,bad,evidence)
            for mutate in (lambda d:d.update(schema_version='addp.manager-raster-media-browser/v3'),
                           lambda d:d['cases'][0].pop('download_bundle'),
                           lambda d:d['cases'][0].update(scanned_at='old'),lambda d:d.update(generation_requests=1)):
                bad=copy.deepcopy(report['browser']);mutate(bad)
                with self.assertRaises(OWNER.SuiteError):IMG.validate_browser(OWNER,bad,evidence)

    def test_tiff_rejects_old_hint_unknown_level_overviews_and_fabricated_location(self):
        good=detail(IMG.TIFF_SOURCES[0]);IMG.validate_tiff_meta(OWNER,good)
        for mutate in (lambda a:a['format_info']['tiff'].update(profile='plain_tiff'),
                       lambda a:a['format_info']['tiff'].update(is_cloud_optimized=False),
                       lambda a:a['format_info']['tiff'].pop('is_cloud_optimized'),
                       lambda a:a['format_info']['tiff'].pop('cog_check_level'),
                       lambda a:a['format_info']['tiff'].update(has_overviews=True),
                       lambda a:a['type_info']['media'].update(width=241),
                       lambda a:a.update(capabilities={'spatial':{'srid':4326}}),
                       lambda a:a['type_info'].update(spatial={'srid':4326})):
            bad=copy.deepcopy(good);mutate(bad['attributes'])
            with self.assertRaises(OWNER.SuiteError):IMG.validate_tiff_meta(OWNER,bad)

    def test_tiff_physical_and_synchronous_refresh_proof_cannot_be_partial(self):
        spec=IMG.TIFF_SOURCES[0];good=detail(spec)
        response={'status':'success','items_scanned':1,'started_at':'2026-10-30T00:00:00.5Z'}
        after=copy.deepcopy(good);after['scanned_at']='2026-10-30T00:00:01Z'
        after['attributes']['item']['full_name']=after['full_name']
        model=OWNER.ArtifactFixture('tiff',good,'item:1')
        for mutate in (lambda r,a:r.update(status='failed'),lambda r,a:r.update(items_scanned=2),
                       lambda r,a:r.update(items_scanned=True),lambda r,a:r.pop('started_at'),
                       lambda r,a:a.update(scanned_at=good['scanned_at']),lambda r,a:a.update(id=2),
                       lambda r,a:a.update(full_name='other.tif'),
                       lambda r,a:a['attributes']['item'].update(full_name='other.tif'),
                       lambda r,a:a['attributes']['item'].pop('full_name'),
                       lambda r,a:a['attributes']['format_info']['tiff'].update(profile='plain_tiff')):
            r,a=copy.deepcopy(response),copy.deepcopy(after);mutate(r,a)
            client=SimpleNamespace(request=lambda method,*args:SimpleNamespace(payload=r if method=='POST'else a))
            with self.assertRaises(OWNER.SuiteError):IMG.refresh_tiff(OWNER,client,2,model,good)
        with tempfile.TemporaryDirectory()as root:
            run,_,_=self.scenario(root,source_format='tiff');facts=run()['physical_sources'][0]
            for key in ('cog_valid','cog_errors','cog_warnings','pixels_verified','complete_mask_verified','overview_count','source_srid'):
                bad=copy.deepcopy(facts);bad.pop(key)
                with self.assertRaises(OWNER.SuiteError):IMG.validate_tiff_physical(OWNER,bad)


if __name__ == '__main__':
    unittest.main()

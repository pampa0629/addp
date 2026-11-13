import copy
import importlib
import json
import tempfile
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

m = importlib.import_module('scripts.test.vector-topology-persistence-online')
base = importlib.import_module('scripts.test.raster-contour-persistence-online_test')


def execution():
    outputs = {key: f'addp://engine/3/path/outputs/{name}?type=file' for key, name in zip(m.fixture.PORTS, m.fixture.TARGETS)}
    inputs = [{'port': 'input.'+node, 'locator': f'addp://engine/3/path/{name}?type=file&item_id={identifier}'}
              for identifier, (node, name) in enumerate(zip(('load', 'load_invalid', 'snap_input', 'snap_reference', 'overlap_input', 'gap_input', 'gap_coverage', 'dangling_input', 'line_overlap_input', 'line_self_overlap_input', 'line_intersection_input', 'line_self_intersection_input', 'split_line_input','line_component_input','line_chain_input'), m.fixture.SOURCES), 10)]
    facts = {'schema_version': 'addp.lineage-facts/v1', 'inputs': sorted(inputs, key=lambda entry: entry['port']), 'outputs': [{'locator': uri, 'write_mode': 'create'} for uri in outputs.values()]}
    return {'status': 'success', 'outputs': {'save_'+key: {'resource': {'locator': uri, 'type': 'file', 'write_mode': 'create'}} for key, uri in outputs.items()},
        'metadata': {'lineage_facts': facts, 'result': {'meta_scan_runs': [{'status': 'submitted', 'target_locator': uri, 'execution_id': 'scan-'+key} for key, uri in outputs.items()]}}}


class Client(base.Client):
    def __init__(self):
        super().__init__()
        self.source.update(full_name='source.geojson', size_bytes=500)
        for identifier, name in enumerate(m.fixture.TARGETS, 30):
            self.targets[identifier] = {'id': identifier, 'fingerprint': name, 'item_type': 'file', 'full_name': 'outputs/'+name,
                'size_bytes': 1000, 'scanned_at': '2026-10-18T00:00:00Z', 'attributes': {'item': {'format': 'geojson'}}}
        self.invalid_source = {**self.source, 'id': 11, 'full_name': 'invalid.geojson', 'fingerprint': 'invalid'}
        self.snap_sources = {identifier: {**self.source, 'id': identifier, 'full_name': name, 'fingerprint': name} for identifier, name in enumerate(m.fixture.SOURCES[2:],12)}
        for identifier,name in ((33,'snapped.gpkg'),(34,'overlaps.gpkg'),(35,'gaps.gpkg'),(36,'dangling.gpkg'),(37,'line-overlaps.gpkg'),(38,'line-self-overlaps.gpkg'),(39,'line-intersections.gpkg'),(40,'line-self-intersections.gpkg'),(41,'split-lines.gpkg'),(42,'line-components.gpkg'),(43,'line-chains.gpkg'),(44,'line-chain-members.gpkg')):
            self.targets[identifier] = base.item(name,identifier); self.targets[identifier]['size_bytes'] = 1000
        self.permissions = sorted(m.PERMISSIONS)
        self.executions['execution'] = execution()
        self.scan_tenant = 2

    def request(self, method, path, expected, body=None):
        if path.endswith('/auth/context'):
            data = {'principal': {'id': 33, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'}, 'token': {'type': 'first_party_access_token'},
                'authorization': {'role_assignments': [{'role_key': 'topology-consumer', 'permissions': self.permissions}]}}
        elif path.endswith('/scan/run/manual'):
            self.calls.append((method,path,body))
            if body['targets'] not in [[f'addp://engine/3/path/{name}?type=file'] for name in m.fixture.SOURCES]: raise AssertionError(body)
            data = {'execution_id': 'source-scan-'+body['targets'][0].split('/path/')[1].split('?')[0]}
        elif path == '/api/v1/develop/executions' and method == 'POST':
            self.calls.append((method,path,body))
            identifier = 'failed-snap' if body['content']['workflow_definition']['tasks'][-1]['params']['target_name']=='failed-snap.gpkg' else 'execution'
            self.executions['failed-snap'] = {'status': 'failed', 'metadata': {}}
            data = {'execution_id': identifier}
        elif '/meta/executions/' in path:
            data = {'status': 'success', 'tenant_id': self.scan_tenant, 'triggered_by': self.actor}
        elif path.endswith('/engines/3/items'):
            data = [self.source, self.invalid_source, *self.snap_sources.values(), *self.targets.values()]
        elif path.startswith('/api/v1/meta/scan/runs?'):
            identifiers = ['source-scan-'+name for name in m.fixture.SOURCES]+['scan-'+port for port in m.fixture.PORTS]
            data = {'total': len(identifiers), 'items': [{'execution_id': value} for value in identifiers]}
        elif path.startswith('/api/v1/meta/items/') and int(path.rsplit('/',1)[1]) in self.snap_sources:
            data = self.snap_sources[int(path.rsplit('/',1)[1])]
        elif path == '/api/v1/meta/items/11':
            data = self.invalid_source
        elif '/meta/lineage/graph?' in path:
            identifier = int(path.split('item_id=')[1].split('&')[0])
            data = {'truncated': False, 'edges': [{'source': {'item_id': source}, 'target': {'item_id': identifier}, 'status': 'active', 'relation_kind': 'derive', 'evidence': {'execution_id': 'execution'}} for source in ([24] if identifier in (43,44) else [23] if identifier == 42 else [22] if identifier == 41 else [21] if identifier == 40 else [20] if identifier == 39 else [19] if identifier == 38 else [18] if identifier == 37 else [17] if identifier == 36 else [15,16] if identifier == 35 else [14] if identifier == 34 else [12,13] if identifier == 33 else [11] if identifier == 32 else [10])]}
        else:
            return super().request(method, path, expected, body)
        return SimpleNamespace(payload=copy.deepcopy(data))


def physical(*args):
    return {'sources': {name: {'sha256': name, 'size_bytes': 500} for name in m.fixture.SOURCES}, 'exact_inventory_verified': True,
            'targets': {name: {'sha256': name, 'size_bytes': 1000, **({'layer_name': name.removesuffix('.gpkg'), 'geometry_column': 'geom', 'fid_column': 'fid'} if name.endswith('.gpkg') else {})} for name in m.fixture.TARGETS}}


ENV = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_RUN_ID': 'topology-1-1'}


class TopologyOnlineTest(unittest.TestCase):
    def test_chain_double_ports_bind_exact_source_identity_scans_and_complete_members(self):
        tasks={t['id']:t for t in m.definition('s0','parent',*[f's{i}' for i in range(1,len(m.fixture.SOURCES))])['tasks']}
        self.assertEqual(tasks['line_chain_input']['params'],{'locator':'s14'})
        self.assertEqual(tasks['line_chain']['params'],{'input_gdf':{'$ref':'line_chain_input'},'id_field':'route_key','match_fields':['road','grade']})
        self.assertEqual(tasks['save_line_chains']['params']['input_df'],{'$ref':'line_chain'})
        self.assertEqual(tasks['save_line_chain_members']['params']['input_df'],{'$ref':'line_chain','port':'members'})
        report=m.run_scenario(Path('.'),self.env,Client(),physical,lambda repo,env,evidence:evidence,diagnostic_runner=lambda *args:{})
        self.assertEqual(report['browser']['line_chain_expected'],m.fixture.line_chain_expected_features())
        self.assertEqual(report['browser']['line_chain_members_expected'],m.fixture.line_chain_expected_features(members=True))
        for key,target in [('line_chains',43),('line_chain_members',44)]:
            self.assertEqual(m.output_sources(key),(14,))
            output=next(o for o in report['outputs'] if o['port']==key)
            self.assertEqual(output['lineage'],[{'source_item_id':24,'target_item_id':target,'execution_id':'execution'}])
            bad=Client();bad.targets[target]['size_bytes']=True
            with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),self.env,bad,physical)
        class WrongScan(Client):
            def request(self,method,path,expected,body=None):
                response=super().request(method,path,expected,body)
                if '/meta/executions/scan-line_chain_members' in path:response.payload['triggered_by']=44
                return response
        with self.assertRaisesRegex(m.SuiteError,'User or Tenant'):
            m.run_scenario(Path('.'),self.env,WrongScan(),lambda *args:self.fail('must reject wrong direction scan actor'))

    def test_chain_report_must_include_both_complete_independent_oracles(self):
        evidence={'execution_id':'current','line_chain_expected':m.fixture.line_chain_expected_features(),
                  'line_chain_members_expected':m.fixture.line_chain_expected_features(members=True)}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        for key in ['line_chain_expected','line_chain_members_expected']:
            def run(*args,**kwargs):
                report={**evidence,'schema_version':'addp.topology-browser/v14','result':'passed'};report.pop(key)
                path.write_text(json.dumps(report));return SimpleNamespace(returncode=0)
            with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_line_component_branch_binds_source_membership_attributes_and_automatic_scan(self):
        tasks=m.definition('s0','parent',*[f's{i}' for i in range(1,len(m.fixture.SOURCES))])['tasks'][:-4]
        self.assertEqual(tasks[-3]['params'],{'locator':'s13'})
        self.assertEqual(tasks[-2]['operator'],'group_line_components')
        self.assertEqual(tasks[-2]['params'],{'input_gdf':{'$ref':'line_component_input'},'id_field':'route_key'})
        self.assertEqual(tasks[-1]['params']['target_name'],'line-components.gpkg')
        self.assertEqual(m.output_sources('line_components'),(13,))
        report=m.run_scenario(Path('.'),self.env,Client(),physical,lambda repo,env,evidence:evidence,diagnostic_runner=lambda *args:{})
        self.assertEqual(report['browser']['line_component_expected'],m.fixture.line_component_expected_features())
        self.assertEqual(report['outputs'][-3]['port'],'line_components')
        self.assertEqual(report['outputs'][-3]['lineage'],[{'source_item_id':23,'target_item_id':42,'execution_id':'execution'}])
        class WrongScan(Client):
            def request(self,method,path,expected,body=None):
                response=super().request(method,path,expected,body)
                if '/meta/executions/scan-line_components' in path:response.payload['triggered_by']=44
                return response
        with self.assertRaisesRegex(m.SuiteError,'User or Tenant'):
            m.run_scenario(Path('.'),self.env,WrongScan(),lambda *args:self.fail('must reject wrong scan actor'))
        wrong=Client();wrong.targets[42]['size_bytes']=True
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),self.env,wrong,physical)

    def test_line_component_report_cannot_omit_expected_groups_or_attributes(self):
        evidence={'execution_id':'current','outputs':[{'port':'line_components'}],'line_component_expected':m.fixture.line_component_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],'schema_version':'addp.topology-browser/v14','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_split_line_branch_binds_exact_source_scan_attributes_and_lineage(self):
        tasks=m.definition('s0','parent',*[f's{i}' for i in range(1,len(m.fixture.SOURCES))])['tasks'][:-7]
        self.assertEqual(tasks[-3]['params'],{'locator':'s12'})
        self.assertEqual(tasks[-2]['operator'],'split_line_intersections')
        self.assertEqual(tasks[-2]['params'],{'input_gdf':{'$ref':'split_line_input'},'id_field':'route_key','scope':'all'})
        self.assertEqual(tasks[-1]['params']['target_name'],'split-lines.gpkg')
        self.assertEqual(m.output_sources('split_lines'),(12,))
        report=m.run_scenario(Path('.'),self.env,Client(),physical,lambda repo,env,evidence:evidence,diagnostic_runner=lambda *args:{})
        self.assertEqual(report['browser']['split_line_expected'],m.fixture.split_line_expected_features())
        self.assertEqual(report['browser']['split_line_scope'],'all')
        self.assertEqual(report['split_line_scope'],'all')
        self.assertEqual(len(report['browser']['split_line_expected']),43)
        self.assertEqual(report['outputs'][-4]['port'],'split_lines')
        self.assertEqual(report['outputs'][-4]['lineage'],[{'source_item_id':22,'target_item_id':41,'execution_id':'execution'}])
        checkpoint=json.loads((Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-before-lineage.json').read_text())
        self.assertEqual(checkpoint['split_line_scope'],'all')
        self.assertEqual(checkpoint['schema_version'],'addp.topology-checkpoint/v14')
        self.assertEqual(checkpoint['result'],'incomplete')
        class WrongScan(Client):
            def request(self,method,path,expected,body=None):
                response=super().request(method,path,expected,body)
                if '/meta/executions/scan-split_lines' in path:response.payload['triggered_by']=44
                return response
        with self.assertRaisesRegex(m.SuiteError,'User or Tenant'):
            m.run_scenario(Path('.'),self.env,WrongScan(),lambda *args:self.fail('must reject wrong scan actor'))
        wrong=Client();wrong.targets[41]['size_bytes']=True
        with self.assertRaises(m.SuiteError):
            m.run_scenario(Path('.'),self.env,wrong,physical,lambda *args:self.fail('must reject noninteger physical size'))

    def test_split_line_browser_report_cannot_omit_attributes_or_expected_fragments(self):
        evidence={'execution_id':'current','outputs':[{'port':'split_lines'}],'split_line_expected':m.fixture.split_line_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],'schema_version':'addp.topology-browser/v14','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_split_all_browser_binding_rejects_missing_or_wrong_scope(self):
        evidence={'execution_id':'current','split_line_scope':'all','split_line_expected':m.fixture.split_line_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        for scope in (None,'between'):
            def run(*args,**kwargs):
                report={**evidence,'schema_version':'addp.topology-browser/v14','result':'passed'}
                if scope is None:report.pop('split_line_scope')
                else:report['split_line_scope']=scope
                path.write_text(json.dumps(report));return SimpleNamespace(returncode=0)
            with self.subTest(scope=scope),patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):
                m.browser(Path('.'),self.env,evidence)

    def test_split_all_map_reads_full_real_pagination_response(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r"""
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
const source=readFileSync(process.argv[1],'utf8')
const start=source.indexOf("    if (output.port === 'split_lines') {",source.indexOf('let previewResponse = await previewRead'))
const end=source.indexOf('    const preview = await json(previewResponse',start)
assert.ok(start>0 && end>start)
const check=new (Object.getPrototypeOf(async function(){}).constructor)('page','frame','output','locator','previewResponse',source.slice(start,end)+';return previewResponse')
const locator='addp://engine/7/path/outputs/split-lines.gpkg?type=file&item_id=8'
const url=new URL('http://localhost/api/v1/manager/preview');url.search=new URLSearchParams({locator,child_name:'pieces',page:'1',page_size:'50'})
let clicks=[]
const response=(url,method='GET')=>({url:()=>url,request:()=>({method:()=>method})})
const full=response(url.href)
const page={waitForResponse(predicate){
 assert.ok(predicate(full))
 assert.ok(!predicate(response(url.href,'POST')))
 for(const [key,value] of [['locator','foreign'],['child_name','other'],['page','2'],['page_size','20']]){
  const wrong=new URL(url);wrong.searchParams.set(key,value);assert.ok(!predicate(response(wrong.href)))
 }
 return Promise.resolve(full)
}}
const frame={locator(selector){assert.equal(selector,'.table-preview .el-pagination__sizes .el-select');return {click:async()=>{clicks.push('sizes')}}},
 getByRole(role,{name}){assert.equal(role,'option');assert.ok(name.test('50条/页'));assert.ok(!name.test('500条/页'));return {click:async()=>{clicks.push('50')}}}}
assert.equal(await check(page,frame,{port:'split_lines',layer_name:'pieces'},locator,'first'),full)
assert.deepEqual(clicks,['sizes','50'])
clicks=[];assert.equal(await check(page,frame,{port:'line_components'},locator,'first'),'first');assert.deepEqual(clicks,[])
assert.ok(source.includes("if (output.port === 'split_lines') expect([data.page, data.page_size, data.rows.length]).toEqual([1,50,43])"))
"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js')],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)

    def test_all_output_map_type_counts_require_exact_port_registration(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r"""
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const source=readFileSync(process.argv[1],'utf8')
const golden={faces:['Polygon',4],diagnostics:['LineString',4],repaired:['MultiPolygon',2],snapped:['LineString',5],
 overlaps:['MultiPolygon',7],gaps:['Polygon',8],dangling:['Point',17],line_overlaps:['MultiLineString',9],
 line_self_overlaps:['MultiLineString',8],line_intersections:['Point',10],line_self_intersections:['Point',15],
 split_lines:['LineString',43],line_components:['LineString',15],line_chains:['LineString',12],line_chain_members:['LineString',18]}
const helperStart=source.indexOf('const expectedOutputGeometry =')
const helperEnd=source.indexOf("test('同一普通User核对",helperStart)
assert.ok(helperStart>0 && helperEnd>helperStart)
const expectedFor=new Function('port',source.slice(helperStart,helperEnd)+'\nreturn expectedGeometryTypes(port)')
for(const [port,[type,count]] of Object.entries(golden))assert.deepEqual(expectedFor(port),Array(count).fill(type))
assert.throws(()=>expectedFor('unknown'),/Unregistered topology output port/)
assert.throws(()=>expectedFor('constructor'),/Unregistered topology output port/)
assert.ok(source.includes('const expectedTypes = expectedGeometryTypes(output.port)'))
assert.ok(source.includes(".toEqual([expectedTypes.length, 3857, 'EPSG:3857'"))
const start=source.indexOf('    await expect.poll(async () => renderer.evaluate(element => {',source.indexOf('const renderer = table.locator'))
const end=source.indexOf("    if (['dangling','line_intersections','line_self_intersections'].includes(output.port))",start)
assert.ok(start>0 && end>start)
const check=new (Object.getPrototypeOf(async function(){}).constructor)('renderer','expect','output','expectedTypes',source.slice(start,end))
const expect={poll:fn=>({async toEqual(expected){assert.deepEqual(await fn(),expected)}})}
async function run(port,types){
 const features=types.map(type=>({getGeometry:()=>({getType:()=>type})}))
 const map={getView:()=>({getAnimating:()=>false}),getLayers:()=>({getArray:()=>[{getSource:()=>({getFeatures:()=>features})}]})}
 const renderer={evaluate:async fn=>fn({__vueParentComponent:{setupState:{mapInstance:map}}})}
 const [type,count]=golden[port]
 await check(renderer,expect,{port},expectedFor(port))
}
for(const [port,[type,count]] of Object.entries(golden)){
 const correct=Array(count).fill(type)
 await run(port,correct)
 await assert.rejects(run(port,correct.slice(1)),{code:'ERR_ASSERTION'})
 await assert.rejects(run(port,[...correct,type]),{code:'ERR_ASSERTION'})
 await assert.rejects(run(port,['GeometryCollection',...correct.slice(1)]),{code:'ERR_ASSERTION'})
}
"""
        result=subprocess.run(['node','--input-type=module','-e',script,
            str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js')],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)

    def test_original_parts_rendered_oracle_rejects_wrong_attributes_order_direction_and_coordinates(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r'''
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const expected=JSON.parse(process.argv[1]); const port=process.argv[4]
const source=readFileSync(process.argv[2],'utf8')
const { webMercatorPreviewCoordinate }=await import(process.argv[3])
const start=source.indexOf("    if (['split_lines','line_components','line_chains','line_chain_members'].includes(output.port)) {",source.indexOf('const table = frame.locator'))
const end=source.indexOf("    if (output.port === 'snapped') {",start)
assert.ok(start>0 && end>start)
const check=new (Object.getPrototypeOf(async function(){}).constructor)('evidence','renderer','expect','table','webMercatorPreviewCoordinate','output','expectedOriginalLines',source.slice(start,end))
function expect(actual) { return {toContainText(value){assert.ok(actual.includes(value))}} }
expect.poll=fn=>({async toEqual(value){assert.deepEqual(await fn(),value)}})
const helperStart=source.indexOf('function expectedOriginalLines(');const helperEnd=source.indexOf("test('同一普通User核对",helperStart)
const expectedOriginalLines=new Function('evidence','port',source.slice(helperStart,helperEnd)+'\nreturn expectedOriginalLines(evidence,port)')
const fields=Object.keys(expected[0].properties)
const table={locator(){return fields.join(' ')}}
async function run(rows,projection='EPSG:3857') {
 const features=rows.map(row=>({get:name=>row.properties[name],getGeometry:()=>({getCoordinates:()=>row.geometry.coordinates.map(webMercatorPreviewCoordinate)})}))
 const map={getView:()=>({getAnimating:()=>false,getProjection:()=>({getCode:()=>projection})}),getLayers:()=>({getArray:()=>[{getSource:()=>({getFeatures:()=>features})}]})}
 const renderer={evaluate:async(fn,args)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},args)}
 await check({split_line_expected:expected,line_component_expected:expected,line_chain_expected:expected,line_chain_members_expected:expected},renderer,expect,table,webMercatorPreviewCoordinate,{port},expectedOriginalLines)
}
await run(expected)
await run([...expected].reverse()) // OL feature order need not equal saved row order.
const mutations=[rows=>rows.pop(),rows=>rows[0].properties[fields[0]]='wrong',rows=>rows[0].properties.grade++,
 rows=>rows[0].geometry.coordinates.reverse()]
if(port==='split_lines') mutations.push(rows=>rows.splice(34,1),rows=>rows[38].properties.part_no=1,
 rows=>rows[20].geometry.coordinates[1]=[102,202],rows=>rows[30].geometry.coordinates.reverse(),
 rows=>rows[41].geometry.coordinates.splice(1,1))
if(port==='split_lines'||port==='line_components') mutations.push(rows=>rows[0].properties.part_no++,
 rows=>rows[0].properties[port==='split_lines'?'segment_no':'component_id']++,rows=>rows[3].properties.note='',
 rows=>port==='split_lines' ? rows[15].geometry.coordinates[1][0]=1e-12 : rows[10].geometry.coordinates[0][0]=1)
else mutations.push(rows=>rows[0].properties.chain_id++,
 rows=>rows[0].properties[port==='line_chains'?'member_count':'member_no']++,
 rows=>rows[port==='line_chains'?8:11].geometry.coordinates[0][0]=1)
if(port==='line_chain_members')mutations.push(rows=>rows[0].properties.source_part_no++,
 rows=>rows[0].properties.is_reversed=true,rows=>rows[0].properties.is_reversed=0,rows=>rows[0].properties.note='')
for(const mutate of mutations){
 const rows=structuredClone(expected);mutate(rows)
 await assert.rejects(run(rows),{code:'ERR_ASSERTION'})
}
await assert.rejects(run(expected,'EPSG:4326'),{code:'ERR_ASSERTION'})
'''
        for port,expected in [('split_lines',m.fixture.split_line_expected_features()),('line_components',m.fixture.line_component_expected_features()),('line_chains',m.fixture.line_chain_expected_features()),('line_chain_members',m.fixture.line_chain_expected_features(members=True))]:
            result=subprocess.run(['node','--input-type=module','-e',script,json.dumps(expected),
                str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),
                (repository/'manager/frontend/e2e/geometry-preview-coordinate.js').as_uri(),port],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)


    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.env = {**ENV, "ADDP_ONLINE_ARTIFACT_DIR": directory.name}

    def test_public_dag_keeps_default_faces_diagnostics_and_checked_repair_branch(self):
        tasks = m.definition('source','parent','invalid','snap-source','snap-ref','overlap-source','gap-source','coverage-source','dangling-source','line-overlap-source','self-source','intersection-source','self-intersection-source','split-source','component-source','chain-source')['tasks']
        self.assertEqual([node['operator'] for node in tasks], ['load','polygonize','save','save','load','check_geometry','repair_geometry','check_geometry','save','load','load','snap_geometry','save','load','check_polygon_overlap','save','load','load','check_polygon_gap','save','load','check_dangling_points','save','load','check_line_overlap','save','load','check_line_self_overlap','save','load','check_line_intersection','save','load','check_line_self_intersection','save','load','split_line_intersections','save','load','group_line_components','save','load','merge_line_chains','save','save'])
        self.assertEqual(tasks[2]['params']['input_df'], {'$ref': 'build'})
        self.assertEqual(tasks[3]['params']['input_df'], {'$ref': 'build', 'port': 'diagnostics'})
        self.assertTrue(all(node['params']['mode'] == 'fail' for node in tasks if node['operator'] == 'save'))

    def test_exact_fifteen_outputs_and_scans_are_mandatory(self):
        value = execution()
        targets = {key: value['outputs']['save_'+key]['resource']['locator'] for key in m.fixture.PORTS}
        sources = [f'addp://engine/3/path/{name}?type=file&item_id={identifier}' for identifier, name in enumerate(m.fixture.SOURCES, 10)]
        m.validate_success(value, sources, targets)
        for mutate in (lambda v: v['outputs'].pop('save_diagnostics'), lambda v: v['outputs'].pop('save_repaired'), lambda v: v['outputs'].pop('save_overlaps'), lambda v: v['outputs'].pop('save_gaps'), lambda v: v['outputs'].pop('save_dangling'), lambda v: v['outputs'].pop('save_line_overlaps'), lambda v: v['outputs'].pop('save_line_self_overlaps'), lambda v: v['outputs'].pop('save_line_intersections'), lambda v: v['outputs'].pop('save_line_self_intersections'), lambda v: v['outputs'].pop('save_split_lines'), lambda v: v['outputs'].pop('save_line_components'), lambda v: v['outputs'].pop('save_line_chains'), lambda v: v['outputs'].pop('save_line_chain_members'),
            lambda v: v['metadata']['lineage_facts']['inputs'].pop(),
            lambda v: v['metadata']['lineage_facts']['inputs'][1].update(locator=v['metadata']['lineage_facts']['inputs'][0]['locator']), lambda v: v['metadata']['result']['meta_scan_runs'].pop(),
            lambda v: v['outputs']['save_faces']['resource'].pop('write_mode'),
            lambda v: v['metadata']['lineage_facts']['outputs'][0].pop('write_mode'),
            lambda v: v['metadata']['lineage_facts']['outputs'][1].update(write_mode='fail'),
            lambda v: v['metadata']['result']['meta_scan_runs'][1].update(execution_id='scan-faces'),
            lambda v: v['metadata']['result']['meta_scan_runs'][0].update(target_locator='parent'),
            lambda v: v['metadata']['lineage_facts']['outputs'].pop()):
            changed = copy.deepcopy(value); mutate(changed)
            with self.subTest(mutate=mutate), self.assertRaises(m.SuiteError): m.validate_success(changed, sources, targets)

    def test_lineage_inputs_bind_exact_read_ports_independently_of_array_order(self):
        value = execution()
        sources = [f'addp://engine/3/path/{name}?type=file&item_id={identifier}' for identifier, name in enumerate(m.fixture.SOURCES, 10)]
        targets = {key: value['outputs']['save_'+key]['resource']['locator'] for key in m.fixture.PORTS}
        self.assertEqual([entry['port'] for entry in value['metadata']['lineage_facts']['inputs']],
                         ['input.dangling_input', 'input.gap_coverage', 'input.gap_input', 'input.line_chain_input', 'input.line_component_input', 'input.line_intersection_input', 'input.line_overlap_input', 'input.line_self_intersection_input', 'input.line_self_overlap_input', 'input.load', 'input.load_invalid', 'input.overlap_input', 'input.snap_input', 'input.snap_reference', 'input.split_line_input'])
        m.validate_success(value, sources, targets)
        value['metadata']['lineage_facts']['inputs'].reverse()
        m.validate_success(value, sources, targets)
        def swap_locators(inputs):
            inputs[0]['locator'], inputs[1]['locator'] = inputs[1]['locator'], inputs[0]['locator']
        for mutate in (
            lambda inputs: inputs[0].pop('port'),
            lambda inputs: inputs[0].update(port='input.foreign'),
            lambda inputs: inputs[0].update(port=inputs[1]['port']),
            lambda inputs: inputs.append(copy.deepcopy(inputs[0])),
            lambda inputs: inputs.extend([{'port':'input.unused','locator':'unrelated'}]),
            swap_locators,
        ):
            changed = copy.deepcopy(value)
            mutate(changed['metadata']['lineage_facts']['inputs'])
            with self.subTest(mutate=mutate), self.assertRaises(m.SuiteError):
                m.validate_success(changed, sources, targets)

    def test_complete_chain_preserves_same_user_and_does_not_manually_scan_outputs(self):
        client = Client()
        report = m.run_scenario(Path('.'), self.env, client, physical, lambda repo, env, evidence: evidence, diagnostic_runner=lambda *args: {'cause':'invalid_snapped_geometry'})
        self.assertEqual(report['result'], 'passed'); self.assertEqual(len(report['outputs']), 15)
        self.assertEqual(len(report['identity']['permissions_verified']), 11)
        self.assertEqual(len([call for call in client.calls if call[0] == 'POST' and '/scan/' in call[1]]), 15)
        self.assertEqual(report['schema_version'],'addp.vector-topology-persistence/v14')
        self.assertEqual(report['physical'],report['post_browser_physical'])
        self.assertEqual(report['stable_outputs'],execution()['outputs'])
        self.assertEqual(report['browser']['overlap_expected'],m.fixture.overlap_expected_features())
        self.assertEqual(report['browser']['gap_expected'],m.fixture.gap_expected_features())
        self.assertEqual(report['browser']['dangling_expected'],m.fixture.dangling_expected_features())

    def test_browser_requires_current_v14_and_exact_fifteenth_output_evidence(self):
        evidence={'execution_id':'current','outputs':[{'port':'gaps'}],'overlap_expected':m.fixture.overlap_expected_features(),'gap_expected':m.fixture.gap_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        for schema,changed,valid in [('addp.topology-browser/v13',False,False),('addp.topology-browser/v14',True,False),('addp.topology-browser/v14',False,True)]:
            def run(*args,**kwargs):
                report={**evidence,'schema_version':schema,'result':'passed'}
                if changed:report['outputs']=[]
                path.write_text(json.dumps(report))
                return SimpleNamespace(returncode=0)
            with self.subTest(schema=schema,changed=changed),patch.object(m.subprocess,'run',side_effect=run):
                if valid:self.assertEqual(m.browser(Path('.'),self.env,evidence)['result'],'passed')
                else:
                    with self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_browser_cannot_omit_gap_oracle_from_current_report(self):
        evidence={'execution_id':'current','outputs':[{'port':'gaps'}],'gap_expected':m.fixture.gap_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],
                'schema_version':'addp.topology-browser/v14','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):
            m.browser(Path('.'),self.env,evidence)

    def test_lineage_failure_retains_all_fifteen_strict_sizes_and_incomplete_physical_checkpoint(self):
        self.env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'] = 'must-not-archive'
        with patch.object(m.raster, 'wait_lineage', side_effect=m.SuiteError('lineage unavailable')):
            with self.assertRaisesRegex(m.SuiteError, 'lineage unavailable'):
                m.run_scenario(Path('.'), self.env, Client(), physical)
        saved = Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-before-lineage.json'
        checkpoint = json.loads(saved.read_text())
        self.assertEqual((checkpoint['stage'], checkpoint['result']), ('before_lineage', 'incomplete'))
        self.assertEqual(checkpoint['physical'], physical())
        self.assertEqual([entry['size_evidence'] for entry in checkpoint['outputs']],
                         [{'physical_size_bytes': 1000, 'meta_size_bytes': 1000}]*3 + [{'physical_size_bytes': 1000, 'meta_size_bytes': 1000, 'layer_name': name} for name in ('snapped','overlaps','gaps','dangling','line-overlaps','line-self-overlaps','line-intersections','line-self-intersections','split-lines','line-components','line-chains','line-chain-members')])
        self.assertTrue(all(entry['triggered_by'] == 33 and entry['tenant_id'] == 2 for entry in checkpoint['automatic_scans']))
        self.assertEqual(checkpoint['lineage_facts'], execution()['metadata']['lineage_facts'])
        self.assertNotIn('must-not-archive', saved.read_text())
        self.assertFalse((saved.parent/'topology-persistence.json').exists())

    def test_failed_snap_rejects_extra_scan_and_wrong_monitor_status(self):
        class WrongClient(Client):
            submitted_failure = False
            def request(self, method, path, expected, body=None):
                response = super().request(method,path,expected,body)
                if method == 'POST' and response.payload.get('execution_id') == 'failed-snap':
                    self.submitted_failure = True
                if self.submitted_failure and self.problem == 'scan' and path.startswith('/api/v1/meta/scan/runs?'):
                    response.payload['items'].append({'execution_id':'unexpected'})
                    response.payload['total'] += 1
                if self.problem == 'monitor' and '/monitor/executions/' in path and path.endswith('failed-snap'):
                    response.payload['status'] = 'success'
                return response
        for problem in ('scan','monitor'):
            client = WrongClient(); client.problem = problem
            with self.subTest(problem=problem), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'),self.env,client,physical,
                    lambda *args:self.fail('failure must stop before browser'), diagnostic_runner=lambda *args:{})

    def test_browser_failure_preserves_verified_negative_case_without_claiming_suite_success(self):
        self.env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'] = 'must-not-archive'
        def unavailable(*args): raise m.SuiteError('map failed')
        with self.assertRaisesRegex(m.SuiteError,'map failed'):
            m.run_scenario(Path('.'),self.env,Client(),physical,unavailable,
                diagnostic_runner=lambda *args:{'cause':'invalid_snapped_geometry'})
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-snap-failure.json'
        saved=json.loads(path.read_text())
        self.assertEqual((saved['result'],saved['stage']),('incomplete','collapsed_snap_validated'))
        self.assertEqual(saved['failed_snap']['execution_id'],'failed-snap')
        self.assertEqual(saved['failed_snap']['observation'],{'status':'failed','outputs':None,'lineage_facts':None,'meta_scan_runs':None})
        self.assertEqual(len(saved['scan_inventory']),30)
        self.assertEqual(saved['identity']['principal_id'],'33')
        self.assertNotIn('must-not-archive',path.read_text())
        self.assertFalse((path.parent/'topology-persistence.json').exists())

    def test_geo_package_container_boundary_and_both_snap_lineage_sources_are_required(self):
        item = Client().targets[33]
        m.validate_item(item,physical()['targets']['snapped.gpkg'],'snapped.gpkg')
        for mutate in (
            lambda value:value['attributes']['type_info']['container']['children'].clear(),
            lambda value:value['attributes']['item'].update(data_type='vector'),
            lambda value:value.update(size_bytes=True),
        ):
            changed=copy.deepcopy(item);mutate(changed)
            with self.subTest(mutate=mutate), self.assertRaises(m.SuiteError):
                m.validate_item(changed,physical()['targets']['snapped.gpkg'],'snapped.gpkg')
        seen=[]
        def lineage(client,source_id,target_id,execution_id,timeout):
            seen.append((source_id,target_id,execution_id));return {}
        with patch.object(m.raster,'wait_lineage',side_effect=lineage):
            m.run_scenario(Path('.'),self.env,Client(),physical,lambda repo,env,evidence:evidence,
                           diagnostic_runner=lambda *args:{})
        self.assertEqual(seen,[(10,30,'execution'),(10,31,'execution'),(11,32,'execution'),
                           (12,33,'execution'),(13,33,'execution'),(14,34,'execution'),(15,35,'execution'),(16,35,'execution'),(17,36,'execution'),(18,37,'execution'),(19,38,'execution'),(20,39,'execution'),(21,40,'execution'),(22,41,'execution'),(23,42,'execution'),(24,43,'execution'),(24,44,'execution')])

    def test_gap_dag_binds_both_read_roles_and_rejects_missing_or_swapped_facts(self):
        tasks=m.definition('line','parent','invalid','snap','reference','overlap','gap-source','coverage','dangling','line-overlap-source','self-source','intersection-source','self-intersection-source','split-source','component-source','chain-source')['tasks'][:-25]
        self.assertEqual(tasks[-4]['params'], {'locator':'gap-source'})
        self.assertEqual(tasks[-3]['params'], {'locator':'coverage'})
        self.assertEqual(tasks[-2]['params'], {'input_gdf':{'$ref':'gap_input'},'coverage_gdf':{'$ref':'gap_coverage'}})
        self.assertEqual(tasks[-2]['depends_on'],['gap_input','gap_coverage'])
        sources=[f'addp://engine/3/path/{name}?type=file&item_id={identifier}' for identifier,name in enumerate(m.fixture.SOURCES,10)]
        targets={port:f'addp://engine/3/path/outputs/{name}?type=file' for port,name in zip(m.fixture.PORTS,m.fixture.TARGETS)}
        for problem in ('missing','swap','duplicate','scan'):
            value=execution();inputs=value['metadata']['lineage_facts']['inputs']
            if problem=='missing': inputs.pop(0)
            elif problem=='swap': inputs[0]['locator'],inputs[1]['locator']=inputs[1]['locator'],inputs[0]['locator']
            elif problem=='duplicate': inputs[0]['port']=inputs[1]['port']
            else: value['metadata']['result']['meta_scan_runs'][-1]['execution_id']='scan-overlaps'
            with self.subTest(problem=problem),self.assertRaises(m.SuiteError):m.validate_success(value,sources,targets)

    def test_gap_missing_lineage_on_either_source_blocks_browser_acceptance(self):
        for missing_source in (15,16):
            def lineage(client,source_id,target_id,execution_id,timeout):
                if target_id==35 and source_id==missing_source:raise m.SuiteError('gap lineage missing')
                return {}
            with self.subTest(source=missing_source),patch.object(m.raster,'wait_lineage',side_effect=lineage):
                with self.assertRaisesRegex(m.SuiteError,'gap lineage missing'):
                    m.run_scenario(Path('.'),self.env,Client(),physical,lambda *args:self.fail('both gap sources required'))

    def test_gap_missing_physical_source_and_wrong_scan_identity_cannot_pass(self):
        def missing(*args):
            value=physical();value['sources'].pop('gap-coverage.gpkg');return value
        with self.assertRaisesRegex(m.SuiteError,'physical target inventory'):
            m.run_scenario(Path('.'),self.env,Client(),missing,lambda *args:self.fail('missing boundary must fail'))
        class WrongGapScan(Client):
            def request(self,method,path,expected,body=None):
                response=super().request(method,path,expected,body)
                if '/meta/executions/scan-gaps' in path:response.payload['triggered_by']=44
                return response
        with self.assertRaisesRegex(m.SuiteError,'User or Tenant'):
            m.run_scenario(Path('.'),self.env,WrongGapScan(),lambda *args:self.fail('wrong gap scan actor'))

    def test_extra_permission_or_wrong_scan_actor_blocks_acceptance(self):
        for change in ('permission','actor'):
            client = Client()
            if change == 'permission': client.permissions.append('develop.task.create')
            else: client.actor = 44
            with self.subTest(change=change), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'), self.env, client, lambda *args: self.fail('must stop before physical acceptance'))

    def test_scan_identity_requires_public_integer_user_and_tenant(self):
        for field, values in (('actor', (None, True, 0, -1, '33', 33.)),
                              ('scan_tenant', (None, True, 0, -1, 3, '2', 2.))):
            for value in values:
                client = Client(); setattr(client, field, value)
                with self.subTest(field=field, value=value), self.assertRaisesRegex(m.SuiteError, 'User or Tenant'):
                    m.run_scenario(Path('.'), self.env, client, lambda *args: self.fail('must stop before physical acceptance'))

    def test_private_actor_cannot_replace_public_initiator(self):
        class PrivateOnlyClient(Client):
            def request(self, method, path, expected, body=None):
                if '/meta/executions/' in path:
                    return SimpleNamespace(payload={'status': 'success', 'tenant_id': 2, 'actor_principal_id': 33})
                return super().request(method, path, expected, body)
        with self.assertRaisesRegex(m.SuiteError, 'User or Tenant'):
            m.run_scenario(Path('.'), self.env, PrivateOnlyClient(), lambda *args: self.fail('private actor must not be consumed'))

    def test_invalid_size_or_browser_modification_cannot_pass(self):
        for size in (True, 0, 1000., '1000', 999):
            item = Client().targets[30]; item['size_bytes'] = size
            with self.subTest(size=size), self.assertRaises(m.SuiteError): m.validate_item(item, physical()['targets']['faces.geojson'], 'faces.geojson')
        calls = []
        def changed(*args):
            value = physical(); calls.append(1)
            if len(calls) > 1: value['sources']['invalid.geojson']['sha256'] = 'modified'
            return value
        with self.assertRaisesRegex(m.SuiteError, 'changed scans, sources or saved outputs'):
            m.run_scenario(Path('.'), self.env, Client(), changed, lambda repo, env, evidence: evidence, diagnostic_runner=lambda *args: {})

    def test_snap_public_dag_and_failure_no_publication_contract(self):
        for failed, tolerance, name in ((False,.75,'snapped.gpkg'),(True,1.25,'failed-snap.gpkg')):
            tasks = m.snap_definition('source','reference','parent',failed=failed)['tasks']
            self.assertEqual(tasks[2]['params'], {'input_gdf':{'$ref':'snap_input'},'reference_gdf':{'$ref':'snap_reference'},'tolerance':tolerance})
            self.assertEqual(tasks[-1]['params']['target_name'],name)
        self.assertEqual(m.output_sources('snapped'),(2,3))
        self.assertEqual(m.output_sources('overlaps'),(4,))
        self.assertEqual(m.output_sources('gaps'),(5,6))
        for value in ({'status':'success','metadata':{}}, {'status':'failed','outputs':{'save':{}},'metadata':{}},
                      {'status':'failed','metadata':{'lineage_facts':{'outputs':[{'locator':'unexpected'}]}}},
                      {'status':'failed','metadata':{'result':{'meta_scan_runs':[{'execution_id':'unexpected'}]}}}):
            with self.subTest(value=value), self.assertRaises(m.SuiteError): m.validate_failed_snap(value)
        m.validate_failed_snap({'status':'failed','metadata':{}})

    def test_collapsed_snap_diagnostic_rejects_wrong_owner_execution_and_cause(self):
        directory = Path(self.env['ADDP_ONLINE_ARTIFACT_DIR']); log = directory/'develop'/'backend'/'runtime.jsonl'
        log.parent.mkdir(parents=True)
        event = {'module_name':'develop','role':'backend','message':'[DevExecutor] 工作流引擎调用失败: execution_id=failed-snap err=任务 snap 执行失败: snapping produced invalid geometries; reduce tolerance or revise reference'}
        env = {'ADDP_RUNTIME_LOG_ROOT':str(directory)}
        for field, value in (('module_name','manager'),('role','frontend'),('message','foreign failure')):
            wrong = {**event,field:value};log.write_text(json.dumps(wrong)+'\n')
            with self.subTest(field=field), self.assertRaises(m.SuiteError): m.collapsed_snap_diagnostic(Path('.'),env,'failed-snap',timeout=0)
        log.write_text(json.dumps(event)+'\n')
        with self.assertRaises(m.SuiteError): m.collapsed_snap_diagnostic(Path('.'),env,'other-execution',timeout=0)
        self.assertEqual(m.collapsed_snap_diagnostic(Path('.'),env,'failed-snap',timeout=0)['cause'],'invalid_snapped_geometry')


    def test_dangling_dag_and_current_report_require_original_source_evidence(self):
        tasks=m.definition('line','parent','invalid','snap','reference','overlap','gap','coverage','dangle-source','line-overlap-source','self-source','intersection-source','self-intersection-source','split-source','component-source','chain-source')['tasks'][:-22]
        self.assertEqual(tasks[-3]['params'],{'locator':'dangle-source'})
        self.assertEqual(tasks[-2]['params'],{'input_gdf':{'$ref':'dangling_input'},'id_field':'route_key'})
        self.assertEqual(tasks[-1]['params']['target_name'],'dangling.gpkg')
        self.assertEqual(m.output_sources('dangling'),(7,))
        evidence={'execution_id':'current','outputs':[{'port':'dangling'}],'dangling_expected':m.fixture.dangling_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],
                'schema_version':'addp.topology-browser/v14','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_dangling_missing_lineage_blocks_browser_acceptance(self):
        def lineage(client,source_id,target_id,execution_id,timeout):
            if source_id==17 and target_id==36:raise m.SuiteError('dangling lineage missing')
            return {}
        with patch.object(m.raster,'wait_lineage',side_effect=lineage),self.assertRaisesRegex(m.SuiteError,'dangling lineage missing'):
            m.run_scenario(Path('.'),self.env,Client(),physical,
                lambda *args:self.fail('missing source lineage must stop before browser'))

    def test_point_map_identity_comparison_is_locale_independent_and_keeps_multiplicity(self):
        # Execute the actual Online Point assertion in separate Node/browser
        # realms. Their default collations differ on the original Chinese IDs.
        script = r'''
import assert from 'node:assert/strict'
import fs from 'node:fs'
import vm from 'node:vm'
import { webMercatorPreviewCoordinate } from './manager/frontend/e2e/geometry-preview-coordinate.js'
const source = fs.readFileSync('console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8')
const start = source.indexOf("    if (['dangling','line_intersections','line_self_intersections'].includes(output.port)) {", source.indexOf('const table = frame.locator'))
const end = source.indexOf("    if (output.port === 'gaps') {",start)
assert(start > 0 && end > start)
const check = new (Object.getPrototypeOf(async function(){}).constructor)('evidence','renderer','expect','table','webMercatorPreviewCoordinate','output',source.slice(start,end))
const {expected,intersection,self} = JSON.parse(fs.readFileSync(0,'utf8'))
assert.equal(expected.length,intersection?10:self?15:17)
const identities=intersection?['source_id_a','source_id_b']:['source_id']
const originalCompare = String.prototype.localeCompare
String.prototype.localeCompare = function(other){ return originalCompare.call(this,other,'en-US') }
const nodeOrder = ['孤线','干线'].sort((a,b) => a.localeCompare(b))
const browserOrder = ['孤线','干线'].sort(new Intl.Collator('zh-CN').compare)
assert.notDeepEqual(nodeOrder,browserOrder,'the regression must exercise different collations')
const expectedPoints = expected.map(row => ({...row.properties,coordinates:webMercatorPreviewCoordinate(row.geometry.coordinates)}))
async function run(points) {
  const features = points.map(point => ({get:name => point[name],getGeometry:() => ({getCoordinates:() => point.coordinates})}))
  const layer = { getSource: () => ({ getFeatures: () => features }) }
  const map = {
    getView: () => ({ getAnimating: () => false, getProjection: () => ({ getCode: () => 'EPSG:3857' }) }),
    getLayers: () => ({ getArray: () => [layer] })
  }
  const element = {__vueParentComponent:{setupState:{mapInstance:map}}}
  const context = vm.createContext({element})
  vm.runInContext("const compare=String.prototype.localeCompare;String.prototype.localeCompare=function(other){return compare.call(this,other,'zh-CN')}",context)
  const renderer = {evaluate:async (fn,arg) => JSON.parse(JSON.stringify(vm.runInContext('('+fn.toString()+')(element,'+JSON.stringify(arg)+')',context)))}
  const expect = {poll:fn => ({toEqual:async value => assert.deepEqual(await fn(),value)})}
  const table = {locator:() => ({})}; const assertion = expect
  const callable = Object.assign(() => ({toContainText:async() => {}}),assertion)
  await check({dangling_expected:expected,line_intersection_expected:expected,line_self_intersection_expected:expected},renderer,callable,table,webMercatorPreviewCoordinate,{port:intersection?'line_intersections':self?'line_self_intersections':'dangling'})
}
await run([...expectedPoints].reverse())
for (const kind of ['missing','duplicate','wrong-source','wrong-coordinate','micro-gap-rounded','source-coordinate-mismatch']) {
  const points = structuredClone(expectedPoints)
  if(kind==='missing') points.pop()
  if(kind==='duplicate') points.push(structuredClone(points[0]))
  if(kind==='wrong-source') points[0][identities[0]]='闭环'
  if(kind==='wrong-coordinate') points[0].coordinates[0]+=1
  if(kind==='micro-gap-rounded') (intersection?points.at(-1):self?points[8]:points.find(p => p.source_id==='微乙')).coordinates=webMercatorPreviewCoordinate(intersection?[0,120]:self?[0,160]:[251,0])
  if(kind==='source-coordinate-mismatch') [points[0][identities[0]],points[2][identities[0]]]=[points[2][identities[0]],points[0][identities[0]]]
  await assert.rejects(run(points),{code:'ERR_ASSERTION'},kind)
}
console.log('cross-realm Chinese/English Point identity and six corruptions verified')
'''
        for intersection,self_check,expected in [(False,False,m.fixture.dangling_expected_features()),(True,False,m.fixture.line_intersection_expected_features()),(False,True,m.fixture.line_self_intersection_expected_features())]:
            result = m.subprocess.run(['node','--input-type=module','-e',script],
                input=json.dumps({'expected':expected,'intersection':intersection,'self':self_check},ensure_ascii=False),
                text=True,capture_output=True,timeout=30)
            with self.subTest(intersection=intersection,self_check=self_check):self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_point_map_pixels_reject_empty_water_and_keep_line_polygon_checks(self):
        script = r'''
import assert from 'node:assert/strict'
import fs from 'node:fs'
const source = fs.readFileSync('console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8')
const start = source.indexOf('    let ',source.indexOf("    if (['dangling','line_intersections','line_self_intersections'].includes(output.port)) {",source.indexOf('const table = frame.locator')))
const end = source.indexOf('    const rendered =',start)
assert(start > 0 && end > start)
const check = new (Object.getPrototypeOf(async function(){}).constructor)('renderer','expect','output',source.slice(start,end))
async function run(port,rgba) {
  const data=Uint8ClampedArray.from(Array.from({length:40},() => rgba).flat())
  const canvases=[{getContext:() => ({getImageData:() => ({data})}),width:40,height:1}]
  const renderer={locator:() => ({evaluateAll:async(fn,arg) => fn(canvases,arg)})}
  const expect={poll:fn => ({toBeGreaterThan:async minimum => assert((await fn())>minimum)})}
  await check(renderer,expect,{port})
}
await run('dangling',[64,158,255,255])
await run('line_intersections',[64,158,255,255])
await run('line_self_intersections',[64,158,255,255])
await assert.rejects(run('line_self_intersections',[173,216,230,255]),{code:'ERR_ASSERTION'},'water cannot substitute self-intersection Points')
await assert.rejects(run('line_intersections',[173,216,230,255]),{code:'ERR_ASSERTION'},'basemap water cannot substitute intersection Points')
await assert.rejects(run('dangling',[173,216,230,255]),{code:'ERR_ASSERTION'},'basemap water cannot count as Points')
await assert.rejects(run('dangling',[103,194,58,255]),{code:'ERR_ASSERTION'},'green line/polygon pixels cannot substitute Points')
await assert.rejects(run('dangling',[64,158,255,0]),{code:'ERR_ASSERTION'},'transparent Points are not visible')
await run('gaps',[103,194,58,255])
await assert.rejects(run('gaps',[64,158,255,255]),{code:'ERR_ASSERTION'},'old green geometry assertion remains strict')
console.log('Point foreground pixels and preserved polygon assertions verified')
'''
        result=m.subprocess.run(['node','--input-type=module','-e',script],
            text=True,capture_output=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)


    def test_line_overlap_dag_and_lineage_bind_original_source(self):
        tasks=m.definition('line','parent','invalid','snap','reference','overlap','gap','coverage','dangling','line-overlap-source','self-source','intersection-source','self-intersection-source','split-source','component-source','chain-source')['tasks'][:-19]
        self.assertEqual(tasks[-3]['params'],{'locator':'line-overlap-source'})
        self.assertEqual(tasks[-2]['operator'],'check_line_overlap')
        self.assertEqual(tasks[-2]['params'],{'input_gdf':{'$ref':'line_overlap_input'},'id_field':'route_key'})
        self.assertEqual(tasks[-1]['params']['target_name'],'line-overlaps.gpkg')
        self.assertEqual(m.output_sources('line_overlaps'),(8,))
        def lineage(client,source_id,target_id,execution_id,timeout):
            if source_id==18 and target_id==37:raise m.SuiteError('line overlap lineage missing')
            return {}
        with patch.object(m.raster,'wait_lineage',side_effect=lineage),self.assertRaisesRegex(m.SuiteError,'line overlap lineage missing'):
            m.run_scenario(Path('.'),self.env,Client(),physical,lambda *args:self.fail('must stop before browser'))

    def test_line_overlap_browser_cannot_omit_expected_pairs(self):
        evidence={'execution_id':'current','outputs':[{'port':'line_overlaps'}],'line_overlap_expected':m.fixture.line_overlap_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],
                'schema_version':'addp.topology-browser/v14','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_line_overlap_map_binds_both_ids_length_and_exact_display_components(self):
        script=r'''
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { webMercatorPreviewCoordinate } from './manager/frontend/e2e/geometry-preview-coordinate.js'
const source=fs.readFileSync('console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8')
const start=source.indexOf("    if (['line_overlaps','line_self_overlaps'].includes(output.port)) {",source.indexOf('const table = frame.locator'))
const end=source.indexOf("    if (output.port === 'snapped') {",start)
assert(start>0 && end>start)
const check=new (Object.getPrototypeOf(async function(){}).constructor)('evidence','renderer','expect','table','webMercatorPreviewCoordinate','output','canonical',source.slice(start,end))
const {expected,self}=JSON.parse(fs.readFileSync(0,'utf8'))
assert.equal(expected.length,self?8:9)
const rows=expected.map(row=>({...row.properties,coordinates:row.geometry.coordinates.map(part=>part.map(webMercatorPreviewCoordinate))}))
const canonical=points=>[points,[...points].reverse()].map(value=>JSON.stringify(value)).sort()[0]
async function run(values) {
  const features=values.map(row=>({get:name=>row[name],getGeometry:()=>({getCoordinates:()=>row.coordinates})}))
  const map={getView:()=>({getAnimating:()=>false,getResolution:()=>1,getProjection:()=>({getCode:()=> 'EPSG:3857'})}),
    getLayers:()=>({getArray:()=>[{getSource:()=>({getFeatures:()=>features})}]})}
  const renderer={evaluate:async (fn,arg)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},arg)}
  const expect=Object.assign(()=>({toContainText:async()=>{}}),{poll:fn=>({toEqual:async wanted=>assert.deepEqual(await fn(),wanted)})})
  await check({line_overlap_expected:expected,line_self_overlap_expected:expected},renderer,expect,{locator:()=>({})},webMercatorPreviewCoordinate,{port:self?'line_self_overlaps':'line_overlaps'},canonical)
}
await run([...rows].reverse())
for(const kind of ['missing','duplicate','source-a','source-b','length','tiny-length','coordinate','part','pair-binding']) {
  const values=structuredClone(rows)
  if(kind==='missing')values.pop()
  if(kind==='duplicate')values[1]=structuredClone(values[0])
  if(kind==='source-a')values[0][self?'source_id':'source_id_a']='1'
  if(kind==='source-b')values[0][self?'source_id':'source_id_b']='接点'
  if(kind==='length')values[0].overlap_length=3
  if(kind==='tiny-length')values[self?4:6].overlap_length=0
  if(kind==='coordinate')values[0].coordinates[0][0][0]+=1
  if(kind==='part')values[self?3:5].coordinates.pop()
  if(kind==='pair-binding'){const field=self?'source_id':'source_id_b';[values[0][field],values[1][field]]=[values[1][field],values[0][field]]}
  await assert.rejects(run(values),{code:'ERR_ASSERTION'},kind)
}
console.log('MultiLineString pair identity, length and coordinates: nine corruptions rejected')
'''
        for single, expected in [(False,m.fixture.line_overlap_expected_features()),(True,m.fixture.line_self_overlap_expected_features())]:
            result=m.subprocess.run(['node','--input-type=module','-e',script],
                input=json.dumps({'self':single,'expected':expected},ensure_ascii=False),
                text=True,capture_output=True,timeout=30)
            with self.subTest(single=single):self.assertEqual(result.returncode,0,result.stdout+result.stderr)


    def test_line_self_overlap_dag_and_lineage_bind_only_original_source(self):
        tasks=m.definition('line','parent','invalid','snap','reference','overlap','gap','coverage','dangling','line-overlap-source','self-source','intersection-source','self-intersection-source','split-source','component-source','chain-source')['tasks'][:-16]
        self.assertEqual(tasks[-3]['params'],{'locator':'self-source'})
        self.assertEqual(tasks[-2]['operator'],'check_line_self_overlap')
        self.assertEqual(tasks[-2]['params'],{'input_gdf':{'$ref':'line_self_overlap_input'},'id_field':'route_key'})
        self.assertEqual(tasks[-1]['params']['target_name'],'line-self-overlaps.gpkg')
        self.assertEqual(m.output_sources('line_self_overlaps'),(9,))
        def lineage(client,source_id,target_id,execution_id,timeout):
            if source_id==19 and target_id==38:raise m.SuiteError('self overlap lineage missing')
            return {}
        with patch.object(m.raster,'wait_lineage',side_effect=lineage),self.assertRaisesRegex(m.SuiteError,'self overlap lineage missing'):
            m.run_scenario(Path('.'),self.env,Client(),physical,lambda *args:self.fail('must stop before browser'))

    def test_line_self_overlap_browser_cannot_omit_original_source_and_expected_results(self):
        evidence={'execution_id':'current','outputs':[{'port':'line_self_overlaps'}],'line_self_overlap_expected':m.fixture.line_self_overlap_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],
                'schema_version':'addp.topology-browser/v14','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)


    def test_line_intersection_dag_scan_and_lineage_bind_the_original_file(self):
        tasks=m.definition('line','parent','invalid','snap','reference','overlap','gap','coverage','dangling','line-overlap-source','self-source','intersection-source','self-intersection-source','split-source','component-source','chain-source')['tasks'][:-13]
        self.assertEqual(tasks[-3]['params'],{'locator':'intersection-source'})
        self.assertEqual(tasks[-2]['operator'],'check_line_intersection')
        self.assertEqual(tasks[-2]['params'],{'input_gdf':{'$ref':'line_intersection_input'},'id_field':'route_key'})
        self.assertEqual(tasks[-1]['params']['target_name'],'line-intersections.gpkg')
        self.assertEqual(m.output_sources('line_intersections'),(10,))
        def lineage(client,source_id,target_id,execution_id,timeout):
            if source_id==20 and target_id==39:raise m.SuiteError('point pair lineage missing')
            return {}
        with patch.object(m.raster,'wait_lineage',side_effect=lineage),self.assertRaisesRegex(m.SuiteError,'point pair lineage missing'):
            m.run_scenario(Path('.'),self.env,Client(),physical,lambda *args:self.fail('must stop before browser'))
        class WrongScan(Client):
            def request(self,method,path,expected,body=None):
                response=super().request(method,path,expected,body)
                if '/meta/executions/scan-line_intersections' in path:response.payload['triggered_by']=44
                return response
        with self.assertRaisesRegex(m.SuiteError,'User or Tenant'):
            m.run_scenario(Path('.'),self.env,WrongScan(),lambda *args:self.fail('wrong scan actor'))
        report=m.run_scenario(Path('.'),self.env,Client(),physical,lambda repo,env,evidence:evidence,diagnostic_runner=lambda *args:{})
        self.assertEqual(report['browser']['line_intersection_expected'],m.fixture.line_intersection_expected_features())

    def test_line_intersection_current_report_cannot_omit_the_pair_oracle(self):
        evidence={'execution_id':'current','outputs':[{'port':'line_intersections'}],'line_intersection_expected':m.fixture.line_intersection_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],'schema_version':'addp.topology-browser/v14','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_line_self_intersection_dag_scan_and_lineage_bind_original_source(self):
        tasks=m.definition('line','parent','invalid','snap','reference','overlap','gap','coverage','dangling','line-overlap-source','self-source','intersection-source','self-intersection-source','split-source','component-source','chain-source')['tasks'][:-10]
        self.assertEqual(tasks[-3]['params'],{'locator':'self-intersection-source'})
        self.assertEqual(tasks[-2]['operator'],'check_line_self_intersection')
        self.assertEqual(tasks[-2]['params'],{'input_gdf':{'$ref':'line_self_intersection_input'},'id_field':'route_key'})
        self.assertEqual(tasks[-1]['params']['target_name'],'line-self-intersections.gpkg')
        self.assertEqual(m.output_sources('line_self_intersections'),(11,))
        def lineage(client,source_id,target_id,execution_id,timeout):
            if source_id==21 and target_id==40:raise m.SuiteError('self point lineage missing')
            return {}
        with patch.object(m.raster,'wait_lineage',side_effect=lineage),self.assertRaisesRegex(m.SuiteError,'self point lineage missing'):
            m.run_scenario(Path('.'),self.env,Client(),physical,lambda *args:self.fail('must stop before browser'))
        class WrongScan(Client):
            def request(self,method,path,expected,body=None):
                response=super().request(method,path,expected,body)
                if '/meta/executions/scan-line_self_intersections' in path:response.payload['triggered_by']=44
                return response
        with self.assertRaisesRegex(m.SuiteError,'User or Tenant'):
            m.run_scenario(Path('.'),self.env,WrongScan(),lambda *args:self.fail('wrong scan actor'))
        report=m.run_scenario(Path('.'),self.env,Client(),physical,lambda repo,env,evidence:evidence,diagnostic_runner=lambda *args:{})
        self.assertEqual(report['browser']['line_self_intersection_expected'],m.fixture.line_self_intersection_expected_features())

    def test_line_self_intersection_report_cannot_omit_expected_points(self):
        evidence={'execution_id':'current','outputs':[{'port':'line_self_intersections'}],'line_self_intersection_expected':m.fixture.line_self_intersection_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],'schema_version':'addp.topology-browser/v14','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)


def normalize_execution():
    source=f'addp://engine/3/path/{m.fixture.NORMALIZE_SOURCE}?type=file&item_id=10'
    targets={p:f'addp://engine/3/path/outputs/{n}?type=file' for p,n in zip(m.fixture.NORMALIZE_PORTS,m.fixture.NORMALIZE_TARGETS)}
    facts={'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.normal_input','locator':source}],
           'outputs':[{'locator':t,'write_mode':'create'} for t in targets.values()]}
    return {'status':'success','outputs':{'save_'+p:{'resource':{'locator':t,'type':'file','write_mode':'create'}} for p,t in targets.items()},
        'metadata':{'lineage_facts':facts,'result':{'meta_scan_runs':[{'execution_id':'scan-'+p,'target_locator':t,'status':'submitted'} for p,t in targets.items()]}}}


class NormalizeClient(base.Client):
    def __init__(self):
        super().__init__(m.fixture.NORMALIZE_SOURCE)
        self.permissions=sorted(m.PERMISSIONS);self.scan_tenant=2
        self.targets={20+i:base.item(name,20+i) for i,name in enumerate(m.fixture.NORMALIZE_TARGETS)}
        self.executions={'normalize-execution':normalize_execution(),'normalize-failed':{
            'execution_id':'normalize-failed','status':'failed','metadata':{},
            'error_details':{'code':'develop.workflow.invalid_params','diagnostic':{
                'schema_version':'addp.execution-failure/v3','stage':'execution','reason':'operator_input_invalid',
                'operator':{'node_index':2,'operator_id':'normalize_line_network','parameter':'input_gdf'},
            }},
        }}
    def request(self,method,path,expected,body=None):
        if path.endswith('/auth/context'):
            data={'principal':{'id':33,'type':'user'},'context':{'type':'tenant','tenant_id':'2'},'token':{'type':'first_party_access_token'},
                  'authorization':{'role_assignments':[{'role_key':'topology-consumer','permissions':self.permissions}]}}
        elif path=='/api/v1/develop/executions' and method=='POST':
            self.calls.append((method,path,body))
            tasks=body['content']['workflow_definition']['tasks']
            assert [t['operator'] for t in tasks]==['load','normalize_line_network','save','save']
            data={'execution_id':'normalize-failed' if tasks[1]['params']['id_field']=='missing_id' else 'normalize-execution'}
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':self.scan_tenant,'triggered_by':self.actor}
        elif path.startswith('/api/v1/meta/scan/runs?'):
            ids=['source-scan','scan-result','scan-members'];data={'total':len(ids),'items':[{'execution_id':i} for i in ids]}
        elif '/meta/lineage/graph?' in path:
            identifier=int(path.split('item_id=')[1].split('&')[0]);data={'truncated':False,'edges':[{'source':{'item_id':10},'target':{'item_id':identifier},'evidence':{'execution_id':'normalize-execution'},'status':'active','relation_kind':'derive'}]}
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


def normalize_physical(*args):
    return {'exact_inventory_verified':True,'sources':{m.fixture.NORMALIZE_SOURCE:{'sha256':'source','size_bytes':500}},
        'targets':{name:{'sha256':name,'size_bytes':98304,'layer_name':name[:-5],'geometry_column':'geom','fid_column':'fid','feature_count':count}
                   for name,count in zip(m.fixture.NORMALIZE_TARGETS,(9,17))}}


class NormalizeOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.NORMALIZE_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=normalize_physical,browser=lambda r,e,v:v):
        return m.run_scenario(Path('.'),self.env,client or NormalizeClient(),physical,browser,
            diagnostic_runner=lambda *a,**kw:self.fail('normalize must use the public diagnostic instead of log text'))
    def test_case_uses_only_one_source_two_ports_exact_identity_and_no_old_branch(self):
        client=NormalizeClient()
        with patch.object(m,'definition',side_effect=AssertionError('old full scenario must not run')):
            result=self.run_case(client)
        self.assertEqual(result['schema_version'],'addp.topology-normalize/v1')
        self.assertEqual(result['case'],m.fixture.NORMALIZE_CASE)
        self.assertEqual([o['port'] for o in result['outputs']],['result','members'])
        self.assertEqual([len(result['browser']['expected'][p]) for p in m.fixture.NORMALIZE_PORTS],[9,17])
        manual=[call for call in client.calls if call[1].endswith('/scan/run/manual')]
        self.assertEqual(len(manual),1);self.assertEqual(manual[0][2]['targets'],[f'addp://engine/3/path/{m.fixture.NORMALIZE_SOURCE}?type=file'])
        failure = result['failed_normalize']
        self.assertTrue(failure['no_publication_or_scan'])
        self.assertEqual(failure['diagnostic'], failure['monitor']['error_details'])
        self.assertEqual(failure['diagnostic']['diagnostic']['operator'],
                         {'node_index':2,'operator_id':'normalize_line_network','parameter':'input_gdf'})
        tasks=m.normalize_definition('s','p')['tasks']
        self.assertEqual(tasks[2]['params']['input_df'],{'$ref':'normal'})
        self.assertEqual(tasks[3]['params']['input_df'],{'$ref':'normal','port':'members'})
    def test_case_rejects_wrong_ports_types_scans_identity_permissions_and_size(self):
        source=f'addp://engine/3/path/{m.fixture.NORMALIZE_SOURCE}?type=file&item_id=10'
        targets={p:f'addp://engine/3/path/outputs/{n}?type=file' for p,n in zip(m.fixture.NORMALIZE_PORTS,m.fixture.NORMALIZE_TARGETS)}
        for mutate in (lambda e:e['outputs'].pop('save_members'),lambda e:e['metadata']['lineage_facts']['inputs'][0].update(port='input.foreign'),
            lambda e:e['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),
            lambda e:e['metadata']['result']['meta_scan_runs'][1].update(execution_id='scan-result')):
            value=normalize_execution();mutate(value)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):m.validate_normalize_success(value,source,targets)
        for field,value in [('actor',True),('actor',34),('scan_tenant','2'),('permissions',sorted(m.PERMISSIONS|{'extra.read'}))]:
            client=NormalizeClient();setattr(client,field,value)
            with self.subTest(field=field),self.assertRaises(m.SuiteError):self.run_case(client)
        client=NormalizeClient();client.targets[20]['size_bytes']=0
        with self.assertRaises(m.SuiteError):self.run_case(client)
    def test_normalization_rejects_incorrect_public_diagnostic(self):
        for owner in ('develop', 'monitor'):
            class WrongDiagnostic(NormalizeClient):
                def request(self, method, path, expected, body=None):
                    result = super().request(method, path, expected, body)
                    if f'/api/v1/{owner}/executions/' in path and path.endswith('/normalize-failed'):
                        result.payload['error_details']['diagnostic']['operator']['node_index'] = 1
                    return result

            with self.subTest(owner=owner), self.assertRaisesRegex(m.SuiteError, 'public diagnostic differs'):
                self.run_case(WrongDiagnostic())

    def test_case_failure_and_browser_cannot_change_physical_inventory(self):
        client=NormalizeClient();client.executions['normalize-failed']['outputs']={'unexpected':{}}
        with self.assertRaises(m.SuiteError):self.run_case(client)
        def browser(*args):raise m.SuiteError('map failed')
        with self.assertRaises(m.SuiteError):self.run_case(browser=browser)
        checkpoint=json.loads((Path(self.directory.name)/'topology-normalize-failure.json').read_text())
        self.assertEqual(checkpoint['result'],'incomplete');self.assertEqual(checkpoint['stage'],'field_failure_validated')
        count=0
        def changed(*args):
            nonlocal count
            count+=1;value=normalize_physical()
            if count==3:value['targets'][m.fixture.NORMALIZE_TARGETS[0]]['sha256']='changed'
            return value
        with self.assertRaises(m.SuiteError):self.run_case(physical=changed)
    def test_browser_selects_only_normalize_and_binds_complete_report(self):
        evidence={'case':m.fixture.NORMALIZE_CASE,'run_id':'r','expected':{'result':[],'members':[]}}
        path=Path(self.directory.name)/'topology-browser.json'
        def invoke(args,**kwargs):
            self.assertEqual(args[-2:],['--grep','重合规整独立普通User双成果与字段拒绝$'])
            path.write_text(json.dumps({**evidence,'schema_version':'addp.topology-normalize-browser/v1','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=invoke):m.browser(Path('.'),self.env,evidence)
        def wrong(*args,**kwargs):
            path.write_text(json.dumps({**evidence,'schema_version':'addp.topology-browser/v14','result':'passed'}));return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=wrong),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_actual_normalize_map_assertion_rejects_missing_duplicate_wrong_provenance_and_direction(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r'''
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
const source=readFileSync(process.argv[1],'utf8')
const expected=JSON.parse(process.argv[2])
const {webMercatorPreviewCoordinate}=await import(process.argv[3])
const helperStart=source.indexOf('function normalizedMapExpectation('),helperEnd=source.indexOf('async function verifyNetworkCase(',helperStart)
const normalizedMapExpectation=new Function('webMercatorPreviewCoordinate',source.slice(helperStart,helperEnd)+';return normalizedMapExpectation')(webMercatorPreviewCoordinate)
const start=source.indexOf('    await expect.poll(async()=>{',helperEnd),end=source.indexOf('    let greenPixels',start)
assert.ok(start>0&&end>start)
const check=new (Object.getPrototypeOf(async function(){}).constructor)('renderer','expect','fields','expected','normalizedMapExpectation','let rendered;'+source.slice(start,end))
const expect={poll:fn=>({async toEqual(value){assert.deepEqual(await fn(),value)}})}
async function run(rows,projection='EPSG:3857'){
 const features=rows.map(row=>({get:k=>row.properties[k],getGeometry:()=>({getType:()=>row.geometry.type,getCoordinates:()=>row.geometry.type==='Polygon'?row.geometry.coordinates.map(r=>r.map(webMercatorPreviewCoordinate)):row.geometry.coordinates.map(webMercatorPreviewCoordinate)})}))
 const map={getView:()=>({getAnimating:()=>false,getProjection:()=>({getCode:()=>projection})}),getLayers:()=>({getArray:()=>[{getSource:()=>({getFeatures:()=>features})}]})}
 const renderer={evaluate:async(fn,args)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},args)}
 await check(renderer,expect,Object.keys(expected[0].properties),expected,normalizedMapExpectation)
}
await run(expected);await run([...expected].reverse())
const buffer=expected[0].geometry.type==='Polygon'
const changes=buffer?[rows=>rows.pop(),rows=>rows.push(structuredClone(rows[0])),rows=>rows[0].properties.distance++,rows=>rows[0].properties.route_key=1,
 rows=>rows[1].properties.grade=0,rows=>rows[0].geometry.coordinates[0].reverse(),rows=>rows[0].geometry.coordinates[0][1][0]+=.25]:[rows=>rows.pop(),rows=>rows.push(structuredClone(rows[0])),rows=>rows[0].properties['edge_id' in rows[0].properties?'edge_id':'chain_id']++,
 rows=>rows[0].geometry.coordinates.reverse(),rows=>rows[0].geometry.coordinates[0][0]+=1]
if(buffer){}
else if('source_id' in expected[0].properties)changes.push(rows=>rows[0].properties.source_id='wrong',
 rows=>rows[0].properties.source_segment_no++,rows=>rows[0].properties.is_reversed=0,rows=>rows[5].properties.grade=0)
else changes.push(rows=>rows[0].properties['occurrence_count' in rows[0].properties?'occurrence_count':'member_count']++)
for(const change of changes){const rows=structuredClone(expected);change(rows);await assert.rejects(run(rows),{code:'ERR_ASSERTION'})}
await assert.rejects(run(expected,'EPSG:4326'),{code:'ERR_ASSERTION'})
'''
        for case,port in [(case,port) for case in (m.fixture.BUFFER_CASE,m.fixture.NORMALIZE_CASE,m.fixture.COMPOSITION_CASE) for port in m.fixture.network_profile(case)['ports']]:
            result=subprocess.run(['node','--input-type=module','-e',script,
                str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),json.dumps(m.fixture.network_features(case,port)),
                (repository/'manager/frontend/e2e/geometry-preview-coordinate.js').as_uri()],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)


    def test_real_playwright_discovery_selects_exactly_one_case_without_running_business(self):
        import os
        repository=Path(__file__).resolve().parents[2]
        run=m.subprocess.run
        titles={m.fixture.VERTICES_CASE:m.fixture.VERTICES_TITLE,m.fixture.RINGS_DIAGNOSTIC_CASE:m.fixture.RINGS_DIAGNOSTIC_TITLE,m.fixture.RINGS_CASE:m.fixture.RINGS_TITLE,m.fixture.ENDPOINT_CASE:m.fixture.ENDPOINT_TITLE,m.fixture.EXTENSION_CASE:m.fixture.EXTENSION_TITLE,m.fixture.PROXIMITY_CASE:m.fixture.PROXIMITY_TITLE,m.fixture.DIAGNOSTIC_CASE:m.fixture.DIAGNOSTIC_TITLE,m.fixture.BUFFER_CASE:'多距缓冲独立普通User单成果与重名拒绝',m.fixture.NORMALIZE_CASE:'重合规整独立普通User双成果与字段拒绝',
            m.fixture.COMPOSITION_CASE:'重合规整链路合并独立普通User三成果与字段拒绝',
            'full':'同一普通User核对构面、修复、严格吸附、面叠面缝悬点线叠自叠线交自交及打断连通分组链路合并地图及塌缩失败'}
        for case,title in titles.items():
            with self.subTest(case=case):
                environment={**os.environ,**self.env,'ADDP_ONLINE_TOPOLOGY_CASE':case}
                def discover(args,**kwargs):
                    # Keep the real owner invocation/config/file/filter; list only.
                    result=run([*args,'--list','--reporter=json'],cwd=kwargs['cwd'],env=kwargs['env'],
                        text=True,capture_output=True,timeout=30)
                    self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                    listing=json.loads(result.stdout)
                    def specs(suites):
                        return [spec for suite in suites for spec in [*suite.get('specs',[]),*specs(suite.get('suites',[]))]]
                    selected=specs(listing['suites'])
                    self.assertEqual([s['title'] for s in selected],[title])
                    self.assertEqual(sum(len(s['tests']) for s in selected),1)
                    self.assertEqual(listing['errors'],[])
                    return result
                with patch.object(m.subprocess,'run',side_effect=discover),self.assertRaisesRegex(m.SuiteError,'evidence missing'):
                    m.browser(repository,environment,{'case':case})
                self.assertFalse((Path(self.directory.name)/'topology-browser.json').exists())


class CompositionClient(NormalizeClient):
    def __init__(self):
        super().__init__();self.source_name=m.fixture.COMPOSITION_SOURCE;self.source['full_name']=self.source_name
        self.targets={20+i:base.item(n,20+i) for i,n in enumerate(m.fixture.COMPOSITION_TARGETS)}
        original=normalize_execution();source=f'addp://engine/3/path/{self.source_name}?type=file&item_id=10'
        targets={p:f'addp://engine/3/path/outputs/{n}?type=file' for p,n in zip(m.fixture.COMPOSITION_PORTS,m.fixture.COMPOSITION_TARGETS)}
        original['outputs']={'save_'+p:{'resource':{'locator':t,'type':'file','write_mode':'create'}} for p,t in targets.items()}
        original['metadata']['lineage_facts']={'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.normal_input','locator':source}],
            'outputs':[{'locator':t,'write_mode':'create'} for t in targets.values()]}
        original['metadata']['result']['meta_scan_runs']=[{'execution_id':'scan-'+p,'target_locator':t,'status':'submitted'} for p,t in targets.items()]
        self.executions['normalize-execution']=original
        self.executions['normalize-failed']['error_details']['diagnostic']['operator'] = {
            'node_index':3, 'operator_id':'merge_line_chains', 'parameter':'input_gdf',
        }
    def request(self,method,path,expected,body=None):
        if path=='/api/v1/develop/executions' and method=='POST':
            self.calls.append((method,path,body));tasks=body['content']['workflow_definition']['tasks']
            assert [t['operator'] for t in tasks]==['load','normalize_line_network','merge_line_chains','save','save','save']
            assert tasks[2]['params']['match_fields']==[] and all('chains' in t['depends_on'] and t['params']['input_df']['$ref'] in t['depends_on'] for t in tasks[3:])
            return SimpleNamespace(payload={'execution_id':'normalize-failed' if tasks[2]['params']['id_field']=='missing_id' else 'normalize-execution'})
        if path.startswith('/api/v1/meta/scan/runs?'):
            ids=['source-scan',*['scan-'+p for p in m.fixture.COMPOSITION_PORTS]]
            return SimpleNamespace(payload={'total':len(ids),'items':[{'execution_id':i} for i in ids]})
        return super().request(method,path,expected,body)


def composition_physical(*args):
    return {'exact_inventory_verified':True,'sources':{m.fixture.COMPOSITION_SOURCE:{'sha256':'source','size_bytes':500}},
        'targets':{name:{'sha256':name,'size_bytes':98304,'layer_name':name[:-5],'geometry_column':'network_shape','fid_column':'fid','feature_count':count}
                   for name,count in zip(m.fixture.COMPOSITION_TARGETS,(9,15,22))}}


class CompositionOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.COMPOSITION_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=composition_physical,browser=lambda r,e,v:v):
        return m.run_scenario(Path('.'),self.env,client or CompositionClient(),physical,browser,
            diagnostic_runner=lambda *a,**kw:self.fail('chains must use the public diagnostic instead of log text'))
    def test_composition_exact_three_outputs_and_failure_barrier_without_old_scenarios(self):
        with patch.object(m,'definition',side_effect=AssertionError('old full must not run')):
            report=self.run_case()
        self.assertEqual(report['schema_version'],'addp.topology-normalize-chains/v1')
        self.assertEqual([o['port'] for o in report['outputs']],list(m.fixture.COMPOSITION_PORTS))
        self.assertEqual([len(report['browser']['expected'][p]) for p in m.fixture.COMPOSITION_PORTS],[9,15,22])
        self.assertEqual(len(report['automatic_scan_observations']),3)
        failure = report['failed_normalize']
        self.assertTrue(failure['no_publication_or_scan'])
        self.assertEqual(failure['diagnostic'], failure['monitor']['error_details'])
        self.assertEqual(failure['diagnostic']['diagnostic']['operator'],
                         {'node_index':3,'operator_id':'merge_line_chains','parameter':'input_gdf'})
        tasks=m.normalize_definition('s','p',case=m.fixture.COMPOSITION_CASE)['tasks']
        self.assertEqual([t['params']['input_df'] for t in tasks[3:]],[{'$ref':'chains'},{'$ref':'chains','port':'members'},{'$ref':'normal','port':'members'}])
    def test_composition_rejects_missing_duplicate_wrong_binding_facts_scans_and_sizes(self):
        original=CompositionClient().executions['normalize-execution'];source=original['metadata']['lineage_facts']['inputs'][0]['locator']
        targets={p:v['resource']['locator'] for p,v in ((key[5:],v) for key,v in original['outputs'].items())}
        for mutate in [lambda e:e['outputs'].pop('save_chain_edges'),lambda e:e['outputs']['save_chains'].update(resource={'locator':targets['edge_sources'],'type':'file','write_mode':'create'}),
            lambda e:e['metadata']['lineage_facts']['outputs'].pop(),lambda e:e['metadata']['lineage_facts']['inputs'][0].update(port='input.chains'),
            lambda e:e['metadata']['result']['meta_scan_runs'][2].update(execution_id='scan-chains'),
            lambda e:e['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail')]:
            value=copy.deepcopy(original);mutate(value)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):m.validate_normalize_success(value,source,targets,m.fixture.COMPOSITION_CASE)
        for field,value in [('actor',True),('actor',34),('scan_tenant','2'),('permissions',sorted(m.PERMISSIONS|{'extra.read'}))]:
            client=CompositionClient();setattr(client,field,value)
            with self.subTest(field=field),self.assertRaises(m.SuiteError):self.run_case(client)
        for i in range(3):
            client=CompositionClient();client.targets[20+i]['size_bytes']=0
            with self.subTest(i=i),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_composition_browser_failure_keeps_checkpoint_not_passed(self):
        def broken(*args):raise m.SuiteError('map failed')
        with self.assertRaises(m.SuiteError):self.run_case(browser=broken)
        checkpoint=json.loads((Path(self.directory.name)/'topology-normalize-failure.json').read_text())
        self.assertEqual(checkpoint['case'],m.fixture.COMPOSITION_CASE);self.assertEqual(checkpoint['result'],'incomplete')
        self.assertEqual(checkpoint['schema_version'],'addp.topology-normalize-chains-checkpoint/v1')
    def test_composition_browser_report_cannot_be_old_case_or_omit_third_output(self):
        evidence={'case':m.fixture.COMPOSITION_CASE,'run_id':'r','outputs':[{'port':p} for p in m.fixture.COMPOSITION_PORTS]}
        path=Path(self.directory.name)/'topology-browser.json'
        for wrong in (False,True):
            def invoke(args,**kwargs):
                self.assertEqual(args[-2:],['--grep','重合规整链路合并独立普通User三成果与字段拒绝$'])
                report={**evidence,'schema_version':'addp.topology-normalize-chains-browser/v1','result':'passed'}
                if wrong:report['outputs']=report['outputs'][:2]
                path.write_text(json.dumps(report));return SimpleNamespace(returncode=0)
            with patch.object(m.subprocess,'run',side_effect=invoke):
                if wrong:
                    with self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)
                else:m.browser(Path('.'),self.env,evidence)


class BufferClient(NormalizeClient):
    def __init__(self):
        super().__init__();self.source_name=m.fixture.BUFFER_SOURCES[0];self.source['full_name']=self.source_name
        self.conflict={**self.source,'id':11,'full_name':m.fixture.BUFFER_SOURCES[1],'fingerprint':'conflict'}
        self.targets={20:base.item('buffers.gpkg',20)}
        value=normalize_execution();target='addp://engine/3/path/outputs/buffers.gpkg?type=file'
        value['outputs']={'save_result':{'resource':{'locator':target,'type':'file','write_mode':'create'}}}
        value['metadata']['lineage_facts']['inputs']=[{'port':'input.normal_input','locator':f'addp://engine/3/path/{self.source_name}?type=file&item_id=10'}]
        value['metadata']['lineage_facts']['outputs']=[{'locator':target,'write_mode':'create'}]
        value['metadata']['result']['meta_scan_runs']=[{'execution_id':'scan-result','target_locator':target,'status':'submitted'}]
        self.executions['normalize-execution']=value
    def request(self,method,path,expected,body=None):
        if path.endswith('/scan/run/manual'):
            self.calls.append((method,path,body));name=body['targets'][0].split('/path/')[1].split('?')[0]
            assert name in m.fixture.BUFFER_SOURCES
            data={'execution_id':'source-scan' if name==self.source_name else 'conflict-scan'}
        elif path.endswith('/engines/3/items'):data=[self.source,self.conflict,*self.targets.values()]
        elif path=='/api/v1/meta/items/11':data=self.conflict
        elif path.startswith('/api/v1/meta/scan/runs?'):
            data={'total':3,'items':[{'execution_id':x} for x in ['source-scan','conflict-scan','scan-result']]}
        elif path=='/api/v1/develop/executions' and method=='POST':
            self.calls.append((method,path,body));tasks=body['content']['workflow_definition']['tasks']
            assert [t['operator'] for t in tasks]==['load','batch_buffer','save']
            assert tasks[1]['params']['distances']==[2,1,2] and tasks[1]['params']['resolution']==1
            data={'execution_id':'normalize-failed' if m.fixture.BUFFER_SOURCES[1] in tasks[0]['params']['locator'] else 'normalize-execution'}
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


def buffer_physical(*args):
    return {'exact_inventory_verified':True,'sources':{name:{'sha256':name,'size_bytes':500} for name in m.fixture.BUFFER_SOURCES},
            'targets':{'buffers.gpkg':{'sha256':'buffers','size_bytes':98304,'layer_name':'buffers','geometry_column':'buffer_shape','fid_column':'fid','feature_count':6}}}


class BufferOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.BUFFER_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=buffer_physical,browser=lambda r,e,v:v):
        return m.run_scenario(Path('.'),self.env,client or BufferClient(),physical,browser,
            diagnostic_runner=lambda *args,**kw:{'cause':kw['cause'],'reason':kw['reason']})
    def test_buffer_uses_only_selected_sources_one_output_and_exact_conflict_diagnostic(self):
        client=BufferClient()
        with patch.object(m,'definition',side_effect=AssertionError('old full must not run')):
            value=self.run_case(client)
        self.assertEqual(value['schema_version'],'addp.topology-buffer/v1')
        self.assertEqual(value['failed_normalize']['diagnostic']['cause'],'distance_conflict')
        self.assertIn('distance 与多距缓冲输出列冲突',value['failed_normalize']['diagnostic']['reason'])
        self.assertEqual(len(value['outputs']),1);self.assertEqual(len(value['browser']['expected']['result']),6)
        manual=[c[2]['targets'][0] for c in client.calls if c[1].endswith('/scan/run/manual')]
        self.assertEqual(manual,[f'addp://engine/3/path/{n}?type=file' for n in m.fixture.BUFFER_SOURCES])
        self.assertEqual(value['conflict_source']['id'],11)
    def test_buffer_rejects_wrong_public_identity_input_facts_scans_and_published_failure(self):
        for attr,value in [('actor',True),('actor',34),('scan_tenant','2'),('permissions',sorted(m.PERMISSIONS|{'extra.read'}))]:
            client=BufferClient();setattr(client,attr,value)
            with self.subTest(attr=attr),self.assertRaises(m.SuiteError):self.run_case(client)
        for mutate in [lambda c:c.executions['normalize-execution']['metadata']['lineage_facts']['inputs'][0].update(locator='conflict'),
                       lambda c:c.executions['normalize-execution']['metadata']['result']['meta_scan_runs'].clear(),
                       lambda c:c.executions['normalize-failed'].update(outputs={'bad':{}}),
                       lambda c:c.targets[20].update(size_bytes=0)]:
            client=BufferClient();mutate(client)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_buffer_rejects_changed_source_and_failed_browser_preserves_checkpoint(self):
        def changed(repo,env,evidence):
            raise m.SuiteError('map failed')
        with self.assertRaises(m.SuiteError):self.run_case(browser=changed)
        evidence=json.loads((Path(self.directory.name)/'topology-normalize-failure.json').read_text())
        self.assertEqual(evidence['result'],'incomplete');self.assertEqual(evidence['conflict_source']['id'],11)
        count=0
        def physical(*args):
            nonlocal count
            count+=1;value=buffer_physical()
            if count==3:value['sources'][m.fixture.BUFFER_SOURCES[1]]['sha256']='mutated'
            return value
        with self.assertRaises(m.SuiteError):self.run_case(physical=physical)


class InputDiagnosticClient:
    def __init__(self,case=None):
        case=case or m.fixture.DIAGNOSTIC_CASE;self.specs=m.input_diagnostic_specs(case);names=m.fixture.network_profile(case)['diagnostic_sources']
        self.calls=[];self.executions={};self.scans=['scan-'+name for name in names]
        self.sources=[{'id':10+i,'full_name':name,'size_bytes':98304,'item_type':'file','fingerprint':name} for i,name in enumerate(names)]
        self.mutate=None
    def request(self,method,path,expected,body=None):
        self.calls.append((method,path,body))
        if path.endswith('/auth/context'):
            data={'principal':{'id':33,'type':'user'},'context':{'type':'tenant','tenant_id':'2'},'token':{'type':'first_party_access_token'},
                  'authorization':{'role_assignments':[{'role_key':'topology-consumer','permissions':sorted(m.PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            data={'execution_id':'scan-'+body['targets'][0].split('/path/')[1].split('?')[0]}
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':2,'triggered_by':33}
        elif path.endswith('/engines/3/items'):data=self.sources
        elif '/meta/scan/runs?' in path:data={'total':len(self.scans),'items':[{'execution_id':value} for value in self.scans]}
        elif '/meta/items/' in path:data=self.sources[int(path.rsplit('/',1)[1])-10]
        elif path.endswith('/workflow-engines'):data=[{'id':7,'engine_type':'geopython_workflow','connection_status':'online'}]
        elif method=='POST' and path.endswith('/develop/executions'):
            spec=self.specs[len(self.executions)];identifier='diagnostic-'+spec[0]
            data={'execution_id':identifier}
            self.executions[identifier]={'execution_id':identifier,'triggered_by':33,'status':'failed','outputs':{},'metadata':{},
                'error_details':{'code':'develop.workflow.invalid_params','category':'execution_failed','message':'controlled', 'diagnostic':m.expected_input_diagnostic(spec)},'execution_config':{'content':{'workflow_definition':copy.deepcopy(body['content']['workflow_definition'])}}}
            if self.mutate:self.mutate(self.executions[identifier])
        elif '/develop/executions/' in path:data=self.executions[path.rsplit('/',1)[1]]
        elif '/monitor/executions/by-execution-id/' in path:data={**self.executions[path.rsplit('/',1)[1]],'module':'develop'}
        elif '/meta/lineage/graph?' in path:data={'truncated':False,'nodes':[],'edges':[]}
        else:raise AssertionError((method,path,body))
        return SimpleNamespace(payload=copy.deepcopy(data))


def diagnostic_physical(*args):
    return {'sources':{name:{'size_bytes':98304,'sha256':name} for name in m.fixture.DIAGNOSTIC_SOURCES},'targets':{},'exact_inventory_verified':True}


class InputDiagnosticOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.DIAGNOSTIC_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=diagnostic_physical,browser=lambda repo,env,evidence:evidence):
        return m.run_scenario(Path('.'),self.env,client or InputDiagnosticClient(),physical,browser,diagnostic_runner=lambda *a:self.fail('no private log oracle'))
    def test_diagnostic_case_four_real_dags_keep_original_node_position_and_never_old_business(self):
        client=InputDiagnosticClient();report=self.run_case(client)
        self.assertEqual(report['schema_version'],'addp.operator-input-diagnostics/v1')
        self.assertEqual(report['physical']['targets'],{})
        self.assertEqual(len(report['executions']),4)
        for spec,run in zip(m.DIAGNOSTIC_SPECS,report['executions']):
            tasks=run['definition']['tasks']
            self.assertEqual(tasks[0]['operator'],spec[1]);self.assertEqual(tasks[1]['operator'],'load')
            self.assertEqual(tasks[0]['depends_on'][0],'source');self.assertEqual(tasks[-1]['operator'],'save')
            self.assertEqual(run['execution']['error_details']['diagnostic']['operator']['node_index'],1)
        self.assertIs(report['executions'][1]['definition']['tasks'][0]['params']['distances'][0],True)
        self.assertEqual(report['initial_scan_inventory'],report['final_scan_inventory'])
        self.assertEqual(sum(method=='POST' and path.endswith('/develop/executions') for method,path,_ in client.calls),4)
        self.assertFalse(any('/manager/preview' in path for _,path,_ in client.calls))
    def test_diagnostic_rejects_wrong_tuple_private_context_and_any_publication(self):
        for mutate in [lambda e:e.update(status='success'),lambda e:e.update(execution_id='foreign'),lambda e:e.update(triggered_by=True),lambda e:e.update(triggered_by=99),lambda e:e.update(outputs={'save':{}}),
            lambda e:e['error_details'].update(code='develop.workflow.execution_failed'),
            lambda e:e['error_details']['diagnostic']['operator'].update(node_index=True),
            lambda e:e['error_details']['diagnostic']['operator'].update(node_index=2),
            lambda e:e['error_details']['diagnostic']['operator'].update(operator_id='distance_to'),
            lambda e:e['error_details']['diagnostic']['operator'].update(field='PRIVATE_DIAGNOSTIC_SOURCE_VALUE'),
            lambda e:e['error_details'].update(message='Traceback /tmp/private'),
            lambda e:e['metadata'].update(lineage_facts={'outputs':[{'locator':'bad'}]}),
            lambda e:e['metadata'].update(result={'meta_scan_runs':[{'execution_id':'bad'}]})]:
            client=InputDiagnosticClient();client.mutate=mutate
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_diagnostic_failure_keeps_incomplete_checkpoint_and_browser_cannot_change_source(self):
        with self.assertRaisesRegex(m.SuiteError,'browser failed'):
            self.run_case(browser=lambda *a:(_ for _ in ()).throw(m.SuiteError('browser failed')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-input-diagnostics-checkpoint.json').read_text())
        self.assertEqual(checkpoint['result'],'incomplete');self.assertEqual(len(checkpoint['executions']),4)
        count=0
        def changed(*args):
            nonlocal count
            count+=1;value=diagnostic_physical()
            if count==6:value['sources'][m.fixture.DIAGNOSTIC_SOURCES[0]]['sha256']='changed'
            return value
        with self.assertRaisesRegex(m.SuiteError,'browser changed'):self.run_case(physical=changed)
    def test_diagnostic_browser_requires_exact_sixteen_uuid_locale_owner_bindings(self):
        evidence=self.run_case()['browser'];output=Path(self.directory.name)/'topology-browser.json'
        observations=[{'key':run['key'],'execution_id':run['execution_id'],'owner':owner,'locale':locale,'raw':run['execution'] if owner=='develop' else {'execution':run['execution']},
                      'clipboard':'safe'} for run in evidence['executions'] for owner in ('develop','monitor') for locale in ('zh-cn','en')]
        report={**evidence,'schema_version':m.fixture.DIAGNOSTIC_SCHEMA+'-browser/v1','result':'passed','observations':observations}
        def invoke(*args,**kwargs):output.write_text(json.dumps(report));return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=invoke):
            m.browser(Path('.'),self.env,evidence)
            for mutate in [lambda r:r['observations'].pop(),lambda r:r['observations'][0].update(locale='en'),
                           lambda r:r['observations'][0].update(execution_id='old'),lambda r:r['observations'][0].update(clipboard='Traceback')]:
                saved=copy.deepcopy(report);mutate(report)
                with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)
                report=saved

    def test_diagnostic_browser_literals_match_current_shared_contract_without_running_old_business(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        program=r'''
const fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(process.argv[1],'utf8');
const start=source.indexOf('const inputDiagnosticText ='),end=source.indexOf('async function assertInputDiagnosticPanel(',start);
const sandbox={};vm.createContext(sandbox);vm.runInContext(source.slice(start,end)+';this.present=inputDiagnosticPageExpectation;',sandbox);
for(const locale of ['zh-cn','en']){
 const text=JSON.parse(fs.readFileSync(process.argv[2]+'/'+locale+'.json','utf8')).common.executionFailure;
 for(const [key,reason,op,param,field] of [
 ['a-id','operator_input_invalid','intersection','input_gdf',''],
 ['b-id','operator_reference_invalid','intersection','gdf_b',''],
 ['crs','operator_crs_invalid','intersection','',''],
 ['fields','operator_overlay_fields_conflict','intersection','',''],
 ['ring-field-conflict','operator_field_conflict','extract_polygon_rings','input_gdf','ring_id'],
 ['ring-geometry-invalid','operator_geometry_invalid','extract_polygon_rings','input_gdf',''],
 ['field-conflict','operator_field_conflict','batch_buffer','input_gdf','distance'],
 ['number-array','operator_number_array_invalid','batch_buffer','distances',''],
 ['crs-mismatch','operator_crs_mismatch','distance_to','',''],
 ['positive-number','operator_positive_number_required','check_line_proximity','tolerance','']]){
 const reasonText=text.reasons[reason].replace('{field}',field),action=text.actions[reason].replace('{field}',field);
 const values=[text.stages.execution,text.operatorNames[op],text.operatorNodeIndex.replace('{index}','1'),param?text.operatorParameters[param]:'',field,action,'develop.workflow.invalid_params','run-uuid'].filter(Boolean);
 const actual=sandbox.present(key,locale,'run-uuid');
 if(actual.reason!==reasonText || JSON.stringify(actual.values)!==JSON.stringify(values) || actual.clipboard!==[reasonText,...values].join('\n'))throw Error('literal UI/clipboard contract differs '+locale+' '+key);
 }
}
'''
        result=subprocess.run(['node','-e',program,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),
                               str(repository/'common-frontend/basic/src/i18n')],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)


def proximity_physical(*args):
    return {'exact_inventory_verified':True,'sources':{name:{'size_bytes':98304,'sha256':name} for name in m.fixture.PROXIMITY_SOURCES},
            'targets':{m.fixture.PROXIMITY_TARGET:{'size_bytes':98304,'sha256':'output','feature_count':7,'layer_name':'proximity-points','geometry_column':'geom','fid_column':'fid','features':m.fixture.proximity_features('result')}}}


class ProximityClient(InputDiagnosticClient):
    def __init__(self):
        super().__init__()
        self.sources=[]
        for i,(name,count) in enumerate(zip(m.fixture.PROXIMITY_SOURCES,[5,9])):
            item=base.item(name,10+i);item.update(full_name=name,size_bytes=98304)
            item['attributes']['type_info']['container']['children'][0].update(row_count=count,column_count=4)
            self.sources.append(item)
        self.output=base.item(m.fixture.PROXIMITY_TARGET,20);self.output['size_bytes']=98304
        self.output['attributes']['type_info']['container']['children'][0].update(row_count=7,column_count=7)
        self.scans=['scan-'+name for name in m.fixture.PROXIMITY_SOURCES]
        locators=[f'addp://engine/3/path/{name}?type=file&item_id={10+i}' for i,name in enumerate(m.fixture.PROXIMITY_SOURCES)]
        target='addp://engine/3/path/outputs/'+m.fixture.PROXIMITY_TARGET+'?type=file'
        facts={'schema_version':'addp.lineage-facts/v1','inputs':[{'port':port,'locator':locator} for port,locator in zip(['input.load_source','input.load_reference'],locators)],'outputs':[{'locator':target,'write_mode':'create'}]}
        self.success={'execution_id':'proximity-run','status':'success','triggered_by':33,'outputs':{'save_result':{'resource':{'locator':target,'type':'file','write_mode':'create'}}},
            'metadata':{'lineage_facts':facts,'result':{'meta_scan_runs':[{'execution_id':'output-scan','target_locator':target,'status':'submitted'}]}},
            'execution_config':{'content':{'workflow_definition':m.proximity_definition(locators,'addp://engine/3/path/outputs?type=directory')}}}
        self.scan_tenant=2;self.scan_actor=33
        self.graph={'truncated':False,'nodes':[],'edges':[{'source':{'item_id':source},'target':{'item_id':20},'relation_kind':'derive','status':'active','evidence':{'execution_id':'proximity-run'}} for source in [10,11]]}
    def request(self,method,path,expected,body=None):
        if method=='POST' and path.endswith('/develop/executions'):
            self.calls.append((method,path,body));self.executions['proximity-run']=self.success;self.scans.append('output-scan');data={'execution_id':'proximity-run'}
        elif path.endswith('/engines/3/items'):data=[*self.sources,self.output]
        elif path=='/api/v1/meta/items/20':data=self.output
        elif path.startswith('/api/v1/meta/lineage/graph?'):data=self.graph
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':self.scan_tenant,'triggered_by':self.scan_actor}
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


class ProximityOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.PROXIMITY_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=proximity_physical,browser=lambda repo,env,evidence:evidence):
        return m.run_scenario(Path('.'),self.env,client or ProximityClient(),physical,browser,diagnostic_runner=lambda *a:self.fail('no old failure scenario'))
    def test_proximity_only_one_success_execution_two_sources_one_scan_and_exact_current_edges(self):
        client=ProximityClient();report=self.run_case(client)
        self.assertEqual(report['schema_version'],'addp.line-proximity/v1');self.assertEqual(report['result'],'passed')
        self.assertEqual(len(report['browser']['expected']),7);self.assertEqual(len(report['raw_lineage']['edges']),2)
        self.assertEqual(set(report['meta_after']),{*m.fixture.PROXIMITY_SOURCES,m.fixture.PROXIMITY_TARGET})
        self.assertEqual(report['final_scan_inventory'],sorted([*report['initial_scan_inventory'],'output-scan']))
        self.assertEqual(sum(method=='POST' and path.endswith('/develop/executions') for method,path,_ in client.calls),1)
        self.assertEqual(report['definition']['tasks'][2]['params']['reference_id_field'],'ref_key')
    def test_proximity_read_ports_are_order_independent_but_wrong_binding_or_extra_publication_rejected(self):
        client=ProximityClient();client.success['metadata']['lineage_facts']['inputs'].reverse();self.run_case(client)
        mutations=[lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(locator='wrong'),
            lambda c:c.success['metadata']['lineage_facts']['inputs'][1].update(port='input.load_source'),
            lambda c:c.success['metadata']['lineage_facts']['inputs'].append(copy.deepcopy(c.success['metadata']['lineage_facts']['inputs'][0])),
            lambda c:c.success['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),
            lambda c:c.success['outputs'].update(extra={}),lambda c:c.success['metadata']['result']['meta_scan_runs'].append({'execution_id':'extra'}),
            lambda c:c.success.update(triggered_by=True),lambda c:c.success.update(execution_config={'content':{'workflow_definition':{}}}),
            lambda c:c.output.update(size_bytes=0),lambda c:c.sources[0].update(size_bytes='98304'),
            lambda c:c.output['attributes']['type_info']['container']['children'][0].update(row_count=3),
            lambda c:setattr(c,'scan_tenant','2'),lambda c:setattr(c,'scan_actor',99)]
        for mutate in mutations:
            client=ProximityClient();mutate(client)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_proximity_rejects_missing_duplicate_extra_or_wrong_current_lineage(self):
        for mutate in [lambda g:g['edges'].pop(),lambda g:g['edges'].append(copy.deepcopy(g['edges'][0])),
            lambda g:g['edges'][0]['source'].update(item_id=99),lambda g:g['edges'][1]['evidence'].update(execution_id='old'),
            lambda g:g['edges'][0].update(status='inactive'),lambda g:g.update(truncated=True)]:
            client=ProximityClient();mutate(client.graph)
            # The waiter would otherwise wait its real timeout for a deliberately absent edge.
            with patch.object(m.raster,'wait_lineage',return_value={}):
                with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_proximity_browser_failure_keeps_incomplete_and_cannot_relabel_partial_evidence(self):
        with self.assertRaisesRegex(m.SuiteError,'Point failed'):
            self.run_case(browser=lambda *a:(_ for _ in ()).throw(m.SuiteError('Point failed')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-proximity-checkpoint.json').read_text())
        self.assertEqual(checkpoint['result'],'incomplete');self.assertEqual(checkpoint['stage'],'before_lineage')
        def changed(repo,env,evidence):
            client.sources[0]['size_bytes']=999;return evidence
        client=ProximityClient()
        with self.assertRaisesRegex(m.SuiteError,'source Meta changed'):self.run_case(client,browser=changed)
        calls=0
        def physical(*a):
            nonlocal calls
            calls+=1;value=proximity_physical()
            if calls==2:value['targets'][m.fixture.PROXIMITY_TARGET]['sha256']='changed'
            return value
        with self.assertRaisesRegex(m.SuiteError,'physical files'):self.run_case(physical=physical)

    def test_proximity_actual_point_map_assertion_rejects_dedup_wrong_position_type_id_and_pixels(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r'''
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
const source=readFileSync(process.argv[1],'utf8'),expected=JSON.parse(process.argv[2]);
const {webMercatorPreviewCoordinate}=await import(process.argv[3]);
const start=source.indexOf('function proximityPointExpectation('),end=source.indexOf('function assertProximityLineage(',start);
const expect=value=>({toHaveLength:n=>assert.equal(value.length,n),toBe:n=>assert.equal(value,n)});
expect.poll=fn=>({async toEqual(value){assert.deepEqual(await fn(),value)},async toBeGreaterThan(value){assert.ok(await fn()>value)}});
const check=new Function('webMercatorPreviewCoordinate','expect',source.slice(start,end)+';return assertProximityPointMap')(webMercatorPreviewCoordinate,expect);
globalThis.OffscreenCanvas=class {getContext(){return {clearRect(){},fillRect(){},fillStyle:'#000000',getImageData(){return {data:[...this.fillStyle.slice(1).match(/../g).map(value=>parseInt(value,16)),255]}}}}};
async function run(rows,projection='EPSG:3857',pixels=40,color='#409eff',rgba=[64,158,255,255],visible=true){
 const features=rows.map(row=>({get:k=>row.properties[k],getGeometry:()=>({getType:()=>row.geometry.type,getCoordinates:()=>webMercatorPreviewCoordinate(row.geometry.coordinates)})}));
 const context={canvas:{width:pixels,height:1},getImageData:()=>({data:new Uint8ClampedArray(Array.from({length:pixels},()=>rgba).flat())})};
 const layer={getSource:()=>({getFeatures:()=>features}),getVisible:()=>visible,getOpacity:()=>1,getRenderer:()=>({context}),getStyleFunction:()=>()=>({getImage:()=>({getFill:()=>({getColor:()=>color})})})};
 const map={getView:()=>({getAnimating:()=>false,getResolution:()=>1,getProjection:()=>({getCode:()=>projection})}),getLayers:()=>({getArray:()=>[layer]})};
 const canvas={width:pixels,height:1,getContext:()=>context};
 const renderer={evaluate:async(fn,fields)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},fields),locator:()=>({evaluateAll:async fn=>fn([canvas])})};
 return check(renderer,expected);
}
assert.equal((await run(expected)).painted_pixels,40);await run([...expected].reverse());
assert.equal((await run(expected,'EPSG:3857',40,'#67c23a',[103,194,58,255])).painted_pixels,40);
for(const mutate of [r=>r.pop(),r=>r.push(structuredClone(r[0])),r=>r[0].properties.source_id=1,r=>r[0].properties.reference_id='other',
 r=>r[0].properties.distance=1,r=>r[2].properties.nearest_x=.25,r=>r[0].geometry.coordinates=[.5,0],r=>r[0].geometry.type='LineString']){
 const rows=structuredClone(expected);mutate(rows);await assert.rejects(run(rows),{code:'ERR_ASSERTION'});
}
await assert.rejects(run(expected,'EPSG:4326'),{code:'ERR_ASSERTION'});await assert.rejects(run(expected,'EPSG:3857',0),{code:'ERR_ASSERTION'});
await assert.rejects(run(expected,'EPSG:3857',40,'#409eff',[103,194,58,255]),{code:'ERR_ASSERTION'});
await assert.rejects(run(expected,'EPSG:3857',40,'#409eff',[64,158,255,0]),{code:'ERR_ASSERTION'});
await assert.rejects(run(expected,'EPSG:3857',40,'#409eff',[64,158,255,255],false),{code:'ERR_ASSERTION'});
'''
        result=subprocess.run(['node','--input-type=module','-e',script,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),
            json.dumps(m.fixture.proximity_features('result')),(repository/'manager/frontend/e2e/geometry-preview-coordinate.js').as_uri()],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)

    def test_proximity_browser_stage_evidence_cannot_mistake_partial_for_passed(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r'''
import assert from 'node:assert/strict'
import {readFileSync,writeFileSync} from 'node:fs'
import {resolve} from 'node:path'
const source=readFileSync(process.argv[1],'utf8');
const start=source.indexOf('function persistProximityEvidence('),end=source.indexOf("test('近接检查独立普通User双源Point成果'",start);
const persist=new Function('writeFileSync','resolve',source.slice(start,end)+';return persistProximityEvidence')(writeFileSync,resolve);
const raw={case:'line-proximity',run_id:'run',execution_id:'same-uuid',result:'passed'};
persist(process.argv[2],raw,'preview_api_verified');
let evidence=JSON.parse(readFileSync(resolve(process.argv[2],'topology-proximity-browser-raw.json'),'utf8'));
assert.equal(evidence.result,'incomplete');assert.equal(evidence.stage,'preview_api_verified');assert.equal(evidence.execution_id,'same-uuid');
persist(process.argv[2],raw,'complete','passed');
evidence=JSON.parse(readFileSync(resolve(process.argv[2],'topology-proximity-browser-raw.json'),'utf8'));
assert.equal(evidence.result,'passed');assert.equal(evidence.stage,'complete');
'''
        result=subprocess.run(['node','--input-type=module','-e',script,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),self.directory.name],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)


def extension_physical(*args):
    return {'exact_inventory_verified':True,'sources':{n:{'size_bytes':98304,'sha256':n} for n in m.fixture.EXTENSION_SOURCES},
        'targets':{name:{'size_bytes':98304,'sha256':name,'feature_count':len(m.fixture.extension_features(port)),'layer_name':name[:-5],
            'geometry_column':'geom','fid_column':'fid','features':m.fixture.extension_features(port)} for port,name in zip(m.fixture.EXTENSION_PORTS,m.fixture.EXTENSION_TARGETS)}}


class ExtensionClient(InputDiagnosticClient):
    def __init__(self):
        super().__init__();self.sources=[];self.outputs=[];self.scan_tenant=2;self.scan_actor=33;self.mutate_graph=None
        for i,(name,count,columns) in enumerate(zip(m.fixture.EXTENSION_SOURCES,[7,14,1],[6,4,4])):
            item=base.item(name,10+i);item['full_name']=name;item['attributes']['type_info']['container']['children'][0].update(row_count=count,column_count=columns);self.sources.append(item)
        for i,(name,count,columns) in enumerate(zip(m.fixture.EXTENSION_TARGETS,[7,8],[6,11])):
            item=base.item(name,20+i);item['attributes']['type_info']['container']['children'][0].update(row_count=count,column_count=columns);self.outputs.append(item)
        self.scans=['scan-'+name for name in m.fixture.EXTENSION_SOURCES]
        locators=[f'addp://engine/3/path/{name}?type=file&item_id={10+i}' for i,name in enumerate(m.fixture.EXTENSION_SOURCES)]
        parent='addp://engine/3/path/outputs?type=directory';targets={p:f'addp://engine/3/path/outputs/{n}?type=file' for p,n in zip(m.fixture.EXTENSION_PORTS,m.fixture.EXTENSION_TARGETS)}
        facts={'schema_version':'addp.lineage-facts/v1','inputs':[{'port':p,'locator':l} for p,l in zip(['input.load_source','input.load_reference'],locators)],'outputs':[{'locator':t,'write_mode':'create'} for t in targets.values()]}
        self.success={'execution_id':'extension-run','status':'success','triggered_by':33,'outputs':{'save_'+p:{'resource':{'locator':t,'type':'file','write_mode':'create'}} for p,t in targets.items()},
            'metadata':{'lineage_facts':facts,'result':{'meta_scan_runs':[{'execution_id':'output-scan-'+p,'target_locator':t,'status':'submitted'} for p,t in targets.items()]}},
            'execution_config':{'content':{'workflow_definition':m.extension_definition(locators,parent)}}}
        self.failed={'execution_id':'extension-failed','status':'failed','triggered_by':33,'outputs':{},'metadata':{},'execution_config':{'content':{'workflow_definition':m.extension_definition(locators,parent,failed=True)}}}
    def request(self,method,path,expected,body=None):
        if method=='POST' and path.endswith('/develop/executions'):
            self.calls.append((method,path,body));failed=body['content']['workflow_definition']['tasks'][-1]['params']['target_name'].startswith('failed-')
            value=self.failed if failed else self.success;self.executions[value['execution_id']]=value
            if not failed:self.scans.extend(['output-scan-result','output-scan-details'])
            data={'execution_id':value['execution_id']}
        elif path.endswith('/engines/3/items'):data=[*self.sources,*self.outputs]
        elif path in ['/api/v1/meta/items/20','/api/v1/meta/items/21']:data=self.outputs[int(path.rsplit('/',1)[1])-20]
        elif path.startswith('/api/v1/meta/lineage/graph?'):
            target=int(path.split('item_id=')[1].split('&')[0]);data={'truncated':False,'nodes':[],'edges':[{'source':{'item_id':s},'target':{'item_id':target},'relation_kind':'derive','status':'active','evidence':{'execution_id':'extension-run'}} for s in (10,11)]}
            if self.mutate_graph:self.mutate_graph(data)
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':self.scan_tenant,'triggered_by':self.scan_actor}
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


class ExtensionOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.EXTENSION_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=extension_physical,browser=lambda repo,env,evidence:evidence):
        def diagnostic(repo,env,identifier,**kwargs):
            self.assertEqual(kwargs['task'],'extend');self.assertEqual(kwargs['reason'],m.fixture.EXTENSION_FAILURE);self.assertLessEqual(kwargs['timeout'],30)
            return {'execution_id':identifier,'cause':kwargs['cause'],'source':'develop_runtime_log'}
        return m.run_scenario(Path('.'),self.env,client or ExtensionClient(),physical,browser,diagnostic_runner=diagnostic)
    def test_extension_exact_two_sources_per_output_two_scans_four_edges_and_isolated_failure(self):
        client=ExtensionClient();report=self.run_case(client)
        self.assertEqual(report['schema_version'],'addp.line-extension/v1');self.assertEqual(report['result'],'passed')
        self.assertEqual([len(report['browser']['expected'][p]) for p in m.fixture.EXTENSION_PORTS],[7,8])
        self.assertEqual(sum(len(g['edges']) for g in report['raw_lineage'].values()),4)
        self.assertEqual(len(report['initial_scan_inventory']),3);self.assertEqual(len(report['final_scan_inventory']),5)
        self.assertEqual(len(report['meta_after']),5)
        posts=[body for method,path,body in client.calls if method=='POST' and path.endswith('/develop/executions')]
        self.assertEqual(len(posts),2)
        self.assertTrue(posts[1]['content']['workflow_definition']['tasks'][1]['params']['locator'].startswith('addp://engine/3/path/extension-failure-reference.gpkg'))
        self.assertEqual(report['failure']['diagnostic']['cause'],'unrepresentable_first_intersection')
    def test_extension_binding_order_independent_and_all_identity_size_fact_scan_guards(self):
        c=ExtensionClient();c.success['metadata']['lineage_facts']['inputs'].reverse();self.run_case(c)
        mutations=[lambda c:c.success['metadata']['lineage_facts']['inputs'][1].update(locator='failure-reference'),
            lambda c:c.success['metadata']['lineage_facts']['inputs'].append(copy.deepcopy(c.success['metadata']['lineage_facts']['inputs'][0])),
            lambda c:c.success['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),lambda c:c.success['outputs'].pop('save_details'),
            lambda c:c.success['metadata']['result']['meta_scan_runs'][1].update(execution_id='output-scan-result'),
            lambda c:c.success.update(triggered_by=True),lambda c:c.success.update(execution_config={'content':{'workflow_definition':{}}}),
            lambda c:setattr(c,'scan_tenant','2'),lambda c:setattr(c,'scan_actor',99),
            lambda c:c.sources[0].update(size_bytes=0),lambda c:c.outputs[1].update(size_bytes='98304'),
            lambda c:c.outputs[1]['attributes']['type_info']['container']['children'][0].update(row_count=7),
            lambda c:c.failed.update(outputs={'bad':{}}),lambda c:c.failed.update(triggered_by=99),
            lambda c:c.failed['metadata'].update(lineage_facts={'outputs':[{}]}),lambda c:c.failed['metadata'].update(result={'meta_scan_runs':[{}]})]
        for mutate in mutations:
            c=ExtensionClient();mutate(c)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(c)
    def test_extension_missing_duplicate_failure_source_and_old_execution_edges_rejected(self):
        for mutate in [lambda g:g['edges'].pop(),lambda g:g['edges'].append(copy.deepcopy(g['edges'][0])),
            lambda g:g['edges'][1]['source'].update(item_id=12),lambda g:g['edges'][1]['evidence'].update(execution_id='old'),lambda g:g.update(truncated=True)]:
            c=ExtensionClient();c.mutate_graph=mutate
            with patch.object(m.raster,'wait_lineage',return_value={}):
                with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(c)
    def test_extension_failure_or_browser_error_keeps_incomplete_original_payload_and_final_guards(self):
        with self.assertRaisesRegex(m.SuiteError,'browser broke'):self.run_case(browser=lambda *a:(_ for _ in ()).throw(m.SuiteError('browser broke')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-extension-checkpoint.json').read_text())
        self.assertEqual(checkpoint['stage'],'before_browser');self.assertEqual(checkpoint['result'],'incomplete')
        self.assertEqual(checkpoint['failure']['execution']['execution_id'],'extension-failed')
        calls=0
        def changed(*a):
            nonlocal calls
            calls+=1;value=extension_physical()
            if calls==3:value['targets'][m.fixture.EXTENSION_TARGETS[1]]['sha256']='changed'
            return value
        with self.assertRaisesRegex(m.SuiteError,'physical files'):self.run_case(physical=changed)
        c=ExtensionClient()
        def meta_changed(*a):c.sources[2]['size_bytes']=999;return a[2]
        with self.assertRaisesRegex(m.SuiteError,'Meta changed'):self.run_case(c,browser=meta_changed)
    def test_extension_actual_multiline_and_detail_map_rejects_missing_attribute_direction_pixels_and_old_geometry(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r'''
import assert from 'node:assert/strict';import{readFileSync}from'node:fs';
const source=readFileSync(process.argv[1],'utf8'),expectations=JSON.parse(process.argv[2]);
const {webMercatorPreviewCoordinate}=await import(process.argv[3]);
const a=source.indexOf('function normalizedMapExpectation('),b=source.indexOf('async function verifyNetworkCase(',a);
const expectation=new Function('webMercatorPreviewCoordinate',source.slice(a,b)+';return normalizedMapExpectation')(webMercatorPreviewCoordinate);
const start=source.indexOf('async function assertExtensionLineMap('),end=source.indexOf('function persistExtensionEvidence(',start);
const expect={poll:fn=>({async toEqual(x){assert.deepEqual(await fn(),x)},async toBe(x){assert.equal(await fn(),x)}})};
const check=new Function('expect','normalizedMapExpectation','webMercatorPreviewCoordinate',source.slice(start,end)+';return assertExtensionLineMap')(expect,expectation,webMercatorPreviewCoordinate);
const project=c=>typeof c[0]==='number'?webMercatorPreviewCoordinate(c):c.map(project);
async function run(rows,expected,mode='normal'){
 const pixels=new Uint8ClampedArray(40*40*4);
 for(let x=0;x<36;x++){const i=(10*40+x)*4;pixels.set([0,200,0,255],i)}
 if(mode==='old-only'){pixels.fill(0);for(let y=0;y<7;y++)for(let x=0;x<6;x++)pixels.set([0,200,0,255],(y*40+x)*4)}
 if(mode==='blank')pixels.fill(0);
 const features=rows.map(r=>({get:k=>r.properties[k],getGeometry:()=>({getType:()=>r.geometry.type,getCoordinates:()=>project(r.geometry.coordinates)})}));
 const context={canvas:{width:40,height:40,getBoundingClientRect:()=>({left:0,top:0,width:40,height:40})},getImageData:()=>({data:pixels})};
 const layer={getVisible:()=>mode!=='hidden',getOpacity:()=>1,getSource:()=>({getFeatures:()=>features}),getRenderer:()=>({context})};
 const map={getView:()=>({getAnimating:()=>false,getResolution:()=>1,getProjection:()=>({getCode:()=> 'EPSG:3857'})}),getLayers:()=>({getArray:()=>[layer]}),getPixelFromCoordinate:()=>[10,10],getViewport:()=>({getBoundingClientRect:()=>({left:0,top:0})})};
 await check({evaluate:async(fn,arg)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},arg)},expected);
}
for(const expected of Object.values(expectations)){
 await run(expected,expected);
 for(const mode of ['blank','hidden','old-only'])await assert.rejects(()=>run(expected,expected,mode));
 for(const mutation of [r=>r.pop(),r=>r.reverse(),r=>r[0].properties[Object.keys(r[0].properties)[0]]='wrong',r=>r[0].geometry.type='Point']){
  const rows=structuredClone(expected);mutation(rows);if(JSON.stringify(rows)!==JSON.stringify(expected)){
   // Rendering order is immaterial; a reversed row array preserves the same features.
   if(JSON.stringify(rows)===JSON.stringify([...expected].reverse()))await run(rows,expected);else await assert.rejects(()=>run(rows,expected));
  }
 }
 const changed=structuredClone(expected),coordinates=changed[0].geometry.coordinates;
 if(changed[0].geometry.type==='MultiLineString')coordinates[0].pop();else coordinates[0][0]+=1;
 await assert.rejects(()=>run(changed,expected));
}
'''
        value=subprocess.run(['node','--input-type=module','-e',script,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),json.dumps({p:m.fixture.extension_features(p) for p in m.fixture.EXTENSION_PORTS}),(repository/'manager/frontend/e2e/geometry-preview-coordinate.js').as_uri()],text=True,capture_output=True)
        self.assertEqual(value.returncode,0,value.stdout+value.stderr)

    def test_extension_failure_diagnostic_must_bind_exact_uuid_and_final_budget_stays_bounded(self):
        for diagnostic in ({'execution_id':'other','cause':'unrepresentable_first_intersection','source':'develop_runtime_log'},
                {'execution_id':'extension-failed','cause':'other','source':'develop_runtime_log'},{}):
            with self.subTest(diagnostic=diagnostic),self.assertRaisesRegex(m.SuiteError,'diagnostic binding'):
                m.run_scenario(Path('.'),self.env,ExtensionClient(),extension_physical,lambda *a:a[2],diagnostic_runner=lambda *a,**kw:diagnostic)
        environment={**self.env,'ADDP_ONLINE_TEST_TIMEOUT_SECONDS':'0'}
        with self.assertRaisesRegex(m.SuiteError,'budget exhausted'):
            m.run_scenario(Path('.'),environment,ExtensionClient(),extension_physical,lambda *a:a[2])

    def test_extension_installs_real_canvas_observer_before_first_login_navigation(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r'''
import assert from 'node:assert/strict';import{readFileSync}from'node:fs';
const source=readFileSync(process.argv[1],'utf8');
const config=JSON.parse(process.argv[2]);const marker="test('"+config.title+"',async({page})=>{";
const start=source.indexOf(marker);assert.ok(start>=0);
const end=source.indexOf('\n})',start);assert.ok(end>start);
const body=source.slice(start+marker.length,end);
const AsyncFunction=Object.getPrototypeOf(async function(){}).constructor;
const helper=source.slice(source.indexOf('async function verifyLineEditCase('),source.indexOf("test('线端延伸独立普通User双成果与精确首次交点拒绝'"));
const run=new AsyncFunction('page','expect','process','observeLineageCanvas','login','writeFileSync','resolve',helper+';'+body);
const events=[],stop=new Error('stop after first navigation'),page={on(){},waitForResponse(){events.push('read');return Promise.resolve({})}};
const evidence={case:config.case,sources:Array(config.count),outputs:[{port:'result'},{port:'details'}],expected:{result:Array(config.case==='line-trimming'?15:config.case==='line-extension'?7:12),details:Array(config.case==='line-trimming'?9:config.case==='line-extension'?8:10)}};
const expect=value=>({toBe:x=>assert.equal(value,x),toHaveLength:x=>assert.equal(value.length,x),toEqual:x=>assert.deepEqual(value,x)});
let installed=false;
await assert.rejects(()=>run(page,expect,{env:{ADDP_ONLINE_TOPOLOGY_EVIDENCE:JSON.stringify(evidence)}},async actual=>{assert.equal(actual,page);await Promise.resolve();installed=true;events.push('observer')},async()=>{assert.ok(installed,'Canvas observer was not awaited before login');events.push('login');throw stop},()=>{},(...parts)=>parts.join('/')),error=>error===stop);
assert.deepEqual(events,['observer','read','login']);
'''
        for config in ({'case':m.fixture.TRIM_CASE,'count':11,'title':m.fixture.TRIM_TITLE},{'case':'line-extension','count':3,'title':m.fixture.EXTENSION_TITLE},{'case':m.fixture.ENDPOINT_CASE,'count':7,'title':m.fixture.ENDPOINT_TITLE}):
            value=subprocess.run(['node','--input-type=module','-e',script,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),json.dumps(config)],text=True,capture_output=True)
            self.assertEqual(value.returncode,0,value.stdout+value.stderr)


def endpoint_physical(*args):
    return {'exact_inventory_verified':True,'sources':{n:{'size_bytes':98304,'sha256':n} for n in m.fixture.ENDPOINT_SOURCES},
        'targets':{name:{'size_bytes':98304,'sha256':name,'feature_count':len(m.fixture.endpoint_features(port)),'layer_name':name[:-5],
            'geometry_column':'geom','fid_column':'fid','features':m.fixture.endpoint_features(port)} for port,name in zip(m.fixture.ENDPOINT_PORTS,m.fixture.ENDPOINT_TARGETS)}}


class EndpointClient(ExtensionClient):
    def __init__(self):
        super().__init__();self.sources=[];self.outputs=[];self.mutate_failed=None;self.failed_graph=None
        for i,(name,kind) in enumerate(zip(m.fixture.ENDPOINT_SOURCES,m.fixture.ENDPOINT_KINDS)):
            rows=m.fixture.endpoint_features(kind);item=base.item(name,10+i);item['full_name']=name
            item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.sources.append(item)
        for i,(name,port) in enumerate(zip(m.fixture.ENDPOINT_TARGETS,m.fixture.ENDPOINT_PORTS)):
            rows=m.fixture.endpoint_features(port);item=base.item(name,20+i)
            item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.outputs.append(item)
        self.scans=['scan-'+name for name in m.fixture.ENDPOINT_SOURCES]
        self.locators=[f'addp://engine/3/path/{name}?type=file&item_id={10+i}' for i,name in enumerate(m.fixture.ENDPOINT_SOURCES)]
        self.parent='addp://engine/3/path/outputs?type=directory'
        targets={p:f'addp://engine/3/path/outputs/{n}?type=file' for p,n in zip(m.fixture.ENDPOINT_PORTS,m.fixture.ENDPOINT_TARGETS)}
        self.success.update(execution_id='endpoint-run',outputs={'save_'+p:{'resource':{'locator':t,'type':'file','write_mode':'create'}} for p,t in targets.items()},
            execution_config={'content':{'workflow_definition':m.endpoint_definition(self.locators,self.parent)}})
        self.success['metadata']={'lineage_facts':{'schema_version':'addp.lineage-facts/v1','inputs':[{'port':p,'locator':l} for p,l in zip(['input.load_source','input.load_reference'],self.locators)],
            'outputs':[{'locator':t,'write_mode':'create'} for t in targets.values()]},'result':{'meta_scan_runs':[{'execution_id':'output-scan-'+p,'target_locator':t,'status':'submitted'} for p,t in targets.items()]}}
    def request(self,method,path,expected,body=None):
        if method=='POST' and path.endswith('/develop/executions'):
            self.calls.append((method,path,body));definition=body['content']['workflow_definition'];name=definition['tasks'][-1]['params']['target_name']
            if name.startswith('failed-'):
                key=next(k for k,_ in m.fixture.ENDPOINT_FAILURES if name.startswith('failed-'+k+'-'))
                value={'execution_id':'failed-'+key,'status':'failed','triggered_by':33,'outputs':{},'metadata':{},'execution_config':{'content':{'workflow_definition':definition}}}
                if self.mutate_failed:self.mutate_failed(value)
            else:value=self.success;self.scans.extend(['output-scan-result','output-scan-details'])
            self.executions[value['execution_id']]=value;data={'execution_id':value['execution_id']}
        elif path.startswith('/api/v1/meta/lineage/graph?'):
            if 'direction=downstream' in path:
                data={'truncated':False,'nodes':[],'edges':[]}
                if self.failed_graph:self.failed_graph(data)
            else:
                target=int(path.split('item_id=')[1].split('&')[0]);data={'truncated':False,'nodes':[],'edges':[{'source':{'item_id':v},'target':{'item_id':target},'relation_kind':'derive','status':'active','evidence':{'execution_id':'endpoint-run'}} for v in (10,11)]}
                if self.mutate_graph:self.mutate_graph(data)
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


class EndpointOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.ENDPOINT_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=endpoint_physical,browser=lambda repo,env,evidence:evidence,diagnostic=None):
        def check(repo,env,identifier,**kwargs):
            self.assertEqual(kwargs['task'],'snap');self.assertLessEqual(kwargs['timeout'],30)
            self.assertEqual(kwargs['reason'],dict(m.fixture.ENDPOINT_FAILURES)[kwargs['cause']])
            return {'execution_id':identifier,'cause':kwargs['cause'],'source':'develop_runtime_log'}
        return m.run_scenario(Path('.'),self.env,client or EndpointClient(),physical,browser,diagnostic_runner=diagnostic or check)
    def test_endpoint_four_failures_no_publication_two_scans_four_edges(self):
        c=EndpointClient();report=self.run_case(c)
        self.assertEqual(report['result'],'passed');self.assertEqual(report['schema_version'],'addp.endpoint-snapping/v1')
        self.assertEqual([len(report['browser']['expected'][p]) for p in m.fixture.ENDPOINT_PORTS],[12,10])
        self.assertEqual(sum(len(g['edges']) for g in report['raw_lineage'].values()),4)
        self.assertEqual([len(report[k]) for k in ['initial_scan_inventory','final_scan_inventory','meta_after']],[7,9,9])
        self.assertEqual([f['key'] for f in report['failures']],[k for k,_ in m.fixture.ENDPOINT_FAILURES])
        self.assertEqual(len({f['execution_id'] for f in report['failures']}),4)
        self.assertEqual(sum(method=='POST' and path.endswith('/develop/executions') for method,path,_ in c.calls),5)
        self.assertTrue(all(len(f['source_lineage'])==2 for f in report['failures']))
    def test_endpoint_rejects_wrong_identity_extra_or_misbound_facts_scans_and_edges(self):
        mutations=[lambda c:c.success.update(triggered_by=True),lambda c:setattr(c,'scan_tenant',True),lambda c:setattr(c,'scan_actor',99),
            lambda c:c.success['outputs']['save_result']['resource'].update(write_mode='fail'),
            lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(locator=c.locators[2]),
            lambda c:c.success['metadata']['lineage_facts']['inputs'].append(copy.deepcopy(c.success['metadata']['lineage_facts']['inputs'][0])),
            lambda c:c.success['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),
            lambda c:c.success['metadata']['result']['meta_scan_runs'].pop(),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'][0]['source'].update(item_id=12)),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'][0]['evidence'].update(execution_id='old'))]
        for mutate in mutations:
            c=EndpointClient();mutate(c)
            with patch.object(m.raster,'wait_lineage',return_value={}):
                with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(c)
        c=EndpointClient();c.success['metadata']['lineage_facts']['inputs'].reverse();self.run_case(c)
    def test_endpoint_every_failure_rejects_publication_derive_or_wrong_diagnostic(self):
        for mutation in [lambda v:v.update(outputs={'save_result':{}}),lambda v:v.update(triggered_by=True),
            lambda v:v['metadata'].update(lineage_facts={'outputs':[{}]}),lambda v:v['metadata'].update(result={'meta_scan_runs':[{}]})]:
            c=EndpointClient();c.mutate_failed=mutation
            with self.assertRaises(m.SuiteError):self.run_case(c)
        for key,_ in m.fixture.ENDPOINT_FAILURES:
            c=EndpointClient();c.failed_graph=lambda g:g['edges'].append({'relation_kind':'derive','evidence':{'execution_id':'failed-'+key}})
            with self.subTest(key=key),self.assertRaises(m.SuiteError):self.run_case(c)
            with self.assertRaises(m.SuiteError):self.run_case(diagnostic=lambda *a,**kw:{'execution_id':'wrong','cause':key,'source':'develop_runtime_log'})
    def test_endpoint_failure_or_browser_error_preserves_raw_checkpoint_and_all_final_guards(self):
        with self.assertRaisesRegex(m.SuiteError,'browser broke'):self.run_case(browser=lambda *a:(_ for _ in ()).throw(m.SuiteError('browser broke')))
        evidence=json.loads((Path(self.directory.name)/'topology-endpoint-checkpoint.json').read_text())
        self.assertEqual(evidence['result'],'incomplete');self.assertEqual(len(evidence['failures']),4)
        self.assertTrue(all('source_lineage' in v for v in evidence['failures']))
        calls=0
        def changed(*a):
            nonlocal calls
            calls+=1;value=endpoint_physical()
            if calls==6:value['targets'][m.fixture.ENDPOINT_TARGETS[0]]['sha256']='changed'
            return value
        with self.assertRaises(m.SuiteError):self.run_case(physical=changed)
        c=EndpointClient()
        def mutate(*a):c.sources[-1]['size_bytes']=1;return a[2]
        with self.assertRaisesRegex(m.SuiteError,'Meta changed'):self.run_case(c,browser=mutate)
        self.env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS']='0'
        with self.assertRaisesRegex(m.SuiteError,'budget exhausted'):self.run_case()

    def test_endpoint_actual_line_map_rejects_missing_attributes_changed_endpoint_and_blank_pixels(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r'''
import assert from 'node:assert/strict';import{readFileSync}from'node:fs';
const source=readFileSync(process.argv[1],'utf8'),expectations=JSON.parse(process.argv[2]);
const {webMercatorPreviewCoordinate}=await import(process.argv[3]);
const a=source.indexOf('function normalizedMapExpectation('),b=source.indexOf('async function verifyNetworkCase(',a);
const expectation=new Function('webMercatorPreviewCoordinate',source.slice(a,b)+';return normalizedMapExpectation')(webMercatorPreviewCoordinate);
const start=source.indexOf('async function assertExtensionLineMap('),end=source.indexOf('function persistExtensionEvidence(',start);
const expect={poll:fn=>({async toEqual(x){assert.deepEqual(await fn(),x)},async toBe(x){assert.equal(await fn(),x)}})};
const check=new Function('expect','normalizedMapExpectation','webMercatorPreviewCoordinate',source.slice(start,end)+';return assertExtensionLineMap')(expect,expectation,webMercatorPreviewCoordinate);
const project=c=>typeof c[0]==='number'?webMercatorPreviewCoordinate(c):c.map(project);
async function run(rows,expected,mode='normal'){
 const pixels=new Uint8ClampedArray(40*40*4);
 for(let x=0;x<36;x++){const i=(10*40+x)*4;pixels.set([0,200,0,255],i)}
 if(mode==='old-only'){pixels.fill(0);for(let y=0;y<7;y++)for(let x=0;x<6;x++)pixels.set([0,200,0,255],(y*40+x)*4)}
 if(mode==='blank')pixels.fill(0);
 const features=rows.map(r=>({get:k=>r.properties[k],getGeometry:()=>({getType:()=>r.geometry.type,getCoordinates:()=>project(r.geometry.coordinates)})}));
 const context={canvas:{width:40,height:40,getBoundingClientRect:()=>({left:0,top:0,width:40,height:40})},getImageData:()=>({data:pixels})};
 const layer={getVisible:()=>mode!=='hidden',getOpacity:()=>1,getSource:()=>({getFeatures:()=>features}),getRenderer:()=>({context})};
 const map={getView:()=>({getAnimating:()=>false,getResolution:()=>1,getProjection:()=>({getCode:()=> 'EPSG:3857'})}),getLayers:()=>({getArray:()=>[layer]}),getPixelFromCoordinate:()=>[10,10],getViewport:()=>({getBoundingClientRect:()=>({left:0,top:0})})};
 await check({evaluate:async(fn,arg)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},arg)},expected);
}
for(const expected of Object.values(expectations)){
 await run(expected,expected);
 for(const mode of ['blank','hidden','old-only'])await assert.rejects(()=>run(expected,expected,mode));
 for(const mutation of [r=>r.pop(),r=>r.reverse(),r=>r[0].properties[Object.keys(r[0].properties)[0]]='wrong',r=>r[0].geometry.type='Point']){
  const rows=structuredClone(expected);mutation(rows);if(JSON.stringify(rows)!==JSON.stringify(expected)){
   // Rendering order is immaterial; a reversed row array preserves the same features.
   if(JSON.stringify(rows)===JSON.stringify([...expected].reverse()))await run(rows,expected);else await assert.rejects(()=>run(rows,expected));
  }
 }
 const changed=structuredClone(expected),coordinates=changed[0].geometry.coordinates;
 if(changed[0].geometry.type==='MultiLineString')coordinates[0].pop();else coordinates[0][0]+=1;
 await assert.rejects(()=>run(changed,expected));
}
'''
        value=subprocess.run(['node','--input-type=module','-e',script,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),json.dumps({'result':m.fixture.endpoint_features('result'),'trim':m.fixture.trim_features('result'),'rings':[{**row,'properties':{k:row['properties'][k] for k in ('parcel_key','source_id','part_no','ring_no','ring_id','is_hole')}} for row in m.fixture.rings_features('result')]}),(repository/'manager/frontend/e2e/geometry-preview-coordinate.js').as_uri()],text=True,capture_output=True)
        self.assertEqual(value.returncode,0,value.stdout+value.stderr)


    def test_endpoint_actual_point_map_requires_all_original_points_and_attributes_and_style_pixels(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r"""
import assert from 'node:assert/strict';import{readFileSync}from'node:fs';
const source=readFileSync(process.argv[1],'utf8'),expected=JSON.parse(process.argv[2]);
const {webMercatorPreviewCoordinate}=await import(process.argv[3]);
const a=source.indexOf('function proximityPointExpectation('),b=source.indexOf('function assertProximityLineage(',a);
const expect={poll:fn=>({async toEqual(v){assert.deepEqual(await fn(),v)},async toBeGreaterThan(v){assert.ok(await fn()>v)}})};
function assertion(value){return{toHaveLength:v=>assert.equal(value.length,v),toBe:v=>assert.equal(value,v)}}
assertion.poll=expect.poll;
globalThis.OffscreenCanvas=class{getContext(){return{clearRect(){},set fillStyle(x){},fillRect(){},getImageData(){return{data:[64,158,255,255]}}}}};
const check=new Function('expect','webMercatorPreviewCoordinate',source.slice(a,b)+';return assertProximityPointMap')(assertion,webMercatorPreviewCoordinate);
async function run(rows,blank=false){
 const features=rows.map(r=>({get:k=>r.properties[k],getGeometry:()=>({getType:()=>r.geometry.type,getCoordinates:()=>webMercatorPreviewCoordinate(r.geometry.coordinates)})}));
 const pixels=new Uint8ClampedArray(40*4);if(!blank)for(let i=0;i<40;i++)pixels.set([64,158,255,255],i*4);
 const layer={getVisible:()=>true,getOpacity:()=>1,getSource:()=>({getFeatures:()=>features}),getStyleFunction:()=>()=>({getImage:()=>({getFill:()=>({getColor:()=> '#409EFF'})})}),getRenderer:()=>({context:{getImageData:()=>({data:pixels}),canvas:{width:40,height:1}}})};
 const map={getView:()=>({getAnimating:()=>false,getResolution:()=>1,getProjection:()=>({getCode:()=> 'EPSG:3857'})}),getLayers:()=>({getArray:()=>[layer]})};
 await check({evaluate:async(fn,arg)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},arg)},expected);
}
await run(expected);await assert.rejects(()=>run(expected,true));
for(const change of [r=>r.pop(),r=>r[0].properties.reference_id='lost',r=>r[0].geometry.coordinates=[.5,0],r=>r[0].geometry.type='LineString']){
 const rows=structuredClone(expected);change(rows);await assert.rejects(()=>run(rows));
}
"""
        value=subprocess.run(['node','--input-type=module','-e',script,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),json.dumps(m.fixture.endpoint_features('details')),(repository/'manager/frontend/e2e/geometry-preview-coordinate.js').as_uri()],text=True,capture_output=True)
        self.assertEqual(value.returncode,0,value.stdout+value.stderr)


def rings_physical(*args):
    return {'exact_inventory_verified':True,'sources':{n:{'size_bytes':98304,'sha256':n} for n in m.fixture.RINGS_SOURCES},
        'targets':{m.fixture.RINGS_TARGET:{'size_bytes':98304,'sha256':'rings','feature_count':8,'layer_name':'rings','geometry_column':'geom','fid_column':'fid','fields':list(m.fixture.rings_features('result')[0]['properties'])}}}


class RingsClient(InputDiagnosticClient):
    def __init__(self):
        super().__init__();self.sources=[];self.mutate_success=None;self.mutate_failed=None;self.mutate_graph=None;self.failed_graph=None;self.scan_tenant=2;self.scan_actor=33
        for i,(name,kind) in enumerate(zip(m.fixture.RINGS_SOURCES,('source','conflict','invalid'))):
            rows=m.fixture.rings_features(kind);item=base.item(name,10+i);item['full_name']=name;item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.sources.append(item)
        self.output=base.item(m.fixture.RINGS_TARGET,20);self.output['attributes']['type_info']['container']['children'][0].update(row_count=8,column_count=13)
        self.scans=['scan-'+n for n in m.fixture.RINGS_SOURCES];self.locators=[f'addp://engine/3/path/{n}?type=file&item_id={10+i}' for i,n in enumerate(m.fixture.RINGS_SOURCES)];self.parent='addp://engine/3/path/outputs?type=directory'
        target='addp://engine/3/path/outputs/rings.gpkg?type=file'
        self.success={'execution_id':'rings-run','status':'success','triggered_by':33,'outputs':{'save_result':{'resource':{'locator':target,'type':'file','write_mode':'create'}}},
            'metadata':{'lineage_facts':{'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.rings_input','locator':self.locators[0]}],'outputs':[{'locator':target,'write_mode':'create'}]},
                'result':{'meta_scan_runs':[{'execution_id':'rings-output-scan','target_locator':target,'status':'submitted'}]}},
            'execution_config':{'content':{'workflow_definition':m.rings_definition(self.locators,self.parent)}}}
    def request(self,method,path,expected,body=None):
        if method=='POST' and path.endswith('/develop/executions'):
            self.calls.append((method,path,body));definition=body['content']['workflow_definition'];name=definition['tasks'][-1]['params']['target_name']
            if name.startswith('failed-'):
                key=next(k for k,_ in m.fixture.RINGS_FAILURES if name.startswith('failed-'+k+'-'))
                value={'execution_id':'failed-'+key,'status':'failed','triggered_by':33,'outputs':{},'metadata':{},'execution_config':{'content':{'workflow_definition':definition}}}
                if self.mutate_failed:self.mutate_failed(value)
            else:
                value=copy.deepcopy(self.success);self.scans.append('rings-output-scan')
                if self.mutate_success:self.mutate_success(value)
            self.executions[value['execution_id']]=value;data={'execution_id':value['execution_id']}
        elif path.endswith('/engines/3/items'):data=[*self.sources,self.output]
        elif path=='/api/v1/meta/items/20':data=self.output
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':self.scan_tenant,'triggered_by':self.scan_actor}
        elif '/meta/lineage/graph?' in path:
            data={'truncated':False,'nodes':[],'edges':[] if 'direction=downstream' in path else [{'source':{'item_id':10},'target':{'item_id':20},'relation_kind':'derive','status':'active','evidence':{'execution_id':'rings-run'}}]}
            callback=self.failed_graph if 'direction=downstream' in path else self.mutate_graph
            if callback:callback(data)
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


class RingsOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.RINGS_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=rings_physical,browser=lambda repo,env,evidence:evidence,diagnostic=None):
        def checked(repo,env,uid,**kw):
            self.assertEqual(kw['task'],'rings');self.assertEqual(kw['reason'],dict(m.fixture.RINGS_FAILURES)[kw['cause']]);self.assertLessEqual(kw['timeout'],30)
            return {'execution_id':uid,'cause':kw['cause'],'source':'develop_runtime_log'}
        with patch.object(m.raster,'wait_lineage',return_value={}):
            return m.run_scenario(Path('.'),self.env,client or RingsClient(),physical,browser,diagnostic_runner=diagnostic or checked)
    def test_rings_one_source_one_scan_one_edge_and_two_failures(self):
        client=RingsClient();report=self.run_case(client)
        self.assertEqual(report['schema_version'],'addp.polygon-rings/v1');self.assertEqual(report['result'],'passed');self.assertEqual(len(report['raw_lineage']['edges']),1)
        self.assertEqual(len(report['final_scan_inventory']),4);self.assertEqual(len(report['meta_after']),4);self.assertEqual(len(report['failures']),2)
        self.assertEqual(len(report['browser']['expected']['result']),8);self.assertEqual(report['browser']['expected']['result'][5]['properties']['grade'],str(2**53+1))
        self.assertEqual(sum(method=='POST' and path.endswith('/develop/executions') for method,path,_ in client.calls),3)
    def test_rings_rejects_wrong_identity_facts_scans_and_current_graph(self):
        mutations=[lambda c:setattr(c,'scan_tenant',True),lambda c:setattr(c,'scan_actor',99),
            lambda c:c.success['outputs']['save_result']['resource'].update(write_mode='replace'),
            lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(port='input.other'),
            lambda c:c.success['metadata']['lineage_facts']['inputs'].append(copy.deepcopy(c.success['metadata']['lineage_facts']['inputs'][0])),
            lambda c:c.success['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'].append(copy.deepcopy(g['edges'][0]))),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'][0]['evidence'].update(execution_id='old')),
            lambda c:setattr(c,'mutate_graph',lambda g:g.update(truncated=True))]
        for mutate in mutations:
            client=RingsClient();mutate(client)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_rings_failures_cannot_publish_or_hide_lineage_or_wrong_diagnostic(self):
        for mutate in [lambda f:f.update(outputs={'save_result':{}}),lambda f:f.update(triggered_by=True),lambda f:f['metadata'].update(lineage_facts={'outputs':[{}]}),lambda f:f['metadata'].update(result={'meta_scan_runs':[{}]})]:
            c=RingsClient();c.mutate_failed=mutate
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(c)
        c=RingsClient();c.failed_graph=lambda g:g['edges'].append({'relation_kind':'derive','evidence':{'execution_id':'failed-conflict'}})
        with self.assertRaises(m.SuiteError):self.run_case(c)
        with self.assertRaises(m.SuiteError):self.run_case(diagnostic=lambda *a,**kw:{'execution_id':'foreign','cause':kw['cause'],'source':'develop_runtime_log'})
    def test_rings_incomplete_browser_and_final_immutable_guards(self):
        def fail(*a):raise m.SuiteError('browser failed')
        with self.assertRaises(m.SuiteError):self.run_case(browser=fail)
        checkpoint=json.loads((Path(self.directory.name)/'topology-rings-checkpoint.json').read_text());self.assertEqual(checkpoint['result'],'incomplete');self.assertEqual(len(checkpoint['failures']),2)
        calls=0
        def changed(*a):
            nonlocal calls
            calls+=1;value=rings_physical()
            if calls==4:value['targets'][m.fixture.RINGS_TARGET]['sha256']='changed'
            return value
        with self.assertRaises(m.SuiteError):self.run_case(physical=changed)
    def test_rings_raw_json_requires_exact_big_integer_values_types_and_all_attributes(self):
        rows=[r['properties'] for r in m.fixture.rings_features('result')];m.validate_rings_preview_text(json.dumps({'data':{'rows':rows}}))
        for mutate in [lambda r:r.pop(),lambda r:r[5].update(grade=float(2**53+1)),lambda r:r[0].update(note='lost-null'),lambda r:r[1].update(is_hole=1),lambda r:r[5].update(geometry='overwritten')]:
            values=copy.deepcopy(rows);mutate(values)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):m.validate_rings_preview_text(json.dumps({'data':{'rows':values}}))

    def test_rings_browser_uses_exact_one_source_edge_and_keeps_incomplete_stage(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r'''
import assert from 'node:assert/strict';import{readFileSync,writeFileSync}from'node:fs';import{resolve}from'node:path';
const source=readFileSync(process.argv[1],'utf8');
const start=source.indexOf('function assertRingsLineage('),end=source.indexOf('function persistRingsEvidence(',start);
const expect=value=>({toBe:other=>assert.equal(value,other),toEqual:other=>assert.deepEqual(value,other)});
const check=new Function('expect',source.slice(start,end)+';return assertRingsLineage')(expect);
const evidence={sources:[{id:10},{id:11},{id:12}],outputs:[{item_id:20}],execution_id:'current'};
const graph={truncated:false,edges:[{source:{item_id:10},target:{item_id:20},evidence:{execution_id:'current'},relation_kind:'derive',status:'active'}]};
check(graph,evidence);
for(const mutate of [g=>g.edges.pop(),g=>g.edges.push(structuredClone(g.edges[0])),g=>g.edges[0].source.item_id=11,g=>g.edges[0].target.item_id=21,g=>g.edges[0].evidence.execution_id='old',g=>g.truncated=true]){
 const copy=structuredClone(graph);mutate(copy);assert.throws(()=>check(copy,evidence));
}
const stop=source.indexOf("test('面界提取",end);
const persist=new Function('writeFileSync','resolve',source.slice(end,stop)+';return persistRingsEvidence')(writeFileSync,resolve);
persist(process.argv[2],{execution_id:'current'},'preview_observed');let raw=JSON.parse(readFileSync(resolve(process.argv[2],'topology-rings-browser-raw.json'),'utf8'));assert.equal(raw.result,'incomplete');
persist(process.argv[2],{execution_id:'current'},'complete','passed');raw=JSON.parse(readFileSync(resolve(process.argv[2],'topology-rings-browser-raw.json'),'utf8'));assert.equal(raw.result,'passed');
'''
        result=subprocess.run(['node','--input-type=module','-e',script,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),self.directory.name],text=True,capture_output=True)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_rings_browser_owner_rejects_partial_foreign_or_rounded_raw_response(self):
        evidence={'case':m.fixture.RINGS_CASE,'run_id':'run','execution_id':'current'}
        rows=[r['properties'] for r in m.fixture.rings_features('result')]
        def run_raw(raw):
            def complete(*a,**kw):
                root=Path(self.directory.name);(root/'topology-browser.json').write_text(json.dumps({**evidence,'schema_version':m.fixture.RINGS_SCHEMA+'-browser/v1','result':'passed'}));(root/'topology-rings-browser-raw.json').write_text(json.dumps(raw))
                return SimpleNamespace(returncode=0)
            with patch.object(m.subprocess,'run',side_effect=complete):return m.browser(Path('.'),self.env,evidence)
        raw={**evidence,'stage':'complete','result':'passed','preview_text':json.dumps({'data':{'rows':rows}})}
        self.assertEqual(run_raw(raw)['result'],'passed')
        for mutate in [lambda r:r.update(result='incomplete'),lambda r:r.update(execution_id='foreign'),lambda r:r.update(run_id='foreign'),lambda r:r.update(stage='preview_observed'),lambda r:r.update(preview_text=json.dumps({'data':{'rows':[{}]}}))]:
            value=copy.deepcopy(raw);mutate(value)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):run_raw(value)


class RingInputDiagnosticOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.RINGS_DIAGNOSTIC_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=None,browser=lambda repo,env,evidence:evidence):
        if physical is None:physical=lambda *args:{'sources':{name:{'size_bytes':98304,'sha256':name} for name in m.fixture.RINGS_DIAGNOSTIC_SOURCES},'targets':{},'exact_inventory_verified':True}
        return m.run_scenario(Path('.'),self.env,client or InputDiagnosticClient(m.fixture.RINGS_DIAGNOSTIC_CASE),physical,browser)
    def test_rings_diagnostic_only_two_failures_keep_frozen_position_and_no_publish(self):
        client=InputDiagnosticClient(m.fixture.RINGS_DIAGNOSTIC_CASE);report=self.run_case(client)
        self.assertEqual(report['schema_version'],'addp.polygon-rings-input-diagnostics/v1')
        self.assertEqual(len(report['executions']),2);self.assertEqual(len(report['sources']),2)
        self.assertEqual(report['initial_scan_inventory'],report['final_scan_inventory'])
        for run,spec in zip(report['executions'],m.RINGS_DIAGNOSTIC_SPECS):
            self.assertEqual(run['expected_diagnostic'],m.expected_input_diagnostic(spec))
            self.assertEqual(run['definition']['tasks'][0]['operator'],'extract_polygon_rings')
            self.assertEqual(run['definition']['tasks'][-1]['operator'],'save')
        self.assertEqual(sum(method=='POST' and path.endswith('/develop/executions') for method,path,_ in client.calls),2)
    def test_rings_diagnostic_rejects_wrong_context_frozen_source_or_publication(self):
        for mutate in [lambda v:v['error_details']['diagnostic']['operator'].update(node_index=2),
            lambda v:v['error_details']['diagnostic']['operator'].update(field='source-private'),
            lambda v:v['execution_config']['content']['workflow_definition']['tasks'][0].update(operator='load'),
            lambda v:v.update(outputs={'save':{}}),lambda v:v['metadata'].update(lineage_facts={'outputs':[{}]}),
            lambda v:v['metadata'].update(result={'meta_scan_runs':[{}]}),lambda v:v.update(triggered_by=True)]:
            c=InputDiagnosticClient(m.fixture.RINGS_DIAGNOSTIC_CASE);c.mutate=mutate
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(c)
        c=InputDiagnosticClient(m.fixture.RINGS_DIAGNOSTIC_CASE);c.sources[0]['size_bytes']=0
        with self.assertRaises(m.SuiteError):self.run_case(c)
    def test_rings_diagnostic_requires_exact_eight_uuid_owner_locale_bindings(self):
        evidence=self.run_case()['browser'];output=Path(self.directory.name)/'topology-browser.json'
        observations=[{'key':run['key'],'execution_id':run['execution_id'],'owner':owner,'locale':locale,
            'raw':run['execution'] if owner=='develop' else {'execution':run['execution']},'clipboard':'safe'}
            for run in evidence['executions'] for owner in ('develop','monitor') for locale in ('zh-cn','en')]
        report={**evidence,'schema_version':'addp.polygon-rings-input-diagnostics-browser/v1','result':'passed','observations':observations}
        def invoke(*args,**kwargs):output.write_text(json.dumps(report));return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=invoke):
            m.browser(Path('.'),self.env,evidence)
            for mutate in [lambda v:v['observations'].pop(),lambda v:v['observations'][0].update(locale='en'),
                lambda v:v['observations'][0].update(execution_id='old'),lambda v:v['observations'][0].update(clipboard='Traceback')]:
                before=copy.deepcopy(report);mutate(report)
                with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)
                report=before


def trim_physical(*args):
    return {'exact_inventory_verified':True,'sources':{n:{'size_bytes':98304,'sha256':n} for n in m.fixture.TRIM_SOURCES},
        'targets':{name:{'size_bytes':98304,'sha256':name,'feature_count':len(m.fixture.trim_features(port)),'layer_name':name[:-5],
            'geometry_column':'geom','fid_column':'fid','features':m.fixture.trim_features(port)}
            for port,name in zip(m.fixture.EXTENSION_PORTS,m.fixture.TRIM_TARGETS)}}


class TrimClient(InputDiagnosticClient):
    def __init__(self):
        super().__init__();self.sources=[];self.outputs=[];self.mutate_failed=None;self.failed_graph=None;self.mutate_graph=None;self.scan_actor=33;self.scan_tenant=2
        for i,(name,kind) in enumerate(zip(m.fixture.TRIM_SOURCES,m.fixture.TRIM_KINDS)):
            rows=m.fixture.trim_features(kind);item=base.item(name,10+i);item['full_name']=name
            item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.sources.append(item)
        for i,(name,port) in enumerate(zip(m.fixture.TRIM_TARGETS,m.fixture.EXTENSION_PORTS)):
            rows=m.fixture.trim_features(port);item=base.item(name,30+i)
            item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.outputs.append(item)
        self.scans=['scan-'+n for n in m.fixture.TRIM_SOURCES]
        self.locators=[f'addp://engine/3/path/{n}?type=file&item_id={10+i}' for i,n in enumerate(m.fixture.TRIM_SOURCES)]
        self.parent='addp://engine/3/path/outputs?type=directory'
        targets={p:f'addp://engine/3/path/outputs/{n}?type=file' for p,n in zip(m.fixture.EXTENSION_PORTS,m.fixture.TRIM_TARGETS)}
        self.success={'execution_id':'trim-run','status':'success','triggered_by':33,
            'outputs':{'save_'+p:{'resource':{'locator':t,'type':'file','write_mode':'create'}} for p,t in targets.items()},
            'execution_config':{'content':{'workflow_definition':m.trim_definition(self.locators,self.parent)}},
            'metadata':{'lineage_facts':{'schema_version':'addp.lineage-facts/v1','inputs':[{'port':p,'locator':l} for p,l in zip(['input.load_source','input.load_reference'],self.locators)],
              'outputs':[{'locator':t,'write_mode':'create'} for t in targets.values()]},'result':{'meta_scan_runs':[{'execution_id':'output-scan-'+p,'target_locator':t,'status':'submitted'} for p,t in targets.items()]}}}
    def request(self,method,path,expected,body=None):
        if method=='POST' and path.endswith('/develop/executions'):
            self.calls.append((method,path,body));definition=body['content']['workflow_definition'];name=definition['tasks'][-1]['params']['target_name']
            if name.startswith('failed-'):
                spec=next(v for v in m.fixture.TRIM_FAILURES if name.startswith('failed-'+v[0]+'-'))
                value={'execution_id':'failed-'+spec[0],'status':'failed','triggered_by':33,'outputs':{},'metadata':{},
                    'execution_config':{'content':{'workflow_definition':definition}},'error_details':{'code':'develop.workflow.invalid_params' if spec[3] else 'develop.workflow.execution_failed','message':'controlled'}}
                if spec[3]:value['error_details']['diagnostic']=m.trim_diagnostic(spec)
                if self.mutate_failed:self.mutate_failed(value)
            else:value=self.success;self.scans.extend(['output-scan-result','output-scan-details'])
            self.executions[value['execution_id']]=value;data={'execution_id':value['execution_id']}
        elif path.endswith('/engines/3/items'):data=[*self.sources,*self.outputs]
        elif path.startswith('/api/v1/meta/items/'):
            item_id=int(path.rsplit('/',1)[1]);data=next(v for v in [*self.sources,*self.outputs] if v['id']==item_id)
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':self.scan_tenant,'triggered_by':self.scan_actor}
        elif path.startswith('/api/v1/meta/lineage/graph?'):
            if 'direction=downstream' in path:
                data={'truncated':False,'nodes':[],'edges':[]}
                if self.failed_graph:self.failed_graph(data)
            else:
                target=int(path.split('item_id=')[1].split('&')[0]);data={'truncated':False,'nodes':[],'edges':[{'source':{'item_id':v},'target':{'item_id':target},'relation_kind':'derive','status':'active','evidence':{'execution_id':'trim-run'}} for v in (10,11)]}
                if self.mutate_graph:self.mutate_graph(data)
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


class TrimOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.TRIM_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=trim_physical,browser=lambda *a:a[2],diagnostic=None):
        def check(repo,env,identifier,**kwargs):
            self.assertEqual((kwargs['task'],kwargs['reason'],kwargs['cause']),('trim',m.fixture.TRIM_LANDING_FAILURE,'landing'))
            return {'execution_id':identifier,'cause':kwargs['cause'],'source':'develop_runtime_log'}
        return m.run_scenario(Path('.'),self.env,client or TrimClient(),physical,browser,diagnostic_runner=diagnostic or check)
    def test_trim_one_success_eight_failures_exact_types_two_scans_four_edges(self):
        c=TrimClient();report=self.run_case(c)
        self.assertEqual(report['schema_version'],'addp.line-trimming/v1');self.assertEqual(report['result'],'passed')
        self.assertEqual([len(report['browser']['expected'][p]) for p in ('result','details')],[15,9])
        self.assertEqual([len(report[k]) for k in ['initial_scan_inventory','final_scan_inventory','meta_after']],[11,13,13])
        self.assertEqual(sum(len(g['edges']) for g in report['raw_lineage'].values()),4)
        self.assertEqual([f['key'] for f in report['failures']],[v[0] for v in m.fixture.TRIM_FAILURES])
        self.assertEqual(len({f['execution_id'] for f in report['failures']}),8)
        self.assertEqual(sum(method=='POST' and path.endswith('/develop/executions') for method,path,_ in c.calls),9)
        self.assertEqual(len(report['browser']['failures']),8)
    def test_trim_rejects_wrong_source_or_actor_and_missing_outputs_scans_and_current_edges(self):
        changes=[lambda c:c.success.update(triggered_by=True),lambda c:setattr(c,'scan_actor',99),lambda c:setattr(c,'scan_tenant','2'),
            lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(locator=c.locators[2]),
            lambda c:c.success['outputs'].pop('save_details'),lambda c:c.success['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),
            lambda c:c.success['metadata']['result']['meta_scan_runs'].pop(),lambda c:c.outputs[0].update(size_bytes=0),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'][0]['evidence'].update(execution_id='old'))]
        for change in changes:
            c=TrimClient();change(c)
            with patch.object(m.raster,'wait_lineage',return_value={}):
                with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(c)
        c=TrimClient();c.success['metadata']['lineage_facts']['inputs'].reverse();self.run_case(c)
    def test_trim_rejects_failure_publication_wrong_tuple_private_data_and_incomplete_browser(self):
        changes=[lambda v:v.update(outputs={'save_result':{}}),lambda v:v['metadata'].update(result={'meta_scan_runs':[{}]}),
            lambda v:v['error_details'].update(diagnostic={'reason':'wrong'}),lambda v:v['error_details'].update(message='source_shape PRIVATE_DIAGNOSTIC_NODE')]
        for change in changes:
            c=TrimClient();c.mutate_failed=change
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(c)
        c=TrimClient();c.failed_graph=lambda g:g['edges'].append({'relation_kind':'derive','evidence':{'execution_id':'failed-limit'}})
        with self.assertRaises(m.SuiteError):self.run_case(c)
        with self.assertRaisesRegex(m.SuiteError,'broken browser'):self.run_case(browser=lambda *a:(_ for _ in ()).throw(m.SuiteError('broken browser')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-trim-checkpoint.json').read_text())
        self.assertEqual((checkpoint['result'],checkpoint['stage'],len(checkpoint['failures'])),('incomplete','before_browser',8))
        with self.assertRaises(m.SuiteError):self.run_case(diagnostic=lambda *a,**k:{'execution_id':'other'})

    def test_trim_raw_evidence_requires_final_maps_pixels_both_owner_pages_and_exact_identity(self):
        report=self.run_case();e=report['browser'];c=TrimClient()
        raw={'schema_version':m.fixture.TRIM_SCHEMA+'-browser-raw/v1','case':e['case'],'run_id':e['run_id'],'execution_id':e['execution_id'],
            'result':'passed','stage':'complete','execution':c.success,'observations':[],'failures':[]}
        for output in e['outputs']:
            raw['observations'].append({'port':output['port'],'output_item_id':output['item_id'],'output':output['before'],
                'sources':[{'item_id':s['id'],'item':s['before']} for s in e['sources']],
                'graph':report['raw_lineage'][output['port']],'removed_segment_pixels':0,
                'rendered':{'projection':'EPSG:3857','features':[{}]*len(e['expected'][output['port']]),'painted_pixels':50,'new_segment_pixels':3}})
        for failure in report['failures']:
            raw['failures'].append({'key':failure['key'],'execution_id':failure['execution_id'],'execution':failure['execution'],
                'monitor':failure['monitor'],'monitor_page':{'execution':failure['monitor']}})
        m.validate_trim_browser_raw(raw,e)
        changes=[lambda v:v.update(stage='before_browser'),lambda v:v.update(execution_id='old'),lambda v:v['observations'].pop(),
            lambda v:v['observations'][0].update(removed_segment_pixels=1),lambda v:v['observations'][0]['rendered'].update(painted_pixels=0),
            lambda v:v['observations'][0]['graph']['edges'][0]['source'].update(item_id=12),lambda v:v['failures'].pop(),
            lambda v:v['failures'][0]['monitor_page']['execution'].update(triggered_by=True),
            lambda v:v['failures'][0]['execution']['error_details']['diagnostic']['operator'].update(node_index=True)]
        for change in changes:
            value=copy.deepcopy(raw);change(value)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):m.validate_trim_browser_raw(value,e)

    def test_trim_removed_segment_guard_rejects_old_pixels_and_offscreen_witness(self):
        import subprocess
        script=r"""
import assert from 'node:assert/strict';import{readFileSync}from'node:fs';
const source=readFileSync(process.argv[1],'utf8');const a=source.indexOf('async function assertTrimRemovedSegment('),b=source.indexOf('async function assertTrimFailurePanel(',a);
const expect={poll:fn=>({async toBe(value){assert.equal(await fn(),value)}})};
const check=new Function('expect',source.slice(a,b)+';return assertTrimRemovedSegment')(expect);
async function run(old=false,offscreen=false){
 const data=new Uint8ClampedArray(7*7*4);if(old)data.set([0,200,0,255]);
 const canvas={width:100,height:100,getBoundingClientRect:()=>({left:0,top:0,width:100,height:100})};
 const layer={getVisible:()=>true,getOpacity:()=>1,getSource:()=>({getFeatures:()=>[{}]}),getRenderer:()=>({context:{canvas,getImageData:()=>({data})}})};
 const map={getPixelFromCoordinate:()=>offscreen?[-1,50]:[50,50],getViewport:()=>({getBoundingClientRect:()=>({left:0,top:0})}),getLayers:()=>({getArray:()=>[layer]})};
 return check({evaluate:async(fn)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}})});
}
assert.equal(await run(),0);await assert.rejects(()=>run(true));await assert.rejects(()=>run(false,true));
"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(Path(__file__).resolve().parents[2]/'console/frontend/e2e/online/vector-topology-persistence.spec.js')],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)


def nodes_physical(*args):
    return {'exact_inventory_verified':True,'sources':{n:{'size_bytes':98304,'sha256':n} for n in m.fixture.NODES_SOURCES},
        'targets':{name:{'size_bytes':98304,'sha256':name,'feature_count':len(m.fixture.nodes_features(port)),'layer_name':name[:-5],
            'geometry_column':'geom','fid_column':'fid','features':m.fixture.nodes_features(port)}
            for port,name in zip(m.fixture.NODES_PORTS,m.fixture.NODES_TARGETS)}}


class NodesClient(InputDiagnosticClient):
    def __init__(self):
        super().__init__();self.sources=[];self.outputs=[];self.mutate_failed=None;self.failed_graph=None;self.mutate_graph=None;self.scan_actor=33;self.scan_tenant=2
        for i,(name,kind) in enumerate(zip(m.fixture.NODES_SOURCES,m.fixture.NODES_KINDS)):
            rows=m.fixture.nodes_features(kind);item=base.item(name,10+i);item['full_name']=name
            item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.sources.append(item)
        for i,(name,port) in enumerate(zip(m.fixture.NODES_TARGETS,m.fixture.NODES_PORTS)):
            rows=m.fixture.nodes_features(port);item=base.item(name,30+i)
            item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.outputs.append(item)
        self.scans=['scan-'+n for n in m.fixture.NODES_SOURCES]
        self.locators=[f'addp://engine/3/path/{n}?type=file&item_id={10+i}' for i,n in enumerate(m.fixture.NODES_SOURCES)]
        self.parent='addp://engine/3/path/outputs?type=directory'
        targets={p:f'addp://engine/3/path/outputs/{n}?type=file' for p,n in zip(m.fixture.NODES_PORTS,m.fixture.NODES_TARGETS)}
        self.success={'execution_id':'nodes-run','status':'success','triggered_by':33,
            'outputs':{'save_'+p:{'resource':{'locator':t,'type':'file','write_mode':'create'}} for p,t in targets.items()},
            'execution_config':{'content':{'workflow_definition':m.nodes_definition(self.locators,self.parent)}},
            'metadata':{'lineage_facts':{'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.load_source','locator':self.locators[0]}],
              'outputs':[{'locator':t,'write_mode':'create'} for t in targets.values()]},'result':{'meta_scan_runs':[{'execution_id':'output-scan-'+p,'target_locator':t,'status':'submitted'} for p,t in targets.items()]}}}
    def request(self,method,path,expected,body=None):
        if method=='POST' and path.endswith('/develop/executions'):
            self.calls.append((method,path,body));definition=body['content']['workflow_definition'];name=definition['tasks'][-1]['params']['target_name']
            if name.startswith('failed-'):
                spec=next(v for v in m.fixture.NODES_FAILURES if name.startswith('failed-'+v[0]+'-'))
                value={'execution_id':'failed-'+spec[0],'status':'failed','triggered_by':33,'outputs':{},'metadata':{},
                    'execution_config':{'content':{'workflow_definition':definition}},'error_details':{'code':'develop.workflow.invalid_params','message':'controlled','diagnostic':m.nodes_diagnostic(spec)}}
                if self.mutate_failed:self.mutate_failed(value)
            else:value=self.success;self.scans.extend(['output-scan-result','output-scan-members'])
            self.executions[value['execution_id']]=value;data={'execution_id':value['execution_id']}
        elif path.endswith('/engines/3/items'):data=[*self.sources,*self.outputs]
        elif path.startswith('/api/v1/meta/items/'):
            item_id=int(path.rsplit('/',1)[1]);data=next(v for v in [*self.sources,*self.outputs] if v['id']==item_id)
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':self.scan_tenant,'triggered_by':self.scan_actor}
        elif path.startswith('/api/v1/meta/lineage/graph?'):
            if 'direction=downstream' in path:
                data={'truncated':False,'nodes':[],'edges':[]}
                if self.failed_graph:self.failed_graph(data)
            else:
                target=int(path.split('item_id=')[1].split('&')[0]);data={'truncated':False,'nodes':[],'edges':[{'source':{'item_id':10},'target':{'item_id':target},'relation_kind':'derive','status':'active','evidence':{'execution_id':'nodes-run'}}]}
                if self.mutate_graph:self.mutate_graph(data)
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


class NodesOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.NODES_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=nodes_physical,browser=lambda *a:a[2]):
        return m.run_scenario(Path('.'),self.env,client or NodesClient(),physical,browser,diagnostic_runner=lambda *a,**k:self.fail('no private log oracle'))
    def test_nodes_lineage_permissions_requires_exported_reader_before_any_scan(self):
        self.env.update(ADDP_ONLINE_TOPOLOGY_LINEAGE_PERMISSIONS='1', ADDP_ONLINE_HOST='1',
                        ADDP_ONLINE_HOSTED='1', GITHUB_ACTIONS='true', RUNNER_OS='Linux')
        self.env.pop('ADDP_ONLINE_READ_USER_ACCESS_TOKEN', None)
        client = NodesClient()
        with self.assertRaisesRegex(m.SuiteError, 'fixture missing ADDP_ONLINE_READ_USER_ACCESS_TOKEN'):
            self.run_case(client)
        self.assertEqual(client.calls, [])

    def test_nodes_one_success_nine_failures_two_point_scans_and_exact_source_edges(self):
        c=NodesClient();report=self.run_case(c)
        self.assertEqual(report['result'],'passed');self.assertEqual(report['schema_version'],'addp.line-nodes/v1')
        self.assertEqual([len(report['browser']['expected'][k]) for k in m.fixture.NODES_PORTS],[15,22])
        self.assertEqual([len(report[k]) for k in ['initial_scan_inventory','final_scan_inventory','meta_after']],[10,12,12])
        self.assertEqual(sum(len(g['edges']) for g in report['raw_lineage'].values()),2)
        self.assertEqual([f['key'] for f in report['failures']],[s[0] for s in m.fixture.NODES_FAILURES])
        self.assertEqual(len({f['execution_id'] for f in report['failures']}),9)
        self.assertEqual(sum(method=='POST' and path.endswith('/develop/executions') for method,path,_ in c.calls),10)
    def test_nodes_rejects_output_scan_identity_boolean_size_port_and_stale_extra_graph(self):
        changes=[lambda c:c.success['metadata']['lineage_facts']['inputs'].append(copy.deepcopy(c.success['metadata']['lineage_facts']['inputs'][0])),
            lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(port='input.other'),
            lambda c:c.success['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),
            lambda c:c.success['metadata']['result']['meta_scan_runs'][0].update(execution_id=''),
            lambda c:setattr(c,'scan_actor',True),lambda c:setattr(c,'scan_tenant','2'),lambda c:c.outputs[0].update(size_bytes=True),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'].append(copy.deepcopy(g['edges'][0])))]
        for change in changes:
            c=NodesClient();change(c)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(c)
    def test_nodes_rejects_stale_graph_directly_without_waiting_or_budget_change(self):
        for mutation in [lambda g:g['edges'][0]['evidence'].update(execution_id='old'),lambda g:g['edges'].clear(),lambda g:g.update(truncated=True)]:
            graph={'truncated':False,'edges':[{'source':{'item_id':10},'target':{'item_id':30},'relation_kind':'derive','status':'active','evidence':{'execution_id':'current'}}]}
            mutation(graph)
            with self.subTest(mutation=mutation),self.assertRaises(m.SuiteError):m.validate_nodes_graph(graph,[{'id':10}],30,'current')
    def test_nodes_failures_reject_publication_scan_derive_private_or_wrong_tuple(self):
        for mutation in [lambda v:v.update(outputs={'save':{}}),lambda v:v['metadata'].update(lineage_facts={'outputs':[{}]}),
            lambda v:v['metadata'].update(result={'meta_scan_runs':[{}]}),lambda v:v['error_details']['diagnostic']['operator'].update(node_index=True),
            lambda v:v['error_details']['diagnostic']['operator'].update(field='PRIVATE'),lambda v:v['error_details'].update(message='route_key')]:
            c=NodesClient();c.mutate_failed=mutation
            with self.subTest(mutation=mutation),self.assertRaises(m.SuiteError):self.run_case(c)
        c=NodesClient();c.failed_graph=lambda g:g['edges'].append({'relation_kind':'derive','evidence':{'execution_id':'failed-reverse'}})
        with self.assertRaises(m.SuiteError):self.run_case(c)
    def test_nodes_browser_failure_preserves_incomplete_and_never_final_pass(self):
        with self.assertRaisesRegex(m.SuiteError,'browser broke'):self.run_case(browser=lambda *a:(_ for _ in ()).throw(m.SuiteError('browser broke')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-nodes-checkpoint.json').read_text())
        self.assertEqual((checkpoint['result'],checkpoint['stage']),('incomplete','before_browser'));self.assertEqual(len(checkpoint['failures']),9)
    def test_nodes_browser_cannot_change_files_scans_or_meta(self):
        calls=[]
        def physical(*args):
            native=nodes_physical();calls.append(1)
            if len(calls)>10:native['targets']['nodes.gpkg']['sha256']='changed'
            return native
        with self.assertRaisesRegex(m.SuiteError,'physical files'):self.run_case(physical=physical)
        c=NodesClient()
        def browser(*args):c.sources[0]['size_bytes']=1;return args[2]
        with self.assertRaisesRegex(m.SuiteError,'Meta changed'):self.run_case(c,browser=browser)
    def test_nodes_raw_browser_rejects_point_incidence_ewkb_and_owner_locale_forgery(self):
        import base64,struct
        report=self.run_case();e=report['browser'];c=NodesClient()
        raw={'schema_version':m.fixture.NODES_SCHEMA+'-browser-raw/v1','result':'passed','stage':'complete',**{k:e[k] for k in ('case','run_id','execution_id')},'execution':c.success,'observations':[],'failures':[]}
        for output in e['outputs']:
            expected=e['expected'][output['port']];fields=list(expected[0]['properties']);rows=[]
            for i,row in enumerate(expected,1):
                wire=base64.b64encode(struct.pack('<BIIdd',1,0x20000001,3857,*row['geometry']['coordinates'])).decode()
                rows.append({output['fid_column']:i,output['geometry_column']:wire,**row['properties']})
            graph=report['raw_lineage'][output['port']]
            raw['observations'].append({'port':output['port'],'output_item_id':output['item_id'],'sources':[{'item_id':s['id'],'item':s['before']} for s in e['sources']],
                'output':output['before'],'graph':graph,'lineage_rendered':{'edges':graph['edges']},
                'preview':{'preview_type':'table','data':{'total':len(rows),'source_srid':3857,'source_crs':'EPSG:3857','transform_status':'not_transformed','preview_hint':'frontend_transform_required',
                    'columns':[output['fid_column'],output['geometry_column'],*fields],'geometry_columns':[output['geometry_column']],'rows':rows}},
                'rendered':{'projection':'EPSG:3857','painted_pixels':31,'features':[{'type':'Point','coordinates':r['geometry']['coordinates'],'properties':r['properties']} for r in expected]}})
        for failed in report['failures']:
            raw['failures'].append({'key':failed['key'],'execution_id':failed['execution_id'],'observations':[{'owner':owner,'locale':locale,'execution':failed['execution'] if owner=='develop' else {'execution':failed['monitor']},'clipboard':'controlled'} for owner in ('develop','monitor') for locale in ('zh-cn','en')]})
        m.validate_nodes_browser_raw(raw,e)
        changes=[lambda v:v.update(stage='incomplete'),lambda v:v['observations'][1]['preview']['data']['rows'].pop(),
            lambda v:v['observations'][0]['preview']['data']['rows'][0].update(degree=True),lambda v:v['observations'][0]['preview']['data']['rows'][0].update(geom='R1A='),
            lambda v:v['observations'][1]['rendered']['features'].pop(),lambda v:v['observations'][0]['rendered'].update(painted_pixels=0),
            lambda v:v['failures'][0]['observations'].pop(),lambda v:v['failures'][0]['observations'][0].update(locale='other')]
        for mutate in changes:
            v=copy.deepcopy(raw);mutate(v)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):m.validate_nodes_browser_raw(v,e)


class NodesBrowserContractTest(unittest.TestCase):
    def test_nodes_bilingual_panel_and_exact_clipboard_use_current_shared_diagnostic(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r"""
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'
const root=process.argv[1],source=readFileSync(root+'/console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8')
const start=source.indexOf('function nodesPanelExpectation('),end=source.indexOf('async function verifyNodesCase(',start)
const expect=v=>({toBe:expected=>assert.equal(v,expected)})
const expectedFor=new Function('expect',source.slice(start,end)+';return nodesPanelExpectation')(expect)
const { executionFailurePresentation }=await import(pathToFileURL(root+'/common-frontend/basic/src/utils/executionFailure.mjs'))
for(const locale of ['zh-cn','en']){
 const messages=JSON.parse(readFileSync(root+'/common-frontend/basic/src/i18n/'+locale+'.json','utf8'))
 const t=(key,args={})=>key.split('.').reduce((v,k)=>v[k],messages).replace(/\{(\w+)\}/g,(_,k)=>args[k])
 for(const [reason,field] of [['operator_endpoint_network_invalid',''],['operator_field_conflict','node_id'],['operator_field_conflict','degree'],['operator_input_invalid',''],['operator_projected_crs_required','']]){
  const operator={node_index:1,operator_id:'extract_line_nodes',...(reason==='operator_projected_crs_required'?{}:{parameter:'input_gdf'}),...(field?{field}:{})}
  const failure={execution_id:'same-current-uuid',expected_diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator}}
  const actual=executionFailurePresentation({code:'develop.workflow.invalid_params',diagnostic:failure.expected_diagnostic},t)
  const expected=expectedFor(failure,locale)
  assert.equal(expected.copyButton,t('common.executionFailure.copy'))
  assert.ok(source.includes("panel.getByRole('button',{name:expected.copyButton,exact:true}).click()"))
  assert.deepEqual(expected.values,[actual.reason,actual.stage,actual.operator,actual.node,actual.parameter,actual.field,actual.action,actual.code,failure.execution_id].filter(Boolean))
  assert.equal(expected.clipboard,expected.values.join('\n'))
 }
}
assert.ok(source.includes("if(output.port==='members')expect([data.page,data.page_size,data.rows.length]).toEqual([1,50,22])"))
assert.ok(source.includes('observation.rendered=await assertProximityPointMap(renderer,expected)'))
assert.ok(source.includes('topology-nodes-browser-raw.json'))
"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repository)],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)


def mixed_physical(case):
    profile=m.mixed_overlay_profile(case);name=profile['target']
    return {'exact_inventory_verified':True,'sources':{n:{'size_bytes':98304,'sha256':n}for n in profile['sources']},
        'source_tables':{n:{'features':profile['features'](k)}for n,k in zip(profile['sources'],profile['kinds'])},
        'targets':{name:{'size_bytes':98304,'sha256':name,'feature_count':profile['result_count'],'layer_name':name[:-5],
            'geometry_column':'geom','fid_column':'fid','features':profile['features']('result')}}}


def overlay_physical(*args):return mixed_physical(m.fixture.OVERLAY_CASE)
def difference_physical(*args):return mixed_physical(m.fixture.DIFFERENCE_CASE)


class OverlayClient(InputDiagnosticClient):
    def __init__(self,case=None):
        super().__init__();self.case=case or m.fixture.OVERLAY_CASE;self.profile=m.mixed_overlay_profile(self.case);self.run_id='overlay-run'if self.case==m.fixture.OVERLAY_CASE else'difference-run';self.sources=[];self.outputs=[];self.mutate_failed=None;self.mutate_graph=None;self.failed_graph=None
        self.preflight_denied=False;self.scan_actor=33;self.scan_tenant=2
        for i,(name,kind) in enumerate(zip(self.profile['sources'],self.profile['kinds'])):
            rows=self.profile['features'](kind);item=base.item(name,10+i);item.update(full_name=name,size_bytes=98304)
            item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.sources.append(item)
        rows=self.profile['features']('result');item=base.item(self.profile['target'],30);item['size_bytes']=98304
        item['attributes']['type_info']['container']['children'][0].update(row_count=self.profile['result_count'],column_count=len(rows[0]['properties'])+2);self.outputs=[item]
        self.scans=['scan-'+n for n in self.profile['sources']]
        self.locators=[f'addp://engine/3/path/{n}?type=file&item_id={10+i}' for i,n in enumerate(self.profile['sources'])]
        self.parent='addp://engine/3/path/outputs?type=directory';target=f"addp://engine/3/path/outputs/{self.profile['target']}?type=file"
        self.success={'execution_id':self.run_id,'status':'success','triggered_by':33,
            'outputs':{'save_result':{'resource':{'locator':target,'type':'file','write_mode':'create'}}},
            'execution_config':{'content':{'workflow_definition':self.profile['definition'](self.locators,self.parent)}},
            'metadata':{'lineage_facts':{'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.'+node,'locator':self.locators[i]}for i,node in reversed(list(enumerate(self.profile.get('input_nodes',('load_a','load_b')))))],
              'outputs':[{'locator':target,'write_mode':'create'}]},'result':{'meta_scan_runs':[{'execution_id':'overlay-output-scan','target_locator':target,'status':'submitted'}]}}}
    def request(self,method,path,expected,body=None):
        if method=='POST' and path.endswith('/develop/executions'):
            self.calls.append((method,path,body));definition=body['content']['workflow_definition']
            spec = next((entry for entry in self.profile['failures']
                         if definition == self.profile['definition'](self.locators, self.parent, entry[0])), None)
            if spec is not None:
                value={'execution_id':('overlay-failed-'if self.case==m.fixture.OVERLAY_CASE else'difference-failed-')+spec[0],'status':'failed','triggered_by':33,'outputs':{},'metadata':{},
                    'execution_config':{'content':{'workflow_definition':definition}},'error_details':{'code':'develop.workflow.invalid_params','message':'controlled','diagnostic':self.profile['diagnostic'](spec)}}
                if self.mutate_failed:self.mutate_failed(value)
            else:value=self.success;self.scans.append('overlay-output-scan')
            self.executions[value['execution_id']]=value;data={'execution_id':value['execution_id']}
        elif path.endswith('/engines/3/items'):data=[*self.sources,*self.outputs]
        elif path.startswith('/api/v1/meta/items/'):
            data=next(v for v in [*self.sources,*self.outputs] if v['id']==int(path.rsplit('/',1)[1]))
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':self.scan_tenant,'triggered_by':self.scan_actor}
        elif path.startswith('/api/v1/meta/lineage/graph?'):
            if 'direction=downstream' in path:
                if self.preflight_denied:raise m.SuiteError('resource authorization unavailable')
                data={'truncated':False,'nodes':[],'edges':[]}
                if self.failed_graph:self.failed_graph(data)
            else:
                data={'truncated':False,'nodes':[],'edges':[{'source':{'item_id':i},'target':{'item_id':30},'relation_kind':'derive','status':'active','evidence':{'execution_id':self.run_id}} for i in range(10,10+len(self.profile.get('input_nodes',('load_a','load_b'))))]}
                if self.mutate_graph:self.mutate_graph(data)
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


class OverlayOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.OVERLAY_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=overlay_physical,browser=lambda *a:a[2]):
        return m.run_scenario(Path('.'),self.env,client or OverlayClient(),physical,browser)
    def test_overlay_one_success_four_failures_dual_source_single_scan_and_original_facts(self):
        client=OverlayClient();report=self.run_case(client)
        self.assertEqual((report['result'],report['schema_version']),('passed','addp.intersection-overlay/v1'))
        self.assertEqual(len(report['browser']['expected']['result']),16)
        self.assertEqual([len(report[k]) for k in ('initial_scan_inventory','final_scan_inventory','meta_after')],[4,5,5])
        self.assertEqual(len(report['raw_lineage']['edges']),2)
        self.assertEqual([v['key'] for v in report['failures']],[v[0] for v in m.fixture.OVERLAY_FAILURES])
        self.assertEqual(len({v['execution_id'] for v in report['failures']}),4)
        self.assertEqual(sum(method=='POST' and path.endswith('/develop/executions') for method,path,_ in client.calls),5)
    def test_overlay_requires_formal_resource_visibility_before_any_business_execution(self):
        client=OverlayClient();client.preflight_denied=True
        with self.assertRaisesRegex(m.SuiteError,'resource authorization'):self.run_case(client)
        self.assertFalse(client.executions)
        self.assertFalse(any(method=='POST' and path.endswith('/develop/executions') for method,path,_ in client.calls))
    def test_overlay_rejects_wrong_actor_size_port_locator_write_mode_and_current_graph(self):
        changes=[lambda c:c.success['metadata']['lineage_facts']['inputs'].append(copy.deepcopy(c.success['metadata']['lineage_facts']['inputs'][0])),
            lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(port='input.OTHER'),
            lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(locator=c.locators[0]),
            lambda c:c.success['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),
            lambda c:c.success['outputs']['save_result']['resource'].update(write_mode='fail'),
            lambda c:setattr(c,'scan_actor',True),lambda c:setattr(c,'scan_tenant','2'),lambda c:c.outputs[0].update(size_bytes=True),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'].append(copy.deepcopy(g['edges'][0])))]
        for change in changes:
            client=OverlayClient();change(client)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_overlay_failures_reject_publication_scan_wrong_node_private_and_derive(self):
        changes=[lambda v:v.update(outputs={'save':{}}),lambda v:v['metadata'].update(lineage_facts={'outputs':[{}]}),
            lambda v:v['metadata'].update(result={'meta_scan_runs':[{}]}),lambda v:v['error_details']['diagnostic']['operator'].update(node_index=True),
            lambda v:v['error_details']['diagnostic']['operator'].update(field='PRIVATE'),lambda v:v['error_details']['diagnostic']['operator'].update(parameter='id_field'),
            lambda v:v['error_details'].update(message='PRIVATE_OVERLAY_SOURCE')]
        for change in changes:
            client=OverlayClient();client.mutate_failed=change
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(client)
        client=OverlayClient();client.failed_graph=lambda g:g['edges'].append({'relation_kind':'derive','evidence':{'execution_id':'overlay-failed-a-id'}})
        with self.assertRaises(m.SuiteError):self.run_case(client)
    def test_overlay_dual_graph_never_accepts_boolean_dataitem_identity(self):
        graph={'truncated':False,'edges':[{'source':{'item_id':True},'target':{'item_id':3},'relation_kind':'derive','status':'active','evidence':{'execution_id':'current'}},
            {'source':{'item_id':2},'target':{'item_id':3},'relation_kind':'derive','status':'active','evidence':{'execution_id':'current'}}]}
        with self.assertRaises(m.SuiteError):m.validate_proximity_graph(graph,[{'id':1},{'id':2}],3,'current')

    def test_overlay_browser_failure_retains_incomplete_and_cannot_change_bytes_scans_meta(self):
        with self.assertRaisesRegex(m.SuiteError,'browser broke'):self.run_case(browser=lambda *a:(_ for _ in ()).throw(m.SuiteError('browser broke')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-overlay-checkpoint.json').read_text())
        self.assertEqual((checkpoint['result'],checkpoint['stage']),('incomplete','before_browser'));self.assertEqual(len(checkpoint['failures']),4)
        calls=[]
        def physical(*args):
            native=overlay_physical();calls.append(1)
            if len(calls)>5:native['targets'][m.fixture.OVERLAY_TARGET]['sha256']='changed'
            return native
        with self.assertRaisesRegex(m.SuiteError,'physical files'):self.run_case(physical=physical)
        client=OverlayClient()
        def browser(*args):client.sources[0]['size_bytes']=1;return args[2]
        with self.assertRaisesRegex(m.SuiteError,'Meta changed'):self.run_case(client,browser=browser)


def overlay_wire_and_projected(expected):
    import subprocess
    repository=Path(__file__).resolve().parents[2]
    script="""import WKB from 'ol/format/WKB.js';import GeoJSON from 'ol/format/GeoJSON.js';import{webMercatorPreviewGeometry}from'./e2e/geometry-preview-coordinate.js';
let text='';for await(const part of process.stdin)text+=part;const rows=JSON.parse(text),format=new WKB({hex:false,srid:3857}),geojson=new GeoJSON();
console.log(JSON.stringify({wire:rows.map(row=>Buffer.from(format.writeGeometry(geojson.readGeometry(row.geometry))).toString('base64')),
projected:rows.map(row=>({properties:row.properties,geometry:webMercatorPreviewGeometry(row.geometry)}))}));"""
    result=subprocess.run(['node','--input-type=module','-e',script],cwd=repository/'manager/frontend',input=json.dumps(expected),capture_output=True,text=True,timeout=30)
    if result.returncode:raise AssertionError(result.stdout+result.stderr)
    return json.loads(result.stdout)


class OverlayBrowserRawTest(unittest.TestCase):
    def setUp(self):
        self.owner=OverlayOnlineTest();self.owner.setUp();self.addCleanup(self.owner.doCleanups)
        self.report=self.owner.run_case();self.evidence=self.report['browser'];e=self.evidence;c=OverlayClient();output=e['outputs'][0]
        expected=e['expected']['result'];encoded=overlay_wire_and_projected(expected)
        rows=[{**r['properties'],'fid':i+1,'geom':wire} for i,(r,wire) in enumerate(zip(expected,encoded['wire']))]
        graph=self.report['raw_lineage']
        observation={'port':'result','output_item_id':30,'sources':[{'item_id':s['id'],'item':s['before']} for s in e['sources']],
            'output':output['before'],'graph':graph,'lineage_page':graph,'lineage_rendered':graph,
            'lineage_canvas':{'rows':[{'text':s['name']} for s in [*e['sources'][:2],output]],'paths':[[[0,0],[1,1]],[[1,0],[2,1]]]},
            'preview':{'preview_type':'table','data':{'page':1,'total':16,'source_srid':3857,'source_crs':'EPSG:3857','transform_status':'not_transformed','preview_hint':'frontend_transform_required',
                'geometry_columns':['geom'],'columns':['fid','geom',*expected[0]['properties']],'rows':rows}},'decoded_rows':expected,
            'rendered':{'projection':'EPSG:3857','features':encoded['projected'],'collection_pixels':{'point':31,'line':32,'polygon':33,'members':[{'kind':{'Point':'point','LineString':'line','Polygon':'polygon'}[v['type']],'pixels':31}for v in next(row['geometry']['geometries']for row in expected if row['geometry']['type']=='GeometryCollection')]}}}
        self.raw={'schema_version':'addp.intersection-overlay-browser-raw/v1','case':e['case'],'run_id':e['run_id'],'execution_id':e['execution_id'],
            'result':'passed','stage':'complete','execution':c.success,'observations':[observation],'publication_requests':[],'failures':[]}
        for failed in self.report['failures']:
            self.raw['failures'].append({'key':failed['key'],'execution_id':failed['execution_id'],
                'observations':[{'owner':owner,'locale':locale,'raw':failed['execution'] if owner=='develop' else {'execution':failed['monitor']},'clipboard':'safe controlled copy'} for owner in ('develop','monitor') for locale in ('zh-cn','en')]})
        self.raw['failures']=frozen_literal_clipboards(self.raw['failures'])
    def test_overlay_raw_accepts_exact_mixed_ewkb_projected_members_and_complete_owner_pages(self):
        m.validate_overlay_browser_raw(self.raw,self.evidence)
    def test_overlay_raw_refuses_missing_member_coordinate_type_pixels_graph_page_or_identity(self):
        changes=[lambda r:r.update(result='incomplete'),lambda r:r.update(execution_id='other'),
            lambda r:r['observations'][0]['preview']['data']['rows'][0].update(a_enabled=1),
            lambda r:r['observations'][0]['preview']['data']['rows'][0].update(geom='YWJj'),
            lambda r:r['observations'][0]['decoded_rows'][0]['geometry'].update(coordinates=[99,99]),
            lambda r:r['observations'][0]['rendered']['features'].pop(),
            lambda r:r['observations'][0]['rendered']['features'][13]['geometry']['geometries'].pop(),
            lambda r:r['observations'][0]['rendered']['features'][0]['geometry'].update(coordinates=[99,99]),
            lambda r:r['observations'][0]['rendered']['collection_pixels'].update(point=0),lambda r:r['observations'][0]['rendered']['collection_pixels']['members'].pop(),
            lambda r:r['observations'][0]['graph']['edges'].pop(),
            lambda r:r['failures'][0]['observations'].pop(),
            lambda r:r['failures'][0]['observations'][0].update(clipboard='PRIVATE_OVERLAY_SOURCE')]
        for change in changes:
            raw=copy.deepcopy(self.raw);change(raw)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):m.validate_overlay_browser_raw(raw,self.evidence)
    def test_overlay_raw_refuses_wire_that_disagrees_with_claimed_decoded_rows(self):
        raw=copy.deepcopy(self.raw);raw['observations'][0]['preview']['data']['rows'][0]['geom']=raw['observations'][0]['preview']['data']['rows'][2]['geom']
        with self.assertRaises(m.SuiteError):m.validate_overlay_browser_raw(raw,self.evidence)


class OverlayBrowserContractTest(unittest.TestCase):
    def test_overlay_actual_map_helper_rejects_lost_collection_members_wrong_coordinates_and_attributes(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        expected=m.fixture.overlay_features('result')
        script="""import assert from 'node:assert/strict';import{readFileSync}from'node:fs';import{pathToFileURL}from'node:url';
const root=process.argv[1],source=readFileSync(root+'/console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8');
const start=source.indexOf('async function assertOverlayMap('),end=source.indexOf('async function assertOverlayCollectionPixels(',start);
const expect=value=>({toHaveLength:n=>assert.equal(value.length,n)});expect.poll=fn=>({toEqual:async expected=>assert.deepEqual(await fn(),expected)});
const{webMercatorPreviewGeometry}=await import(pathToFileURL(root+'/manager/frontend/e2e/geometry-preview-coordinate.js'));
const check=new Function('expect','webMercatorPreviewGeometry',source.slice(start,end)+';return assertOverlayMap')(expect,webMercatorPreviewGeometry);
let text='';for await(const part of process.stdin)text+=part;const expected=JSON.parse(text);
const native={projection:'EPSG:3857',features:expected.map(row=>({properties:row.properties,geometry:webMercatorPreviewGeometry(row.geometry)}))};
await check({evaluate:async()=>native},expected);
for(const change of [v=>v.features.pop(),v=>v.features[13].geometry.geometries.pop(),v=>v.features[0].geometry.coordinates[0]+=1,
v=>v.features[0].properties.a_enabled=1,v=>v.features[0].properties.geometry='wrong',v=>v.projection='EPSG:4326']){
 const changed=structuredClone(native);change(changed);await assert.rejects(check({evaluate:async()=>changed},expected));}
assert.ok(source.includes('collection_pixels = await assertOverlayCollectionPixels(renderer)'));
assert.ok(source.includes("candidate === feature ? true : undefined"));
console.log('mixed geometry, exact projection and original property rejection verified');"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repository)],input=json.dumps(expected),capture_output=True,text=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_overlay_reuses_authenticated_browser_navigation_for_failure_pages(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script="""import assert from 'node:assert/strict';import{readFileSync}from'node:fs';
const source=readFileSync(process.argv[1]+'/console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8');
const start=source.indexOf('async function verifyInputDiagnosticCase('),end=source.indexOf("test('算子参数诊断独立普通User双语失败页'",start);
const expect=value=>({toBe:other=>assert.equal(value,other),toEqual:other=>assert.deepEqual(value,other)}),stop=new Error('first navigation');
let calls;const login=async()=>{calls.login++;throw stop};
const check=new Function('expect','login',source.slice(start,end)+';return verifyInputDiagnosticCase')(expect,login);
const evidence={case:'intersection-overlay',executions:['a-id','b-id','crs','fields'].map((key,i)=>({key,execution_id:String(i)})),permissions_verified:[]};
const page={context:()=>({grantPermissions:async()=>{}}),on(){},waitForResponse:()=>Promise.resolve({}),goto:async()=>{calls.goto++;throw stop}};
calls={login:0,goto:0};await assert.rejects(check(page,evidence.case,evidence),error=>error===stop);assert.deepEqual(calls,{login:0,goto:1});
process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE=JSON.stringify(evidence);calls={login:0,goto:0};await assert.rejects(check(page,evidence.case),error=>error===stop);assert.deepEqual(calls,{login:1,goto:0});"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repository)],capture_output=True,text=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)


def dissolve_physical(count=4):
    return {'exact_inventory_verified':True,'sources':{n:{'size_bytes':98304,'sha256':n}for n in m.fixture.DISSOLVE_SOURCES},
        'source_tables':{n:{'features':m.fixture.dissolve_features(k)}for n,k in zip(m.fixture.DISSOLVE_SOURCES,m.fixture.DISSOLVE_KINDS)},
        'targets':{n:{'size_bytes':98304,'sha256':n,'feature_count':len(m.fixture.dissolve_features(n[:-5])),'layer_name':n[:-5],
            'geometry_column':'parcel_shape','fid_column':'fid','features':m.fixture.dissolve_features(n[:-5])}for n in m.fixture.DISSOLVE_TARGETS[:count]}}


class DissolveClient(InputDiagnosticClient):
    def __init__(self):
        super().__init__();self.sources=[];self.outputs=[];self.preflight_denied=False;self.success_count=0;self.mutate_success=None;self.mutate_failed=None;self.mutate_graph=None;self.failed_graph=None;self.scan_actor=33;self.scan_tenant=2
        for i,(name,kind)in enumerate(zip(m.fixture.DISSOLVE_SOURCES,m.fixture.DISSOLVE_KINDS)):
            rows=m.fixture.dissolve_features(kind);item=base.item(name,10+i);item.update(full_name=name,size_bytes=98304);item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.sources.append(item)
        for i,name in enumerate(m.fixture.DISSOLVE_TARGETS):
            rows=m.fixture.dissolve_features(name[:-5]);item=base.item(name,30+i);item['size_bytes']=98304;item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.outputs.append(item)
        self.scans=['scan-'+n for n in m.fixture.DISSOLVE_SOURCES];self.locators=[f'addp://engine/3/path/{n}?type=file&item_id={10+i}'for i,n in enumerate(m.fixture.DISSOLVE_SOURCES)];self.parent='addp://engine/3/path/outputs?type=directory'
    def request(self,method,path,expected,body=None):
        if method=='POST'and path.endswith('/develop/executions'):
            self.calls.append((method,path,body));definition=body['content']['workflow_definition'];name=definition['tasks'][-1]['params']['target_name']
            if name.startswith('failed-'):
                spec=next(v for v in m.fixture.DISSOLVE_FAILURES if name.startswith('failed-'+v[0]+'-'));value={'execution_id':'failed-'+spec[0],'status':'failed','triggered_by':33,'outputs':{},'metadata':{},'execution_config':{'content':{'workflow_definition':definition}},'error_details':{'code':'develop.workflow.invalid_params','message':'controlled','diagnostic':m.dissolve_diagnostic(spec)}}
                if self.mutate_failed:self.mutate_failed(value)
            else:
                variant='grouped'if name.startswith('grouped-')else'global';self.success_count+=1;targets={port:self.parent.removesuffix('?type=directory')+'/'+variant+'-'+port+'.gpkg?type=file'for port in('result','members')}
                value={'execution_id':variant+'-run','status':'success','triggered_by':33,'execution_config':{'content':{'workflow_definition':definition}},
                    'outputs':{'save_'+port:{'resource':{'locator':target,'type':'file','write_mode':'create'}}for port,target in targets.items()},
                    'metadata':{'lineage_facts':{'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.load_source','locator':self.locators[0]}],'outputs':[{'locator':target,'write_mode':'create'}for target in targets.values()]},
                        'result':{'meta_scan_runs':[{'execution_id':variant+'-'+port+'-scan','target_locator':target,'status':'submitted'}for port,target in targets.items()]}}}
                self.scans.extend(variant+'-'+port+'-scan'for port in targets)
                if self.mutate_success:self.mutate_success(value)
            self.executions[value['execution_id']]=value;data={'execution_id':value['execution_id']}
        elif path.endswith('/engines/3/items'):data=[*self.sources,*self.outputs]
        elif path.startswith('/api/v1/meta/items/'):data=next(v for v in[*self.sources,*self.outputs]if v['id']==int(path.rsplit('/',1)[1]))
        elif '/meta/executions/'in path:data={'status':'success','tenant_id':self.scan_tenant,'triggered_by':self.scan_actor}
        elif path.startswith('/api/v1/meta/lineage/graph?'):
            if self.preflight_denied:raise m.SuiteError('resource authorization unavailable')
            data={'truncated':False,'nodes':[],'edges':[]}
            if 'direction=upstream'in path:
                target=int(path.split('item_id=')[1].split('&')[0]);data['edges']=[{'source':{'item_id':10},'target':{'item_id':target},'relation_kind':'derive','status':'active','evidence':{'execution_id':'grouped-run'if target<32 else'global-run'}}]
                if self.mutate_graph:self.mutate_graph(data)
            elif self.failed_graph:self.failed_graph(data)
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))


class DissolveOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup);self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.DISSOLVE_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=None,browser=lambda *a:a[2]):
        client=client or DissolveClient();physical=physical or(lambda *a:dissolve_physical(2 if client.success_count==1 else 4))
        with patch.object(m.raster,'wait_lineage',return_value={}):
            return m.run_scenario(Path('.'),self.env,client,physical,browser)
    def test_dissolve_two_success_eight_failures_four_scans_and_execution_bound_source_edges(self):
        client=DissolveClient();report=self.run_case(client)
        self.assertEqual((report['result'],report['schema_version']),('passed','addp.polygon-dissolve/v1'))
        self.assertEqual([len(report[k])for k in('successes','outputs','failures','initial_scan_inventory','final_scan_inventory','meta_after')],[2,4,8,5,9,9])
        self.assertEqual([o['feature_count']for o in report['outputs']],[4,10,1,10])
        self.assertEqual(len({r['execution_id']for r in[*report['successes'],*report['failures']]}),10)
        self.assertEqual(sum(method=='POST'and path.endswith('/develop/executions')for method,path,_ in client.calls),10)
    def test_dissolve_requires_resource_visibility_before_any_execution(self):
        client=DissolveClient();client.preflight_denied=True
        with self.assertRaisesRegex(m.SuiteError,'resource authorization'):self.run_case(client)
        self.assertFalse(client.executions)
    def test_dissolve_rejects_actor_size_scan_port_write_mode_and_foreign_or_duplicate_edges(self):
        changes=[lambda c:setattr(c,'scan_actor',True),lambda c:setattr(c,'scan_tenant','2'),lambda c:c.outputs[0].update(size_bytes=True),
            lambda c:setattr(c,'mutate_success',lambda v:v['metadata']['lineage_facts']['inputs'][0].update(port='input.other')),
            lambda c:setattr(c,'mutate_success',lambda v:v['outputs']['save_members']['resource'].update(write_mode='fail')),
            lambda c:setattr(c,'mutate_success',lambda v:v['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail')),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'].append(copy.deepcopy(g['edges'][0]))),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'][0]['evidence'].update(execution_id='other')),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'][0]['source'].update(item_id=True))]
        for change in changes:
            client=DissolveClient();change(client)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_dissolve_failures_never_accept_outputs_scans_wrong_diagnostics_or_derived_edges(self):
        changes=[lambda v:v.update(outputs={'save':{}}),lambda v:v['metadata'].update(lineage_facts={'outputs':[{}]}),lambda v:v['metadata'].update(result={'meta_scan_runs':[{}]}),
            lambda v:v['error_details']['diagnostic']['operator'].update(node_index=True),lambda v:v['error_details']['diagnostic']['operator'].update(field='source_id'),
            lambda v:v['error_details']['diagnostic']['operator'].update(operator_id='load'),lambda v:v['error_details'].update(message='PRIVATE_DISSOLVE_SOURCE')]
        for change in changes:
            client=DissolveClient();client.mutate_failed=change
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(client)
        client=DissolveClient();client.failed_graph=lambda g:g['edges'].append({'relation_kind':'derive','evidence':{'execution_id':'failed-dissolve-id-missing'}})
        with self.assertRaises(m.SuiteError):self.run_case(client)
    def test_dissolve_browser_failure_preserves_checkpoint_and_source_or_result_changes_refuse(self):
        with self.assertRaisesRegex(m.SuiteError,'browser broke'):self.run_case(browser=lambda *a:(_ for _ in()).throw(m.SuiteError('browser broke')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-dissolve-checkpoint.json').read_text());self.assertEqual((checkpoint['result'],checkpoint['stage']),('incomplete','before_browser'));self.assertEqual(len(checkpoint['failures']),8)
        client=DissolveClient()
        def physical(*a):
            value=dissolve_physical(2 if client.success_count==1 else 4)
            if client.success_count==2:value['targets']['grouped-members.gpkg']['sha256']='changed'
            return value
        with self.assertRaisesRegex(m.SuiteError,'existing physical'):self.run_case(client,physical=physical)
        client=DissolveClient()
        def browser(*a):client.sources[0]['size_bytes']=1;return a[2]
        with self.assertRaisesRegex(m.SuiteError,'Meta changed'):self.run_case(client,browser=browser)


def frozen_literal_clipboards(failures):
    import subprocess
    repo=Path(__file__).resolve().parents[2]
    script="""import{readFileSync}from'node:fs';let text='';for await(const p of process.stdin)text+=p;const source=readFileSync(process.argv[1],'utf8'),start=source.indexOf('const inputDiagnosticText'),end=source.indexOf('async function assertInputDiagnosticPanel(',start);
const expected=new Function(source.slice(start,end)+';return inputDiagnosticPageExpectation')();console.log(JSON.stringify(JSON.parse(text).map(f=>({...f,observations:f.observations.map(r=>({...r,clipboard:expected(f.key,r.locale,f.execution_id).clipboard}))}))));"""
    result=subprocess.run(['node','--input-type=module','-e',script,str(repo/'console/frontend/e2e/online/vector-topology-persistence.spec.js')],input=json.dumps(failures),capture_output=True,text=True,timeout=30)
    if result.returncode:raise AssertionError(result.stderr)
    return json.loads(result.stdout)


class DissolveBrowserRawTest(unittest.TestCase):
    def setUp(self):
        self.owner=DissolveOnlineTest();self.owner.setUp();self.addCleanup(self.owner.doCleanups);self.report=self.owner.run_case();self.evidence=self.report['browser'];e=self.evidence
        self.raw={'schema_version':'addp.polygon-dissolve-browser-raw/v1','case':e['case'],'run_id':e['run_id'],'stage':'complete','result':'passed','publication_requests':[],
            'successes':[{'variant':r['variant'],'execution_id':r['execution_id'],'execution':r['execution']}for r in e['successes']],'observations':[],'failures':[]}
        for output in e['outputs']:
            expected=output['features'];encoded=overlay_wire_and_projected(expected);fields=list(expected[0]['properties']);count=len(expected)
            self.raw['observations'].append({'name':output['name'],'port':output['port'],'execution_id':output['execution_id'],'output_item_id':output['item_id'],
                'sources':[{'item_id':s['id'],'item':s['before']}for s in e['sources']],'output':output['before'],'graph':output['raw_lineage'],'lineage_page':output['raw_lineage'],'lineage_rendered':output['raw_lineage'],
                'lineage_canvas':{'rows':[{'text':n}for n in(e['sources'][0]['name'],output['name'])],'paths':[[[0,0],[1,1]]]},
                'preview':{'preview_type':'table','data':{'page':1,'total':count,'source_srid':3857,'source_crs':'EPSG:3857','transform_status':'not_transformed','preview_hint':'frontend_transform_required',
                    'geometry_columns':['parcel_shape'],'columns':['fid','parcel_shape',*fields],'rows':[{**row['properties'],'fid':i+1,'parcel_shape':wire}for i,(row,wire)in enumerate(zip(expected,encoded['wire']))]}},
                'popup':{'properties':expected[0]['properties'],'rows':[{'key':k+':','value':'空值'if v is None else str(v).lower()if type(v)is bool else str(v)}for k,v in{'fid':1,**expected[0]['properties']}.items()]},'decoded_rows':expected,'rendered':{'projection':'EPSG:3857','features':encoded['projected'],'painted_pixels':31}})
        for failed in e['failures']:
            self.raw['failures'].append({'key':failed['key'],'execution_id':failed['execution_id'],'observations':[{'key':failed['key'],'execution_id':failed['execution_id'],'owner':o,'locale':l,'raw':failed['execution']if o=='develop'else{'execution':failed['monitor']}}for o in('develop','monitor')for l in('zh-cn','en')]})
        self.raw['failures']=frozen_literal_clipboards(self.raw['failures'])
    def test_dissolve_browser_accepts_four_maps_two_execution_pages_and_32_exact_diagnostic_reads(self):
        m.validate_dissolve_browser_raw(self.raw,self.evidence)
    def test_dissolve_browser_refuses_wrong_ewkb_member_properties_pixels_graph_uuid_and_clipboard(self):
        changes=[lambda r:r.update(result='incomplete'),lambda r:r['successes'][1]['execution'].update(status='failed'),lambda r:r['observations'].pop(),
            lambda r:r['observations'][0]['preview']['data']['rows'][0].update(member_count=True),lambda r:r['observations'][1]['preview']['data']['rows'][0].update(enabled=1),
            lambda r:r['observations'][1]['preview']['data']['rows'][0].update(parcel_shape=r['observations'][1]['preview']['data']['rows'][1]['parcel_shape']),
            lambda r:r['observations'][0]['rendered']['features'].pop(),lambda r:r['observations'][0]['rendered'].update(painted_pixels=0),
            lambda r:r['observations'][1]['popup']['rows'].pop(),
            lambda r:r['observations'][2]['graph']['edges'][0]['evidence'].update(execution_id='grouped-run'),
            lambda r:r['failures'][0]['observations'].pop(),lambda r:r['failures'][0]['observations'][0].update(clipboard='wrong'),
            lambda r:r['observations'][1]['decoded_rows'][0]['properties'].update(source_id=1)]
        for change in changes:
            raw=copy.deepcopy(self.raw);change(raw)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):m.validate_dissolve_browser_raw(raw,self.evidence)


class DissolveBrowserContractTest(unittest.TestCase):
    def test_dissolve_frozen_literals_match_shared_contract_and_navigation_reuses_current_identity(self):
        import subprocess
        repo=Path(__file__).resolve().parents[2]
        script="""import assert from 'node:assert/strict';import{readFileSync}from'node:fs';const root=process.argv[1],source=readFileSync(root+'/console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8');
const start=source.indexOf('const inputDiagnosticText'),end=source.indexOf('async function assertInputDiagnosticPanel(',start);const values=new Function(source.slice(start,end)+';return inputDiagnosticText')();
for(const locale of ['zh-cn','en']){const d=JSON.parse(readFileSync(root+'/common-frontend/basic/src/i18n/'+locale+'.json','utf8')).common.executionFailure;
for(const[key,reason,param]of [['dissolve-id-missing','operator_input_invalid','input_gdf'],['dissolve-id-duplicate','operator_input_invalid','input_gdf'],['dissolve-invalid','operator_input_invalid','input_gdf'],['dissolve-crs','operator_crs_invalid',''],['dissolve-groups-type','operator_group_fields_invalid','group_fields'],['dissolve-groups-duplicate','operator_group_fields_invalid','group_fields'],['dissolve-groups-missing','operator_group_fields_invalid','group_fields'],['dissolve-fields','operator_dissolve_fields_conflict','']])assert.deepEqual(values[key][locale],[d.reasons[reason],d.operatorNames.dissolve,param?d.operatorParameters[param]:'','',d.actions[reason]]);}
assert.ok(source.includes('await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity)'));assert.ok(source.includes('if(overlay||dissolve||difference)return observations'));assert.ok(source.includes('topology-dissolve-browser-raw.json'));
"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repo)],capture_output=True,text=True,timeout=30);self.assertEqual(result.returncode,0,result.stderr)


    def test_dissolve_real_playwright_discovery_selects_only_approved_case(self):
        import subprocess,os
        repo=Path(__file__).resolve().parents[2]
        result=subprocess.run(['npm','exec','--','playwright','test','e2e/online/vector-topology-persistence.spec.js','--config=playwright.online.config.js','--grep',m.fixture.DISSOLVE_TITLE+'$','--list','--reporter=json'],cwd=repo/'console/frontend',env={**os.environ,**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.DISSOLVE_CASE},capture_output=True,text=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        def specs(suites):return[spec for suite in suites for spec in[*suite.get('specs',[]),*specs(suite.get('suites',[]))]]
        self.assertEqual([v['title']for v in specs(json.loads(result.stdout)['suites'])],[m.fixture.DISSOLVE_TITLE])

    def test_dissolve_actual_map_helper_refuses_lost_features_coordinates_properties_or_pixels(self):
        import subprocess
        repo=Path(__file__).resolve().parents[2]
        script="""import assert from 'node:assert/strict';import{readFileSync}from'node:fs';import{pathToFileURL}from'node:url';
const root=process.argv[1],source=readFileSync(root+'/console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8');
const start=source.indexOf('async function assertDissolveMap('),end=source.indexOf('function assertDissolveLineage(',start);
const{webMercatorPreviewGeometry}=await import(pathToFileURL(root+'/manager/frontend/e2e/geometry-preview-coordinate.js'));
const expect=value=>({toHaveLength:n=>assert.equal(value.length,n)});expect.poll=fn=>({toEqual:async value=>assert.deepEqual(await fn(),value),toBeGreaterThan:async n=>assert.ok(await fn()>n)});
const check=new Function('expect','webMercatorPreviewGeometry',source.slice(start,end)+';return assertDissolveMap')(expect,webMercatorPreviewGeometry);
let text='';for await(const p of process.stdin)text+=p;const expected=JSON.parse(text),native={projection:'EPSG:3857',features:expected.map(r=>({properties:r.properties,geometry:webMercatorPreviewGeometry(r.geometry)}))};
const renderer=(v,pixels=31)=>({evaluate:async()=>v,locator:()=>({evaluateAll:async()=>pixels})});await check(renderer(native),expected);
for(const change of[v=>v.features.pop(),v=>v.features.push(structuredClone(v.features[0])),v=>v.features[0].properties.source_id++,v=>v.features[0].properties.enabled=1,v=>v.features[0].properties.geometry='changed',v=>v.features[0].geometry.coordinates[0][0]+=1,v=>v.projection='EPSG:4326']){const v=structuredClone(native);change(v);await assert.rejects(check(renderer(v),expected));}
await assert.rejects(check(renderer(native,0),expected));"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repo)],input=json.dumps(m.fixture.dissolve_features('grouped-members')),capture_output=True,text=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)


    def test_dissolve_popup_null_literal_matches_real_shared_chinese_formatter(self):
        import subprocess
        repo=Path(__file__).resolve().parents[2]
        script="""import assert from 'node:assert/strict';import{readFileSync}from'node:fs';import{pathToFileURL}from'node:url';
const root=process.argv[1],locale=JSON.parse(readFileSync(root+'/common-frontend/map/src/i18n/zh-cn.json','utf8')).map;
assert.equal(locale.nullValue,'空值');const{formatFeatureProperties}=await import(pathToFileURL(root+'/common-frontend/map/src/utils/mapFormatters.js'));
const html=formatFeatureProperties({fid:1,parcel_key:1099511627805,zone:'B',rank:null,enabled:false,geometry:'PRIVATE_DISSOLVE_BUSINESS_0'},{geometryType:'Polygon',labels:{nullValue:locale.nullValue}});
assert.ok(html.includes('class="null-value">空值</span>'));assert.ok(html.includes('PRIVATE_DISSOLVE_BUSINESS_0'));assert.ok(html.includes('false'));
const source=readFileSync(root+'/console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8');assert.ok(source.includes("popupProperties[field]===null?'空值':String(popupProperties[field])"));"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repo)],capture_output=True,text=True,timeout=30);self.assertEqual(result.returncode,0,result.stderr)


class DifferenceInitializer:
    def __init__(self, consumer):
        self.consumer=consumer;self.calls=[];self.scans=[];self.mutate_context=None;self.mutate_facts=None;self.unknown_approval=False
    def request(self, method, path, expected, body=None):
        from datetime import datetime, timezone, timedelta
        import hashlib,uuid
        registration=importlib.import_module('scripts.test.online-engine-registration')
        self.calls.append((method,path,copy.deepcopy(body)))
        c=self.consumer
        if path.endswith('/auth/context'):
            data={'principal':{'type':'user','id':'8'},'context':{'type':'tenant','tenant_id':'2'},'authorization':{'role_assignments':[{'role_key':registration.SOURCE_INITIALIZER_ROLE,'permissions':sorted(registration.SOURCE_INITIALIZER_PERMISSIONS|{'meta.scan_task.execute','meta.scan_task.read','system.develop_file_create_approval.create'})}]}}
        elif path.endswith('/scan/run/manual'):
            name=body['targets'][0].split('/path/')[1].split('?')[0];identifier=body['scan_depth']+'-'+name;self.scans.append(identifier);data={'execution_id':identifier}
        elif '/meta/scan/runs?' in path:data={'total':len(self.scans),'items':[{'execution_id':identifier} for identifier in self.scans]}
        elif '/meta/executions/' in path:data={'status':'success'}
        elif path.endswith('/authorization-facts'):
            name=next(n for n in m.fixture.DIFFERENCE_SOURCES if hashlib.sha256(('3:'+n).encode()).hexdigest()==path.split('/')[-2])
            target=registration.ExactFileReadGrants.target(3,name,'nfs');composition={'format':'geopackage','refs':[dict(target=target,role='container',required=True,primary=True)]}
            data=dict(schema_version='meta.data_item_authorization_facts/v1',engine_id='3',item_fingerprint=path.split('/')[-2],item_type='file',full_name=name,basis_state='complete',boundary_hash='a'*64,observed_at='2026-11-12T00:00:00Z',boundary=dict(layout='single',file_composition=composition))
            if self.mutate_facts:self.mutate_facts(data)
        elif path.endswith('/access_grants'):
            data={**body,'engine_id':'3','approval_mode':'independent','operator_principal_id':'8','boundary_hash':body['expected_boundary_hash'],'revocation':None,'expires_at':None,
                  'file_composition':dict(format='geopackage',refs=[dict(target=body['catalog_path'],role='container',required=True,primary=True)])}
            c.grants[body['request_id']]=data
        elif '/access_grants/history?' in path:
            rows=[row for row in c.grants.values() if row.get('production_execution_id')==c.run_id];data={'data':rows,'total':len(rows)}
        elif path.endswith('/revoke'):
            c.grants[path.split('/')[-2]]['revocation']={'revoked':True};data={}
        elif path.endswith('/create-approval-context'):
            now=datetime.now(timezone.utc);c.approval_expires=(now+timedelta(minutes=20)).isoformat()
            data=dict(authorization_id='9',execution_id=c.run_id,tenant_id='2',actor_principal_id='33',expected_attempt=1,scope_digest='b'*64,
                target=dict(target=registration.ExactFileReadGrants.target(3,'outputs/difference.gpkg','nfs'),mode='fail',engine_version='238'),
                authorization_expires_at=c.approval_expires,observed_at=(now-timedelta(seconds=1)).isoformat(),state='awaiting_approval')
            if self.mutate_context:self.mutate_context(data)
        elif path.endswith('/develop-geopackage-create-approvals'):
            if self.unknown_approval:raise RuntimeError('PRIVATE_TOKEN_OR_REMOTE_BODY')
            target=registration.ExactFileReadGrants.target(3,'outputs/difference.gpkg','nfs');identifier=str(uuid.uuid4())
            c.grants[identifier]=dict(request_id=identifier,catalog_path=target,action='read',recipient_type='user',recipient_id='33',approval_mode='task_production',producer_owner='develop',production_execution_id=c.run_id,production_result_id=str(uuid.uuid4()),revocation=None,
                engine_id='3',item_fingerprint=hashlib.sha256(b'3:outputs/difference.gpkg').hexdigest(),boundary_hash='a'*64,operator_principal_id='33',expiry_mode='until_revoked',expires_at=None,
                file_composition=dict(format='geopackage',refs=[dict(target=target,role='container',required=True,primary=True)]))
            data=dict(approval_id=str(uuid.uuid4()),approved_at=datetime.now(timezone.utc).isoformat(),expires_at=body['expires_at'])
        else:raise AssertionError((method,path,body))
        return SimpleNamespace(payload=copy.deepcopy(data))


class DifferenceClient(OverlayClient):
    def __init__(self):
        super().__init__(m.fixture.DIFFERENCE_CASE);self.scans=[];self.grants={};self.issuer=DifferenceInitializer(self)
        self.success['metadata'].pop('result')  # The product contract no longer supplies meta_scan_runs.
    def request(self,method,path,expected,body=None):
        if path.endswith('/auth/context'):
            response=super().request(method,path,expected,body);response.payload['principal']['id']='33';return response
        if path.endswith('/scan/run/manual'):
            self.calls.append((method,path,body));name=body['targets'][0].split('/path/')[1].split('?')[0]
            identifier='deep-'+name;self.scans.append(identifier);return SimpleNamespace(payload={'execution_id':identifier})
        if path.endswith('/read-checks/manager-preview'):
            self.calls.append((method,path,body));allowed=any(row['catalog_path']==body['targets'][0] and row['revocation'] is None for row in self.grants.values())
            if expected!=((200,) if allowed else (403,)):raise m.SuiteError('read-check mismatch')
            return SimpleNamespace(payload={'observed_at':'2026-11-12T00:00:00Z'} if allowed else {})
        if path.endswith('/meta/executions/overlay-output-scan'):
            return SimpleNamespace(payload=dict(execution_id='overlay-output-scan',status='success',tenant_id=self.scan_tenant,triggered_by=self.scan_actor,source='develop',parent_execution_id=self.run_id,
                scan_context=dict(scan_depth='basic',engine_id=3)))
        return super().request(method,path,expected,body)


class DifferenceOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup);self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.DIFFERENCE_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name,'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'initializer-test-token','GATEWAY_URL':'http://deterministic.invalid'}
    def run_case(self,client=None,physical=difference_physical,browser=lambda *a:a[2]):
        client=client or DifferenceClient()
        with patch.object(m.raster,'wait_lineage',return_value={}),patch.object(m.raster,'GatewayClient',return_value=client.issuer):
            return m.run_scenario(Path('.'),self.env,client,physical,browser)
    def test_difference_one_success_four_failures_single_scan_and_two_source_edges(self):
        client=DifferenceClient();report=self.run_case(client);self.assertEqual((report['result'],report['schema_version']),('passed','addp.geometry-difference/v1'))
        self.assertEqual([len(report[k])for k in('sources','outputs','failures','initial_scan_inventory','final_scan_inventory','meta_after')],[5,1,4,0,1,6])
        self.assertEqual(len(report['browser']['expected']['result']),9);self.assertEqual(len(report['raw_lineage']['edges']),2)
        self.assertEqual(sum(method=='POST'and path.endswith('/develop/executions')for method,path,_ in client.calls),5)
        for failure in report['failures']:
            tasks = failure['definition']['tasks']
            self.assertEqual([task['operator'] for task in tasks], ['difference', 'load', 'load'])
            self.assertEqual(tasks[0]['depends_on'], ['load_a', 'load_b'])
            self.assertTrue(all('target_parent_locator' not in task['params'] and 'target_name' not in task['params'] for task in tasks))
    def test_difference_stages_keep_basic_grants_approval_and_deep_separate(self):
        client=DifferenceClient();report=self.run_case(client)
        stages=[row['stage'] for row in report['publication']['stages']]
        self.assertEqual(stages[:3],['source_basic_identified','source_read_granted','source_deep_inspected'])
        self.assertEqual(stages[15:20],['approval_intent','approval_confirmed','result_basic_identified','result_initial_read_granted','result_deep_inspected'])
        self.assertEqual(stages[-3:],['source_grants_revoked','materialized_result_still_readable','all_grants_revoked'])
        self.assertEqual(report['publication']['pending_grants'],[])
        self.assertEqual(len(report['publication']['preparation_scan_inventory']),11)
        self.assertEqual(len([1 for method,path,_ in client.issuer.calls if method=='POST' and path.endswith('/develop-geopackage-create-approvals')]),1)
        self.assertEqual(len([1 for method,path,_ in client.issuer.calls if method=='POST' and path.endswith('/access_grants')]),5)
        self.assertTrue(all(row['revocation'] is not None for row in client.grants.values()))
        self.assertNotIn('meta_scan_runs',report['execution']['metadata'])

    def test_difference_context_mismatch_never_approves_or_leaks_and_revokes_sources(self):
        registration=importlib.import_module('scripts.test.online-engine-registration')
        for mutate in [lambda c:c.update(tenant_id='1'),lambda c:c.update(actor_principal_id='8'),lambda c:c.update(scope_digest='bad'),lambda c:c['target'].update(mode='replace'),lambda c:c.update(state='approved'),lambda c:c['target']['target']['segments'][-1].update(name='other.gpkg')]:
            client=DifferenceClient();client.issuer.mutate_context=mutate
            with self.subTest(mutate=mutate),self.assertRaises(registration.MetadataGrantError):self.run_case(client)
            self.assertFalse(any(path.endswith('/develop-geopackage-create-approvals') for _,path,_ in client.issuer.calls))
            self.assertTrue(all(row['revocation'] is not None for row in client.grants.values()))

    def test_difference_unknown_approval_posts_once_and_keeps_original_intent(self):
        registration=importlib.import_module('scripts.test.online-engine-registration')
        client=DifferenceClient();client.issuer.unknown_approval=True
        with self.assertRaisesRegex(registration.MetadataGrantError,'outcome_unknown_no_resubmit'):self.run_case(client)
        evidence=json.loads((Path(self.directory.name)/'topology-difference-publication.json').read_text())
        self.assertEqual(evidence['result'],'incomplete')
        self.assertIn('approval_outcome_unknown',[row['stage'] for row in evidence['stages']])
        self.assertNotIn('PRIVATE_TOKEN',json.dumps(evidence))
        self.assertEqual(len([1 for method,path,_ in client.issuer.calls if method=='POST' and path.endswith('/develop-geopackage-create-approvals')]),1)

    def test_difference_incomplete_or_expanded_file_boundary_never_grants(self):
        registration=importlib.import_module('scripts.test.online-engine-registration')
        for mutate in [lambda f:f.update(basis_state='incomplete'),lambda f:f['boundary']['file_composition']['refs'].append(copy.deepcopy(f['boundary']['file_composition']['refs'][0])),lambda f:f['boundary'].update(layout='multi')]:
            client=DifferenceClient();client.issuer.mutate_facts=mutate
            with self.subTest(mutate=mutate),self.assertRaises(registration.MetadataGrantError):self.run_case(client)
            self.assertFalse(client.grants)
            self.assertFalse(client.executions)

    def test_difference_requires_resource_visibility_before_any_execution(self):
        client=DifferenceClient();client.preflight_denied=True
        with self.assertRaisesRegex(m.SuiteError,'resource authorization'):self.run_case(client)
        self.assertFalse(client.executions)
    def test_difference_rejects_wrong_actor_scan_size_port_mode_and_graph(self):
        changes=[lambda c:setattr(c,'scan_actor',True),lambda c:setattr(c,'scan_tenant','2'),lambda c:c.outputs[0].update(size_bytes=True),
            lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(port='input.WRONG'),lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(locator=c.locators[0]),
            lambda c:c.success['outputs']['save_result']['resource'].update(write_mode='fail'),lambda c:c.success['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),
            lambda c:setattr(c,'mutate_graph',lambda g:g['edges'].pop()),lambda c:setattr(c,'mutate_graph',lambda g:g['edges'][0]['source'].update(item_id=True))]
        for change in changes:
            client=DifferenceClient();change(client)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_difference_failure_refuses_publication_scan_wrong_operator_or_private_message(self):
        changes=[lambda v:v.update(outputs={'save':{}}),lambda v:v['metadata'].update(lineage_facts={'outputs':[{}]}),lambda v:v['metadata'].update(result={'meta_scan_runs':[{}]}),
            lambda v:v['error_details']['diagnostic']['operator'].update(operator_id='load'),lambda v:v['error_details']['diagnostic']['operator'].update(node_index=True),
            lambda v:v['error_details']['diagnostic']['operator'].update(field='a_key'),lambda v:v['error_details'].update(message='PRIVATE_DIFFERENCE_SOURCE')]
        for change in changes:
            client=DifferenceClient();client.mutate_failed=change
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(client)
        client=DifferenceClient();client.failed_graph=lambda g:g['edges'].append({'relation_kind':'derive','evidence':{'execution_id':'difference-failed-difference-id'}})
        with self.assertRaises(m.SuiteError):self.run_case(client)
    def test_difference_browser_failure_keeps_checkpoint_and_changed_bytes_or_meta_refuse(self):
        with self.assertRaisesRegex(m.SuiteError,'browser broke'):self.run_case(browser=lambda *a:(_ for _ in()).throw(m.SuiteError('browser broke')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-difference-checkpoint.json').read_text());self.assertEqual((checkpoint['result'],checkpoint['stage']),('incomplete','before_browser'));self.assertEqual(len(checkpoint['failures']),4)
        calls=[]
        def physical(*a):
            value=difference_physical();calls.append(1)
            if len(calls)>5:value['targets'][m.fixture.DIFFERENCE_TARGET]['sha256']='changed'
            return value
        with self.assertRaises(m.SuiteError):self.run_case(physical=physical)
        client=DifferenceClient()
        def browser(*a):client.sources[0]['size_bytes']=1;return a[2]
        with self.assertRaisesRegex(m.SuiteError,'Meta changed'):self.run_case(client,browser=browser)


class DifferenceBrowserRawTest(unittest.TestCase):
    def setUp(self):
        self.owner=DifferenceOnlineTest();self.owner.setUp();self.addCleanup(self.owner.doCleanups);report=self.owner.run_case();self.evidence=report['browser'];e=self.evidence;output=e['outputs'][0];expected=e['expected']['result'];encoded=overlay_wire_and_projected(expected);fields=list(expected[0]['properties'])
        observation={'port':'result','output_item_id':30,'sources':[{'item_id':v['id'],'item':v['before']}for v in e['sources']],'output':output['before'],'graph':report['raw_lineage'],'lineage_page':report['raw_lineage'],'lineage_rendered':report['raw_lineage'],
            'lineage_canvas':{'rows':[{'text':n}for n in[e['sources'][0]['name'],e['sources'][1]['name'],output['name']]],'paths':[[[0,0],[1,1]],[[1,0],[2,1]]]},
            'preview':{'preview_type':'table','data':{'page':1,'total':9,'source_srid':3857,'source_crs':'EPSG:3857','transform_status':'not_transformed','preview_hint':'frontend_transform_required','geometry_columns':['geom'],'columns':['fid','geom',*fields],
                'rows':[{**row['properties'],'fid':i+1,'geom':wire}for i,(row,wire)in enumerate(zip(expected,encoded['wire']))]}},
            'decoded_rows':expected,'rendered':{'projection':'EPSG:3857','features':encoded['projected'],'collection_pixels':{'point':31,'line':32,'polygon':33,'members':[{'kind':{'Point':'point','LineString':'line','Polygon':'polygon'}[v['type']],'pixels':31}for v in next(row['geometry']['geometries']for row in expected if row['geometry']['type']=='GeometryCollection')]}}}
        self.raw={'schema_version':'addp.geometry-difference-browser-raw/v1','case':e['case'],'run_id':e['run_id'],'execution_id':e['execution_id'],'result':'passed','stage':'complete','execution':DifferenceClient().success,'observations':[observation],'publication_requests':[],'failures':[]}
        for failed in report['failures']:
            self.raw['failures'].append({'key':failed['key'],'execution_id':failed['execution_id'],'observations':[{'owner':o,'locale':l,'raw':failed['execution']if o=='develop'else{'execution':failed['monitor']}}for o in('develop','monitor')for l in('zh-cn','en')]})
        self.raw['failures']=frozen_literal_clipboards(self.raw['failures'])
    def test_difference_raw_accepts_complete_mixed_ewkb_original_attributes_and_exact_copies(self):m.validate_difference_browser_raw(self.raw,self.evidence)
    def test_difference_raw_refuses_missing_parts_type_changes_wires_copies_or_edges(self):
        changes=[lambda r:r.update(result='incomplete'),lambda r:r.update(execution_id='other'),lambda r:r['observations'][0]['preview']['data']['rows'][0].update(grade=0),
            lambda r:r['observations'][0]['preview']['data']['rows'][0].update(enabled=1),lambda r:r['observations'][0]['preview']['data']['rows'][0].update(geom=r['observations'][0]['preview']['data']['rows'][1]['geom']),
            lambda r:r['observations'][0]['rendered']['features'][7]['geometry']['geometries'].pop(),lambda r:r['observations'][0]['rendered']['collection_pixels'].update(point=0),lambda r:r['observations'][0]['rendered']['collection_pixels']['members'].pop(),
            lambda r:r['observations'][0]['graph']['edges'].pop(),lambda r:r['failures'][0]['observations'].pop(),lambda r:r['failures'][0]['observations'][0].update(clipboard='wrong')]
        for change in changes:
            raw=copy.deepcopy(self.raw);change(raw)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):m.validate_difference_browser_raw(raw,self.evidence)


class DifferenceBrowserContractTest(unittest.TestCase):
    def test_difference_real_discovery_selects_one_case_and_literals_match_shared_diagnostic_contract(self):
        import subprocess,os
        repo=Path(__file__).resolve().parents[2]
        script="""import assert from 'node:assert/strict';import{readFileSync}from'node:fs';const root=process.argv[1],source=readFileSync(root+'/console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8');
const start=source.indexOf('const inputDiagnosticText'),end=source.indexOf('async function assertInputDiagnosticPanel(',start),values=new Function(source.slice(start,end)+';return inputDiagnosticText')();
for(const locale of ['zh-cn','en']){const d=JSON.parse(readFileSync(root+'/common-frontend/basic/src/i18n/'+locale+'.json','utf8')).common.executionFailure;
for(const[key,reason,param]of[['difference-id','operator_input_invalid','input_gdf'],['difference-geometry','operator_input_invalid','input_gdf'],['difference-mask','operator_mask_invalid','gdf_b'],['difference-crs','operator_crs_invalid','']])assert.deepEqual(values[key][locale],[d.reasons[reason],d.operatorNames.difference,param?d.operatorParameters[param]:'','',d.actions[reason]]);}
assert.ok(source.includes('async function verifyMixedOverlayCase('));assert.ok(source.includes('topology-difference-browser-raw.json'));"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repo)],capture_output=True,text=True);self.assertEqual(result.returncode,0,result.stderr)
        result=subprocess.run(['npm','exec','--','playwright','test','e2e/online/vector-topology-persistence.spec.js','--config=playwright.online.config.js','--grep',m.fixture.DIFFERENCE_TITLE+'$','--list','--reporter=json'],cwd=repo/'console/frontend',env={**os.environ,**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.DIFFERENCE_CASE},capture_output=True,text=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        def specs(suites):return[v for suite in suites for v in[*suite.get('specs',[]),*specs(suite.get('suites',[]))]]
        self.assertEqual([v['title']for v in specs(json.loads(result.stdout)['suites'])],[m.fixture.DIFFERENCE_TITLE])


    def test_difference_real_pixel_helper_checks_every_collection_member(self):
        import subprocess
        repo=Path(__file__).resolve().parents[2]
        script="""import assert from 'node:assert/strict';import{readFileSync}from'node:fs';import GeoJSON from 'ol/format/GeoJSON.js';
const source=readFileSync(process.argv[1],'utf8'),start=source.indexOf('async function assertOverlayCollectionPixels('),end=source.indexOf('const mixedOverlayCases=',start);
const expect={poll:fn=>({toBe:async wanted=>assert.equal(await fn(),wanted)})},check=new Function('expect',source.slice(start,end)+';return assertOverlayCollectionPixels')(expect);
let text='';for await(const p of process.stdin)text+=p;const geometry=new GeoJSON().readGeometry(JSON.parse(text));
async function run(bad=-1){let next=0;const feature={getGeometry:()=>geometry};const context={canvas:{width:100,height:100,getBoundingClientRect:()=>({left:0,top:0,width:100,height:100})},getImageData:()=>{const a=new Uint8ClampedArray(13*13*4);if(next++!==bad)for(let i=3;i<a.length;i+=4)a[i]=255;return{data:a}}};
const layer={getSource:()=>({getFeatures:()=>[feature]}),getRenderer:()=>({context}),getVisible:()=>true,getOpacity:()=>1};const map={getLayers:()=>({getArray:()=>[layer]}),getView:()=>({fit(){},getAnimating:()=>false}),getSize:()=>[100,100],renderSync(){},getViewport:()=>({getBoundingClientRect:()=>({left:0,top:0})}),getPixelFromCoordinate:()=>[50,50],forEachFeatureAtPixel:(_,fn)=>fn(feature)};
const result=await check({evaluate:async(fn,arg)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},arg)});assert.equal(result.members.length,geometry.getGeometries().length);return result;}
const good=await run();assert.deepEqual(good.members.map(v=>v.kind),['polygon','polygon','line','line','point','point']);for(let i=0;i<6;i++)await assert.rejects(run(i));"""
        shape=next(r['geometry']for r in m.fixture.difference_features('result')if r['geometry']['type']=='GeometryCollection')
        result=subprocess.run(['node','--input-type=module','-e',script,str(repo/'console/frontend/e2e/online/vector-topology-persistence.spec.js')],cwd=repo/'manager/frontend',input=json.dumps(shape),capture_output=True,text=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)


def vertices_physical(*args):
    return {'exact_inventory_verified':True,'sources':{n:{'size_bytes':98304,'sha256':n} for n in m.fixture.VERTICES_SOURCES},
        'targets':{name:{'size_bytes':98304,'sha256':name,'feature_count':len(m.fixture.vertices_features(port+'-result')),'layer_name':name[:-5],
            'geometry_column':'geom','fid_column':'fid','features':m.fixture.vertices_features(port+'-result')}
            for port,name in zip(m.fixture.VERTICES_FAMILIES,m.fixture.VERTICES_TARGETS)}}


class VerticesClient(InputDiagnosticClient):
    def __init__(self):
        super().__init__();self.permissions=sorted(m.PERMISSIONS);self.sources=[];self.outputs=[];self.mutate_failed=None;self.failed_graph=None;self.mutate_graph=None;self.scan_actor=33;self.scan_tenant=2
        for i,(name,kind) in enumerate(zip(m.fixture.VERTICES_SOURCES,m.fixture.VERTICES_KINDS)):
            rows=m.fixture.vertices_features(kind);item=base.item(name,10+i);item['full_name']=name
            item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.sources.append(item)
        for i,(name,port) in enumerate(zip(m.fixture.VERTICES_TARGETS,m.fixture.VERTICES_FAMILIES)):
            rows=m.fixture.vertices_features(port+'-result');item=base.item(name,30+i)
            item['attributes']['type_info']['container']['children'][0].update(row_count=len(rows),column_count=len(rows[0]['properties'])+2);self.outputs.append(item)
        self.scans=['scan-'+n for n in m.fixture.VERTICES_SOURCES]
        self.locators=[f'addp://engine/3/path/{n}?type=file&item_id={10+i}' for i,n in enumerate(m.fixture.VERTICES_SOURCES)]
        self.parent='addp://engine/3/path/outputs?type=directory'
        targets={p:f'addp://engine/3/path/outputs/{n}?type=file' for p,n in zip(m.fixture.VERTICES_FAMILIES,m.fixture.VERTICES_TARGETS)}
        self.success={'execution_id':'vertices-run','status':'success','triggered_by':33,
            'outputs':{'save_'+p:{'resource':{'locator':t,'type':'file','write_mode':'create'}} for p,t in targets.items()},
            'execution_config':{'content':{'workflow_definition':m.vertices_definition(self.locators,self.parent)}},
            'metadata':{'lineage_facts':{'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.load_'+p,'locator':self.locators[i]} for i,p in enumerate(m.fixture.VERTICES_FAMILIES)],
              'outputs':[{'locator':t,'write_mode':'create'} for t in targets.values()]},'result':{'meta_scan_runs':[{'execution_id':'output-scan-'+p,'target_locator':t,'status':'submitted'} for p,t in targets.items()]}}}
    def request(self,method,path,expected,body=None):
        if path.endswith('/auth/context'):
            return SimpleNamespace(payload={'principal':{'id':33,'type':'user'},'context':{'type':'tenant','tenant_id':'2'},'token':{'type':'first_party_access_token'},'authorization':{'role_assignments':[{'role_key':'topology-consumer','permissions':self.permissions}]}})
        if method=='POST' and path.endswith('/develop/executions'):
            self.calls.append((method,path,body));definition=body['content']['workflow_definition'];name=definition['tasks'][-1]['params']['target_name']
            if name.startswith('failed-'):
                spec=next(v for v in m.fixture.VERTICES_FAILURES if name=='failed-'+v[0]+'.gpkg')
                value={'execution_id':'failed-'+spec[0],'status':'failed','triggered_by':33,'outputs':{},'metadata':{},
                    'execution_config':{'content':{'workflow_definition':definition}},'error_details':{'code':'develop.workflow.invalid_params','message':'controlled','diagnostic':m.vertices_diagnostic(spec)}}
                if self.mutate_failed:self.mutate_failed(value)
            else:value=self.success;self.scans.extend(['output-scan-'+p for p in m.fixture.VERTICES_FAMILIES])
            self.executions[value['execution_id']]=value;data={'execution_id':value['execution_id']}
        elif path.endswith('/engines/3/items'):data=[*self.sources,*self.outputs]
        elif path.startswith('/api/v1/meta/items/'):
            item_id=int(path.rsplit('/',1)[1]);data=next(v for v in [*self.sources,*self.outputs] if v['id']==item_id)
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':self.scan_tenant,'triggered_by':self.scan_actor}
        elif path.startswith('/api/v1/meta/lineage/graph?'):
            if 'direction=downstream' in path:
                data={'truncated':False,'permission_filtered':False,'nodes':[],'edges':[]}
                if self.failed_graph:self.failed_graph(data)
            else:
                target=int(path.split('item_id=')[1].split('&')[0]);data={'truncated':False,'permission_filtered':False,'nodes':[],'edges':[{'source':{'item_id':target-20},'target':{'item_id':target},'relation_kind':'derive','status':'active','evidence':{'execution_id':'vertices-run'}}]}
                if self.mutate_graph:self.mutate_graph(data)
        else:return super().request(method,path,expected,body)
        return SimpleNamespace(payload=copy.deepcopy(data))




class VerticesOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.VERTICES_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=vertices_physical,browser=lambda *a:a[2]):
        return m.run_scenario(Path('.'),self.env,client or VerticesClient(),physical,browser,diagnostic_runner=lambda *a,**k:self.fail('no private log oracle'))
    def test_vertices_one_success_seven_failures_three_point_scans_exact_sources(self):
        c=VerticesClient();report=self.run_case(c)
        self.assertEqual((report['result'],report['schema_version']),('passed','addp.vertex-extraction/v1'))
        self.assertEqual([len(report['browser']['expected'][p]) for p in m.fixture.VERTICES_FAMILIES],[3,8,20])
        self.assertEqual([len(report[k]) for k in ['initial_scan_inventory','final_scan_inventory','meta_after']],[10,13,13])
        self.assertEqual(sum(len(g['edges']) for g in report['raw_lineage'].values()),3)
        self.assertEqual(len(report['source_authorization_preflight']),10)
        self.assertEqual([f['key'] for f in report['failures']],[s[0] for s in m.fixture.VERTICES_FAILURES])
        self.assertEqual(sum(method=='POST' and path.endswith('/develop/executions') for method,path,_ in c.calls),8)
        self.assertEqual(len({report['execution_id'],*[f['execution_id'] for f in report['failures']]}),8)
    def test_vertices_formal_resource_visibility_precedes_any_business_execution(self):
        c=VerticesClient();original=c.request
        def deny(method,path,expected,body=None):
            if '/meta/lineage/graph?' in path:raise m.SuiteError('formal resource grant missing')
            return original(method,path,expected,body)
        c.request=deny
        with self.assertRaisesRegex(m.SuiteError,'formal resource'):self.run_case(c)
        self.assertFalse(any(method=='POST' and path.endswith('/develop/executions') for method,path,_ in c.calls))
    def test_vertices_identity_exact_permissions_ports_sizes_and_wrong_source_edges_rejected(self):
        mutations=[lambda c:setattr(c,'scan_actor',True),lambda c:setattr(c,'scan_tenant','2'),lambda c:c.outputs[0].update(size_bytes=True),
          lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(port='input.other'),
          lambda c:c.success['metadata']['lineage_facts']['outputs'][0].update(write_mode='fail'),
          lambda c:c.success['outputs'].update(extra={}),lambda c:c.permissions.append('extra.read'),
          lambda c:setattr(c,'mutate_graph',lambda g:g['edges'].append(copy.deepcopy(g['edges'][0])))]
        for mutate in mutations:
            c=VerticesClient();mutate(c)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(c)
    def test_vertices_failures_reject_publication_wrong_tuple_and_private_values(self):
        mutations=[lambda v:v.update(outputs={'save':{}}),lambda v:v['metadata'].update(result={'meta_scan_runs':[{}]}),
            lambda v:v['metadata'].update(lineage_facts={'outputs':[{}]}),lambda v:v['error_details']['diagnostic']['operator'].update(node_index=True),
            lambda v:v['error_details'].update(message='PRIVATE_DIAGNOSTIC_SOURCE_VALUE'),lambda v:v['error_details']['diagnostic'].update(reason='other')]
        for mutate in mutations:
            c=VerticesClient();c.mutate_failed=mutate
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):self.run_case(c)
    def test_vertices_browser_failure_keeps_incomplete_not_final_pass(self):
        with self.assertRaisesRegex(m.SuiteError,'browser broke'):self.run_case(browser=lambda *a:(_ for _ in ()).throw(m.SuiteError('browser broke')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-vertices-checkpoint.json').read_text())
        self.assertEqual((checkpoint['result'],checkpoint['stage']),('incomplete','before_browser'));self.assertEqual(len(checkpoint['failures']),7)
    def test_vertices_final_files_scans_and_meta_cannot_change(self):
        calls=[]
        def physical(*a):
            value=vertices_physical();calls.append(1)
            if len(calls)>8:value['targets'][m.fixture.VERTICES_TARGETS[0]]['sha256']='changed'
            return value
        with self.assertRaisesRegex(m.SuiteError,'physical files'):self.run_case(physical=physical)
        c=VerticesClient()
        def browser(*args):c.sources[0]['size_bytes']=1;return args[2]
        with self.assertRaisesRegex(m.SuiteError,'Meta changed'):self.run_case(c,browser=browser)

    def raw_witness(self):
        import base64,struct
        report=self.run_case();e=report['browser'];c=VerticesClient()
        raw={'schema_version':m.fixture.VERTICES_SCHEMA+'-browser-raw/v1','result':'passed','stage':'complete',**{k:e[k] for k in ('case','run_id','execution_id')},'execution':c.success,'observations':[],'failures':[]}
        for output in e['outputs']:
            expected=e['expected'][output['port']];fields=list(expected[0]['properties']);rows=[]
            for i,row in enumerate(expected,1):
                wire=base64.b64encode(struct.pack('<BIIdd',1,0x20000001,3857,*row['geometry']['coordinates'])).decode()
                rows.append({output['fid_column']:i,output['geometry_column']:wire,**row['properties']})
            graph=report['raw_lineage'][output['port']]
            preview={'preview_type':'table','data':{'total':len(rows),'source_srid':3857,'source_crs':'EPSG:3857','transform_status':'not_transformed','preview_hint':'frontend_transform_required',
                'columns':[output['fid_column'],output['geometry_column'],*fields],'geometry_columns':[output['geometry_column']],'rows':rows}}
            raw['observations'].append({'port':output['port'],'output_item_id':output['item_id'],'sources':[{'item_id':s['id'],'item':s['before']} for s in e['sources']],
                'output':output['before'],'graph':graph,'lineage_rendered':{'edges':graph['edges']},'preview':preview,'preview_text':json.dumps(preview),
                'rendered':{'projection':'EPSG:3857','painted_pixels':31,'features':[{'type':'Point','coordinates':r['geometry']['coordinates'],'properties':r['properties']} for r in expected]}})
        for failed in report['failures']:
            raw['failures'].append({'key':failed['key'],'execution_id':failed['execution_id'],'observations':[{'owner':owner,'locale':locale,'execution':failed['execution'] if owner=='develop' else {'execution':failed['monitor']},'clipboard':m.vertices_clipboard(failed,locale)} for owner in ('develop','monitor') for locale in ('zh-cn','en')]})
        return raw,e
    def test_vertices_raw_three_maps_wire_types_numbering_duplicates_and_bilingual_binding(self):
        raw,e=self.raw_witness();m.validate_vertices_browser_raw(raw,e)
        mutations=[lambda v:v.update(stage='incomplete'),lambda v:v['observations'].pop(),lambda v:v['observations'][2]['preview']['data']['rows'].pop(),
            lambda v:v['observations'][0].update(preview_text='{}'),lambda v:v['observations'][0]['preview']['data']['rows'][0].update(vertex_id=True),
            lambda v:v['observations'][1]['rendered']['features'].pop(),lambda v:v['observations'][0]['rendered'].update(painted_pixels=0),
            lambda v:v['observations'][0]['graph']['edges'][0]['source'].update(item_id=11),
            lambda v:v['failures'][0]['observations'].pop(),lambda v:v['failures'][0]['observations'][0].update(locale='other'),lambda v:v['failures'][0]['observations'][0].update(clipboard='controlled'),lambda v:v['observations'][0]['graph'].update(permission_filtered=True)]
        for mutate in mutations:
            value=copy.deepcopy(raw);mutate(value)
            with self.subTest(mutate=mutate),self.assertRaises(m.SuiteError):m.validate_vertices_browser_raw(value,e)
    def test_vertices_browser_panel_and_point_map_reuse_exact_occurrences(self):
        import subprocess
        repository=Path(__file__).resolve().parents[2]
        script=r"""
import assert from 'node:assert/strict';import fs from 'node:fs';
const source=fs.readFileSync(process.argv[1],'utf8'),groups=JSON.parse(process.argv[2]),specs=JSON.parse(process.argv[3]);
const expect=value=>({toBe:other=>assert.equal(value,other),toHaveLength:other=>assert.equal(value.length,other)});
expect.poll=callback=>({toEqual:async other=>assert.deepEqual(await callback(),other),toBeGreaterThan:async n=>assert.ok(await callback()>n)});
const {webMercatorPreviewCoordinate}=await import(process.argv[4]);
const a=source.indexOf('function proximityPointExpectation('),b=source.indexOf('function assertProximityLineage(',a);
const run=new Function('expect','webMercatorPreviewCoordinate',source.slice(a,b)+';return assertProximityPointMap')(expect,webMercatorPreviewCoordinate);
for(const rows of groups){let step=0;const observed={projection:'EPSG:3857',features:rows.map(r=>({type:'Point',properties:r.properties,coordinates:webMercatorPreviewCoordinate(r.geometry.coordinates)}))};
 const renderer={evaluate:async()=>step++===0?observed:40};await run(renderer,rows);
 const bad={...observed,features:observed.features.slice(1)};let n=0;await assert.rejects(()=>run({evaluate:async()=>n++===0?bad:40},rows));
}
const start=source.indexOf('function nodesPanelExpectation('),end=source.indexOf('async function verifyNodesCase(',start);
const panel=new Function('expect',source.slice(start,end)+';return nodesPanelExpectation')(expect);
for(const spec of specs)for(const locale of ['zh-cn','en']){
 const failure={execution_id:'current-'+spec[0],expected_diagnostic:{reason:spec[1],operator:{node_index:1,operator_id:'extract_vertices',...(spec[0]==='crs'?{}:{parameter:'input_gdf'}),...(spec[2]?{field:spec[2]}:{})}}};
 const value=panel(failure,locale,true);assert.ok(value.clipboard.includes(locale==='en'?'Extract vertices':'顶点提取'));assert.ok(value.clipboard.includes(failure.execution_id));assert.ok(!value.clipboard.includes('PRIVATE'));
 if(spec[2])assert.ok(value.action.includes(spec[2]));if(spec[0]==='crs')assert.ok(!value.clipboard.includes('input_gdf'));
}
const body=source.slice(source.indexOf('async function verifyPointExtractionCase('),source.indexOf("test('节点提取"));
assert.ok(body.indexOf('const observation=')<body.indexOf('if(vertices)observation.preview_text='));
assert.equal(source.slice(0,source.indexOf('async function verifyPointExtractionCase')).includes('if(vertices)observation.preview_text='),false);
assert.ok(body.includes('vertices&&expected.length>10'));assert.ok(body.includes('sources:vertices?[evidence.sources[outputIndex]]'));
"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),
            json.dumps([m.fixture.vertices_features(p+'-result') for p in m.fixture.VERTICES_FAMILIES]),json.dumps(m.fixture.VERTICES_FAILURES),(repository/'manager/frontend/e2e/geometry-preview-coordinate.js').as_uri()],text=True,capture_output=True)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)


class AdjacencyClient(OverlayClient):
    def __init__(self):super().__init__(m.fixture.ADJACENCY_CASE)


class AdjacencyOnlineTest(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.ADJACENCY_CASE,'ADDP_ONLINE_ARTIFACT_DIR':self.directory.name}
    def run_case(self,client=None,physical=None,browser=lambda *a:a[2]):
        return m.run_scenario(Path('.'),self.env,client or AdjacencyClient(),physical or(lambda *a:mixed_physical(m.fixture.ADJACENCY_CASE)),browser)
    def test_adjacency_single_source_one_save_four_readonly_failures_scans_and_identity(self):
        client=AdjacencyClient();report=self.run_case(client)
        self.assertEqual((report['result'],report['schema_version']),('passed','addp.polygon-adjacency/v1'))
        self.assertEqual([len(report[k])for k in('initial_scan_inventory','final_scan_inventory','meta_after')],[5,6,6])
        self.assertEqual(len(report['raw_lineage']['edges']),1)
        self.assertEqual(report['raw_lineage']['edges'][0]['source']['item_id'],10)
        self.assertEqual(len(report['browser']['expected']['result']),5)
        self.assertEqual([f['key']for f in report['failures']],[s[0]for s in m.fixture.ADJACENCY_FAILURES])
        for failure in report['failures']:
            self.assertEqual([t['operator']for t in failure['definition']['tasks']],['check_polygon_adjacency','load'])
            self.assertEqual(len(failure['source_lineage']),1)
        self.assertEqual(sum(method=='POST'and path.endswith('/develop/executions')for method,path,_ in client.calls),5)
    def test_adjacency_formal_visibility_required_and_strict_single_input_binding(self):
        client=AdjacencyClient();client.preflight_denied=True
        with self.assertRaisesRegex(m.SuiteError,'resource authorization'):self.run_case(client)
        self.assertFalse(client.executions)
        changes=[lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(port='input.load_a'),lambda c:c.success['metadata']['lineage_facts']['inputs'].append({'port':'input.load_source','locator':c.locators[0]}),lambda c:c.success['metadata']['lineage_facts']['inputs'][0].update(locator=c.locators[1]),lambda c:setattr(c,'scan_actor',True),lambda c:setattr(c,'scan_tenant','2'),lambda c:c.outputs[0].update(size_bytes=True),lambda c:setattr(c,'mutate_graph',lambda g:g['edges'].append(copy.deepcopy(g['edges'][0])))]
        for change in changes:
            client=AdjacencyClient();change(client)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(client)
    def test_adjacency_failed_facts_scans_private_values_and_changed_physical_refuse(self):
        changes=[lambda v:v.update(outputs={'save_result':{}}),lambda v:v['metadata'].update(lineage_facts={'outputs':[{}]}),lambda v:v['metadata'].update(result={'meta_scan_runs':[{}]}),lambda v:v['error_details']['diagnostic']['operator'].update(parameter='id_field'),lambda v:v['error_details']['diagnostic']['operator'].update(field='parcel_key'),lambda v:v['error_details'].update(message='PRIVATE_ADJACENCY_NODE')]
        for change in changes:
            client=AdjacencyClient();client.mutate_failed=change
            with self.subTest(change=change),self.assertRaises(m.SuiteError):self.run_case(client)
        calls=[]
        def physical(*a):
            value=mixed_physical(m.fixture.ADJACENCY_CASE);calls.append(1)
            if len(calls)>1:value['sources'][m.fixture.ADJACENCY_SOURCES[0]]['sha256']='changed'
            return value
        with self.assertRaises(m.SuiteError):self.run_case(physical=physical)
    def test_adjacency_browser_failure_preserves_incomplete_checkpoint(self):
        with self.assertRaisesRegex(m.SuiteError,'browser broke'):self.run_case(browser=lambda *a:(_ for _ in()).throw(m.SuiteError('browser broke')))
        checkpoint=json.loads((Path(self.directory.name)/'topology-adjacency-checkpoint.json').read_text())
        self.assertEqual((checkpoint['result'],checkpoint['stage'],len(checkpoint['failures'])),('incomplete','before_browser',4))


class AdjacencyBrowserRawTest(unittest.TestCase):
    def setUp(self):
        owner=AdjacencyOnlineTest();owner.setUp();self.addCleanup(owner.doCleanups);report=owner.run_case();self.evidence=e=report['browser'];output=e['outputs'][0];expected=e['expected']['result'];encoded=overlay_wire_and_projected(expected);fields=list(expected[0]['properties'])
        observation={'port':'result','output_item_id':30,'sources':[{'item_id':v['id'],'item':v['before']}for v in e['sources']],'output':output['before'],'graph':report['raw_lineage'],'lineage_page':report['raw_lineage'],'lineage_rendered':report['raw_lineage'],
          'lineage_canvas':{'rows':[{'text':n}for n in[e['sources'][0]['name'],output['name']]],'paths':[[[0,0],[1,1]]]},
          'preview':{'preview_type':'table','data':{'page':1,'total':5,'source_srid':3857,'source_crs':'EPSG:3857','transform_status':'not_transformed','preview_hint':'frontend_transform_required','geometry_columns':['geom'],'columns':['fid','geom',*fields],'rows':[{**row['properties'],'fid':i+1,'geom':wire}for i,(row,wire)in enumerate(zip(expected,encoded['wire']))]}},
          'decoded_rows':expected,'rendered':{'projection':'EPSG:3857','features':encoded['projected'],'painted_pixels':31,'shared_edge_pixels':1}}
        self.raw={'schema_version':'addp.polygon-adjacency-browser-raw/v1','case':e['case'],'run_id':e['run_id'],'execution_id':e['execution_id'],'result':'passed','stage':'complete','execution':AdjacencyClient().success,'observations':[observation],'publication_requests':[],'failures':[]}
        for failed in report['failures']:
            self.raw['failures'].append({'key':failed['key'],'execution_id':failed['execution_id'],'observations':[{'owner':o,'locale':l,'raw':failed['execution']if o=='develop'else{'execution':failed['monitor']}}for o in('develop','monitor')for l in('zh-cn','en')]})
        self.raw['failures']=frozen_literal_clipboards(self.raw['failures'])
    def test_adjacency_original_ewkb_native_geometry_one_edge_and_exact_bilingual_copies(self):m.validate_mixed_overlay_browser_raw(self.raw,self.evidence,m.fixture.ADJACENCY_CASE)
    def test_adjacency_browser_entry_requires_bound_original_raw_and_selects_only_this_case(self):
        import subprocess
        raw=self.raw;evidence=self.evidence
        with tempfile.TemporaryDirectory()as temp:
            env={**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.ADJACENCY_CASE,'ADDP_ONLINE_ARTIFACT_DIR':temp}
            original=subprocess.run
            def invoke(args,**kwargs):
                if args[0]!='npm':return original(args,**kwargs)
                self.assertEqual(args[args.index('--grep')+1],m.fixture.ADJACENCY_TITLE+'$')
                Path(temp,'topology-browser.json').write_text(json.dumps({**evidence,'schema_version':'addp.polygon-adjacency-browser/v1','result':'passed'}))
                Path(temp,'topology-adjacency-browser-raw.json').write_text(json.dumps(raw))
                return SimpleNamespace(returncode=0)
            with patch.object(m.subprocess,'run',side_effect=invoke):
                self.assertEqual(m.browser(Path('.'),env,evidence)['result'],'passed')
                raw['result']='incomplete'
                with self.assertRaises(m.SuiteError):m.browser(Path('.'),env,evidence)

    def test_adjacency_raw_rejects_missing_lines_tiny_precision_types_pixels_and_wrong_sources(self):
        changes=[lambda r:r.update(result='incomplete'),lambda r:r['observations'][0]['decoded_rows'][4]['geometry']['coordinates'][0][1].__setitem__(1,0),lambda r:r['observations'][0]['preview']['data']['rows'][0].update(source_id_a=90.),lambda r:r['observations'][0]['rendered']['features'].pop(),lambda r:r['observations'][0]['rendered'].update(shared_edge_pixels=0),lambda r:r['observations'][0]['graph']['edges'].pop(),lambda r:r['observations'][0]['graph']['edges'][0]['source'].update(item_id=11),lambda r:r['failures'][0]['observations'].pop(),lambda r:r['failures'][0]['observations'][0].update(clipboard='wrong')]
        for change in changes:
            raw=copy.deepcopy(self.raw);change(raw)
            with self.subTest(change=change),self.assertRaises(m.SuiteError):m.validate_mixed_overlay_browser_raw(raw,self.evidence,m.fixture.ADJACENCY_CASE)


class AdjacencyBrowserContractTest(unittest.TestCase):
    def test_adjacency_real_discovery_selects_one_case_and_fixed_messages_match_both_locales(self):
        import subprocess,os
        repo=Path(__file__).resolve().parents[2]
        script="""import assert from 'node:assert/strict';import{readFileSync}from'node:fs';const root=process.argv[1],source=readFileSync(root+'/console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8');
const start=source.indexOf('const inputDiagnosticText'),end=source.indexOf('async function assertInputDiagnosticPanel(',start),values=new Function(source.slice(start,end)+';return inputDiagnosticText')();
for(const locale of ['zh-cn','en']){const d=JSON.parse(readFileSync(root+'/common-frontend/basic/src/i18n/'+locale+'.json','utf8')).common.executionFailure,v=d.polygonAdjacency;
for(const[key,reason,param]of[['adjacency-id','operator_input_invalid','input_gdf'],['adjacency-empty','operator_input_invalid','input_gdf'],['adjacency-crs','operator_crs_invalid',''],['adjacency-overlap','operator_input_overlap','input_gdf']])assert.deepEqual(values[key][locale],[v.reasons[reason],d.operatorNames.check_polygon_adjacency,param?d.operatorParameters[param]:'','',v.actions[reason]]);}
assert.ok(source.includes('assertExtensionLineMap(renderer,expected,[2,1])'));assert.ok(source.includes('topology-adjacency-browser-raw.json'));"""
        result=subprocess.run(['node','--input-type=module','-e',script,str(repo)],capture_output=True,text=True);self.assertEqual(result.returncode,0,result.stderr)
        result=subprocess.run(['npm','exec','--','playwright','test','e2e/online/vector-topology-persistence.spec.js','--config=playwright.online.config.js','--grep',m.fixture.ADJACENCY_TITLE+'$','--list','--reporter=json'],cwd=repo/'console/frontend',env={**os.environ,**ENV,'ADDP_ONLINE_TOPOLOGY_CASE':m.fixture.ADJACENCY_CASE},capture_output=True,text=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        def specs(suites):return[v for suite in suites for v in[*suite.get('specs',[]),*specs(suite.get('suites',[]))]]
        self.assertEqual([v['title']for v in specs(json.loads(result.stdout)['suites'])],[m.fixture.ADJACENCY_TITLE])

    def test_adjacency_actual_line_map_and_source_graph_helpers_reject_forged_coordinates_and_pixels(self):
        import subprocess
        repo=Path(__file__).resolve().parents[2]
        script=r'''
import assert from 'node:assert/strict';import{readFileSync}from'node:fs';
const source=readFileSync(process.argv[1],'utf8'),expected=JSON.parse(process.argv[2]);
const {webMercatorPreviewCoordinate}=await import(process.argv[3]);
const a=source.indexOf('function normalizedMapExpectation('),b=source.indexOf('async function verifyNetworkCase(',a);
const expectation=new Function('webMercatorPreviewCoordinate',source.slice(a,b)+';return normalizedMapExpectation')(webMercatorPreviewCoordinate);
const start=source.indexOf('async function assertExtensionLineMap('),end=source.indexOf('function persistExtensionEvidence(',start);
const expect=value=>({toBe:x=>assert.equal(value,x),toHaveLength:x=>assert.equal(value.length,x),toEqual:x=>assert.deepEqual(value,x)});
expect.poll=fn=>({async toEqual(x){assert.deepEqual(await fn(),x)},async toBe(x){assert.equal(await fn(),x)}});
const check=new Function('expect','normalizedMapExpectation','webMercatorPreviewCoordinate',source.slice(start,end)+';return assertExtensionLineMap')(expect,expectation,webMercatorPreviewCoordinate);
const project=c=>typeof c[0]==='number'?webMercatorPreviewCoordinate(c):c.map(project);
async function run(rows,mode='normal'){
 const pixels=new Uint8ClampedArray(40*40*4);for(let x=0;x<36;x++)pixels.set([0,200,0,255],(10*40+x)*4);
 if(mode==='blank')pixels.fill(0);
 const features=rows.map(r=>({get:k=>r.properties[k],getGeometry:()=>({getType:()=>r.geometry.type,getCoordinates:()=>project(r.geometry.coordinates)})}));
 const context={canvas:{width:40,height:40,getBoundingClientRect:()=>({left:0,top:0,width:40,height:40})},getImageData:()=>({data:pixels})};
 const layer={getVisible:()=>mode!=='hidden',getOpacity:()=>1,getSource:()=>({getFeatures:()=>features}),getRenderer:()=>({context})};
 const map={getView:()=>({getAnimating:()=>false,getProjection:()=>({getCode:()=> 'EPSG:3857'})}),getLayers:()=>({getArray:()=>[layer]}),getPixelFromCoordinate:()=>[10,10],getViewport:()=>({getBoundingClientRect:()=>({left:0,top:0})})};
 await check({evaluate:async(fn,arg)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},arg)},expected,[2,1]);
}
await run(expected);await run([...expected].reverse());
for(const mode of['blank','hidden'])await assert.rejects(()=>run(expected,mode));
for(const mutate of[r=>r.pop(),r=>r[0].properties.source_id_a=10,r=>r[3].geometry.coordinates.pop(),r=>r[4].geometry.coordinates[0][1][0]=42]){const rows=structuredClone(expected);mutate(rows);await assert.rejects(()=>run(rows));}
const gs=source.indexOf('function assertProximityLineage('),ge=source.indexOf('function persistProximityEvidence(',gs);
const graphCheck=new Function('expect',source.slice(gs,ge)+';return assertProximityLineage')(expect);
const evidence={sources:[{id:10}],output:{item_id:30},execution_id:'current'},edge={relation_kind:'derive',status:'active',evidence:{execution_id:'current'},source:{item_id:10},target:{item_id:30}};
graphCheck({truncated:false,edges:[edge]},evidence);
for(const edges of[[],[edge,edge],[{...edge,source:{item_id:11}}]])assert.throws(()=>graphCheck({truncated:false,edges},evidence));
'''
        result=subprocess.run(['node','--input-type=module','-e',script,str(repo/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),json.dumps(m.fixture.adjacency_features('result')),(repo/'manager/frontend/e2e/geometry-preview-coordinate.js').as_uri()],capture_output=True,text=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)

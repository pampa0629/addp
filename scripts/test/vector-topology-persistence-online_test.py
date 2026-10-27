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
  const map={getView:()=>({getAnimating:()=>false,getProjection:()=>({getCode:()=> 'EPSG:3857'})}),
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
        self.executions={'normalize-execution':normalize_execution(),'normalize-failed':{'status':'failed','metadata':{}}}
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
        return m.run_scenario(Path('.'),self.env,client or NormalizeClient(),physical,browser,diagnostic_runner=lambda *a,**kw:{'cause':kw['cause']})
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
        self.assertTrue(result['failed_normalize']['no_publication_or_scan'])
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
 const features=rows.map(row=>({get:k=>row.properties[k],getGeometry:()=>({getType:()=>row.geometry.type,getCoordinates:()=>row.geometry.coordinates.map(webMercatorPreviewCoordinate)})}))
 const map={getView:()=>({getAnimating:()=>false,getProjection:()=>({getCode:()=>projection})}),getLayers:()=>({getArray:()=>[{getSource:()=>({getFeatures:()=>features})}]})}
 const renderer={evaluate:async(fn,args)=>fn({__vueParentComponent:{setupState:{mapInstance:map}}},args)}
 await check(renderer,expect,Object.keys(expected[0].properties),expected,normalizedMapExpectation)
}
await run(expected);await run([...expected].reverse())
const changes=[rows=>rows.pop(),rows=>rows.push(structuredClone(rows[0])),rows=>rows[0].properties['edge_id' in rows[0].properties?'edge_id':'chain_id']++,
 rows=>rows[0].geometry.coordinates.reverse(),rows=>rows[0].geometry.coordinates[0][0]+=1]
if('source_id' in expected[0].properties)changes.push(rows=>rows[0].properties.source_id='wrong',
 rows=>rows[0].properties.source_segment_no++,rows=>rows[0].properties.is_reversed=0,rows=>rows[5].properties.grade=0)
else changes.push(rows=>rows[0].properties['occurrence_count' in rows[0].properties?'occurrence_count':'member_count']++)
for(const change of changes){const rows=structuredClone(expected);change(rows);await assert.rejects(run(rows),{code:'ERR_ASSERTION'})}
await assert.rejects(run(expected,'EPSG:4326'),{code:'ERR_ASSERTION'})
'''
        for case,port in [(case,port) for case in (m.fixture.NORMALIZE_CASE,m.fixture.COMPOSITION_CASE) for port in m.fixture.network_profile(case)['ports']]:
            result=subprocess.run(['node','--input-type=module','-e',script,
                str(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js'),json.dumps(m.fixture.network_features(case,port)),
                (repository/'manager/frontend/e2e/geometry-preview-coordinate.js').as_uri()],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)


    def test_real_playwright_discovery_selects_exactly_one_case_without_running_business(self):
        import os
        repository=Path(__file__).resolve().parents[2]
        run=m.subprocess.run
        titles={m.fixture.NORMALIZE_CASE:'重合规整独立普通User双成果与字段拒绝',
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
            diagnostic_runner=lambda *a,**kw:self.assertEqual((kw['task'],kw['cause']),('chains','invalid_chain_id_field')) or {'cause':kw['cause']})
    def test_composition_exact_three_outputs_and_failure_barrier_without_old_scenarios(self):
        with patch.object(m,'definition',side_effect=AssertionError('old full must not run')):
            report=self.run_case()
        self.assertEqual(report['schema_version'],'addp.topology-normalize-chains/v1')
        self.assertEqual([o['port'] for o in report['outputs']],list(m.fixture.COMPOSITION_PORTS))
        self.assertEqual([len(report['browser']['expected'][p]) for p in m.fixture.COMPOSITION_PORTS],[9,15,22])
        self.assertEqual(len(report['automatic_scan_observations']),3)
        self.assertTrue(report['failed_normalize']['no_publication_or_scan'])
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

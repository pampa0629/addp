import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { buildDevelopQueryExecutionRoute, executionFailurePresentation, failureStages, failureReasons, operatorInputRules } from '../src/utils/executionFailure.mjs'

const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8')

test('failure diagnostics use only closed shared vocabulary in both languages', () => {
  const backend = read('../../../common/execution/failure_diagnostic.go')
  const backendStages = [...backend.split('var failureStages =')[1].split('var failureReasons =')[0].matchAll(/"([a-z_]+)":\s*true/g)].map(item => item[1])
  const backendReasons = [...backend.split('var failureReasons =')[1].split('func NewFailureDiagnostic')[0].matchAll(/"([a-z_]+)":\s*true/g)].map(item => item[1])
  assert.deepEqual([...failureStages].sort(), backendStages.sort())
  assert.deepEqual([...failureReasons].sort(), backendReasons.sort())
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    for (const stage of failureStages) assert.ok(messages.stages[stage])
    for (const reason of failureReasons) {
      assert.ok(messages.reasons[reason])
      assert.ok(messages.actions[reason])
    }
  }
  const presentation = executionFailurePresentation({ code: 'develop.query.snapshot_invalid', message: 'secret runtime body',
    diagnostic: { schema_version: 'addp.execution-failure/v3', stage: 'validation', reason: 'query_template_invalid' } }, key => key)
  assert.equal(presentation.stage, 'common.executionFailure.stages.validation')
  assert.equal(presentation.reason, 'common.executionFailure.reasons.query_template_invalid')
  assert.equal(presentation.code, 'develop.query.snapshot_invalid')
  assert.ok(!JSON.stringify(presentation).includes('secret'))
})

test('unknown or free text diagnostics show explicit missing evidence without runtime text', () => {
  for (const diagnostic of [undefined, { schema_version: 'v2', stage: 'validation', reason: 'query_template_invalid' },
    { schema_version: 'addp.execution-failure/v3', stage: 'secret', reason: 'query_failed' },
    { schema_version: 'addp.execution-failure/v3', stage: 'validation', reason: 'query_template_invalid', message: 'business-secret' }]) {
    const result = executionFailurePresentation({ diagnostic, category: 'unknown', code: 'secret-value', message: 'private' }, key => key)
    assert.equal(result.stage, '')
    assert.equal(result.reason, 'common.executionFailure.execution_failed')
    assert.equal(result.action, 'common.executionFailure.unrecordedAction')
    assert.equal(result.code, '')
  }
})

test('existing orchestration failure categories retain their specific explanation', () => {
  for (const category of ['submission_uncertain', 'coordinator_lost', 'child_failed', 'owner_unavailable']) {
    const result = executionFailurePresentation({ category, code: 'orchestrator.' + category }, key => key)
    assert.equal(result.reason, `common.executionFailure.${category}`)
  }
})

test('SQL failure facts accept only bounded identifiers and coordinates under the single current contract', () => {
  const translate = (key, params) => params ? `${key}:${JSON.stringify(params)}` : key
  const base = { schema_version: 'addp.execution-failure/v3', stage: 'preparation', reason: 'table_not_found' }
  const result = executionFailurePresentation({ diagnostic: { ...base, sql: { object: { kind: 'table', name: 'public.farmla1nd' }, position: { line: 2, column: 15 } } } }, translate)
  assert.match(result.object, /public.farmla1nd/)
  assert.match(result.position, /"line":2/)
  for (const sql of [{ object: { kind: 'message', name: 'private' } }, { object: { kind: 'table', name: 'a\nprivate' } },
    { object: { kind: 'table', name: 'a', message: 'private' } }, { position: { line: 1, column: 0 } },
    { position: { line: 1, column: 1, detail: 'private' } }, { object: { kind: 'table', name: '\ud800' } }]) {
    const invalid = executionFailurePresentation({ diagnostic: { ...base, sql } }, translate)
    assert.equal(invalid.object, '')
    assert.equal(invalid.position, '')
  }
  assert.equal(executionFailurePresentation({ diagnostic: { ...base, schema_version: 'addp.execution-failure/v1' } }, translate).stage, '')
})

test('query execution navigation works without a task definition and does not guess other owners', () => {
  assert.equal(buildDevelopQueryExecutionRoute({ module: 'develop', task_type: 'query', execution_id: 'execution/id' }), '/develop/executions/execution%2Fid')
  for (const input of [{ module: 'develop', task_type: 'workflow', execution_id: 'id' }, { module: 'meta', task_type: 'query', execution_id: 'id' }, {}]) {
    assert.equal(buildDevelopQueryExecutionRoute(input), '')
  }
})

test('write diagnostics show only proven write column and independent target risk', () => {
  const translate = (key, params) => params ? `${key}:${JSON.stringify(params)}` : key
  const base = { schema_version: 'addp.execution-failure/v3', stage: 'execution', reason: 'json_invalid' }
  const result = executionFailurePresentation({ diagnostic: { ...base, write: { column_index: 3, declared_type: 'json' }, target_change_possible: true }, message: 'private' }, translate)
  assert.match(result.write, /"column_index":3/)
  assert.equal(result.targetWarning, 'common.executionFailure.targetChangePossible')
  assert.doesNotMatch(JSON.stringify(result), /private/)
  for (const extra of [{ write: null }, { write: { column_index: 0, declared_type: 'json' } },
    { write: { column_index: 65536, declared_type: 'json' } }, { write: { column_index: 1.5, declared_type: 'json' } },
    { write: { column_index: 3, declared_type: 'bytes' } }, { write: { column_index: 3, declared_type: 'json', value: 'private' } },
    { target_change_possible: false }, { target_change_possible: 'true' }, { target_change_possible: null },
    { stage: 'validation', target_change_possible: true }, { schema_version: 'addp.execution-failure/v2' }]) {
    const invalid = executionFailurePresentation({ diagnostic: { ...base, ...extra } }, translate)
    assert.equal(invalid.write, '')
    assert.equal(invalid.targetWarning, '')
    assert.equal(invalid.stage, '')
  }
  const native = executionFailurePresentation({ diagnostic: { ...base, reason: 'table_write_failed', target_change_possible: true } }, translate)
  assert.equal(native.write, '')
  assert.ok(native.targetWarning)
  assert.equal(executionFailurePresentation({ diagnostic: base }, translate).targetWarning, '')
})

test('shared ExecutionFailure is the sole failure panel for Workbench, Owner and Monitor', () => {
  for (const path of ['../../../develop/frontend/src/components/QueryResult.vue', '../../../develop/frontend/src/views/ExecutionDetail.vue', '../../../monitor/frontend/src/views/ExecutionList.vue']) {
    const source = read(path)
    assert.equal((source.match(/<ExecutionFailure\b/g) || []).length, 1)
    assert.doesNotMatch(source, /executionErrorSummary|executionErrorMessage|error-traceback|name="error"/)
  }
})

test('protection refusals remain distinct from data permissions without exposing runtime text', () => {
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const protectionText = messages.reasons.data_protection_required + messages.actions.data_protection_required
    assert.match(messages.reasons.data_protection_required, locale === 'zh-cn' ? /数据脱敏保护校验/ : /data masking protection checks/)
    assert.doesNotMatch(protectionText, /访问资格|读取权限|写入权限|access eligibility|read permission|write permission/i)
    assert.notEqual(messages.reasons.data_protection_required, messages.reasons.permission_denied)
    assert.notEqual(messages.reasons.data_protection_required, messages.reasons.authorization_invalid)
    assert.doesNotMatch(messages.reasons.data_protection_required, /查询|\bquery\b/i)
    for (const stage of ['preparation', 'execution']) assert.doesNotMatch(messages.stages[stage], /查询|\bquery\b/i)
    assert.doesNotMatch(messages.actions.data_protection_required, /SQL|字段别名|column aliases/i)
  }
  for (const stage of ['preparation', 'result']) {
    const presentation = executionFailurePresentation({ code: 'develop.query.protection_required',
      category: 'permission_denied', message: 'private-value SELECT phone',
      diagnostic: { schema_version: 'addp.execution-failure/v3', stage, reason: 'data_protection_required' }
    }, key => key)
    assert.equal(presentation.stage, `common.executionFailure.stages.${stage}`)
    assert.equal(presentation.reason, 'common.executionFailure.reasons.data_protection_required')
    assert.equal(presentation.action, 'common.executionFailure.actions.data_protection_required')
    assert.doesNotMatch(JSON.stringify(presentation), /private-value|SELECT/)
  }
  for (const reason of ['permission_denied', 'authorization_invalid']) {
    const presentation = executionFailurePresentation({ category: 'execution_failed',
      diagnostic: { schema_version: 'addp.execution-failure/v3', stage: 'authorization', reason }
    }, key => key)
    assert.equal(presentation.reason, `common.executionFailure.reasons.${reason}`)
    assert.equal(presentation.action, `common.executionFailure.actions.${reason}`)
  }
})


test('operator input contract matches the canonical runtime tuples and rejects unproven fields', () => {
  const source = read('../../../common-python/addp_common/workflow_runtime/errors.py')
  const canonical = [...source.matchAll(/\('([^']+)', '([^']+)', '([^']*)'\): '([^']*)'/g)].map(match => [match[1],match[2],match[4],match[3]])
  assert.equal(canonical.length, 185)
  const actual = operatorInputRules.map(rule => [...rule])
  assert.deepEqual(actual.sort(), canonical.sort())
  for (const [operator_id, reason, parameter, field] of canonical) {
    const operator = { node_index: 2, operator_id, ...(parameter ? { parameter } : {}), ...(field ? { field } : {}) }
    const diagnostic = { schema_version: 'addp.execution-failure/v3', stage: 'execution', reason, operator }
    const result = executionFailurePresentation({ diagnostic, message: 'private' }, key => key)
    const profiles = { snap_line_endpoints: 'lineEndpoint', extend_line_endpoints: 'lineEndpoint', polygonize: 'polygonize', snap_geometry:'referenceSnap', check_geometry:'geometryValidity', repair_geometry:'geometryValidity', extract_vertices:'vertices', split_multipart:'multipart', get_bounds:'bounds', distance_to:'distance', get_area:'measurement', get_length:'measurement', split_by_area:'measurement', contains:'spatialRelation', intersects:'spatialRelation', voronoi:'voronoi',
      vector_reproject:'vectorReproject', filter_by_geometry_type:'geometrySelection', drop_null_geometry:'geometrySelection',
      load_from_wkt:'wktLoad', export_to_wkt:'wktExport',
      'extract_polygon_rings:operator_input_invalid':'polygonRings',
      'merge_line_chains:operator_input_invalid':'lineCheck', 'merge_line_chains:operator_crs_invalid':'lineCheck',
      'merge_line_chains:operator_endpoint_network_invalid':'lineCheck',
      'normalize_line_network:operator_input_invalid':'lineCheck', 'normalize_line_network:operator_crs_invalid':'lineCheck',
      'group_line_components:operator_input_invalid':'lineCheck', 'group_line_components:operator_crs_invalid':'lineCheck',
      'split_line_intersections:operator_input_invalid':'lineCheck', 'split_line_intersections:operator_crs_invalid':'lineCheck',
      'split_line_intersections:operator_enum_invalid':'lineSplit', 'split_line_intersections:operator_input_overlap':'lineSplit',
      'split_line_intersections:operator_source_topology_invalid':'lineSplit',
      check_line_overlap:'lineCheck', check_line_self_overlap:'lineCheck',
      check_dangling_points:'lineCheck', check_line_intersection:'lineCheck', check_line_self_intersection:'lineCheck',
      check_polygon_adjacency:'polygonAdjacency', check_polygon_overlap:'polygonOverlap', check_polygon_gap:'polygonGap',
      envelope:'construction', centroid:'construction', convex_hull:'construction', minimum_rotated_rectangle:'construction',
      representative_point:'interiorPoint',
      'buffer:operator_input_invalid':'buffer', 'buffer:operator_crs_invalid':'buffer', 'batch_buffer:operator_crs_invalid':'buffer',
      'simplify:operator_input_invalid':'simplify', 'simplify:operator_crs_invalid':'simplify' }
    const profile = profiles[`${operator_id}:${reason}`] || profiles[operator_id]
    const templates = `common.executionFailure${profile ? `.${profile}` : ''}`
    assert.equal(result.reason, `${templates}.reasons.${reason}`)
    assert.ok(result.node)
    assert.equal(result.field, field)
    for (const bad of [
      { ...diagnostic, stage: 'validation' }, { ...diagnostic, operator: null },
      { ...diagnostic, operator: { ...operator, node_index: true } },
      { ...diagnostic, operator: { ...operator, node_index: 65536 } },
      { ...diagnostic, operator: { ...operator, node_id: 'private' } },
      { ...diagnostic, operator: { ...operator, parameter: null } },
      { ...diagnostic, operator: { ...operator, field: 'password' } },
      { ...diagnostic, sql: { position: { line: 1, column: 1 } } },
      { ...diagnostic, reason: 'execution_failed' }
    ]) {
      const rejected = executionFailurePresentation({ diagnostic: bad, message: 'private' }, key => key)
      assert.equal(rejected.operator, '')
      assert.equal(rejected.stage, '')
      assert.doesNotMatch(JSON.stringify(rejected), /private|password/)
    }
  }
})

test('controlled input messages distinguish ordinary and active geometry conflicts in both languages', () => {
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate = (key, params = {}) => key.replace('common.executionFailure.', '').split('.').reduce((node, part) => node[part], messages)
      .replace(/\{(\w+)\}/g, (_, key) => params[key])
    for (const [operator_id, reason] of [['batch_buffer','operator_field_conflict'], ['distance_to','operator_geometry_field_conflict']]) {
      const presentation = executionFailurePresentation({ diagnostic: { schema_version:'addp.execution-failure/v3',stage:'execution',reason,
        operator:{node_index:2, operator_id,parameter:'input_gdf',field:'distance'} },message:'private' }, translate)
      assert.match(presentation.reason, /distance/)
      assert.doesNotMatch(presentation.reason, /distances/)
      assert.match(presentation.node, /2/)
      assert.match(presentation.action, locale === 'zh-cn' ? /重命名/ : /[Rr]ename/)
      assert.doesNotMatch(JSON.stringify(presentation), /private/)
    }
    assert.notEqual(messages.reasons.operator_field_conflict, messages.reasons.operator_geometry_field_conflict)
  }
})

test('ring diagnostics provide controlled bilingual reasons and explicit repair guidance', () => {
  for (const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    for (const [operator_id, reason, parameter, field] of operatorInputRules.filter(([name]) => name === 'extract_polygon_rings')) {
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,parameter,...(field?{field}:{})}}
      const presentation=executionFailurePresentation({diagnostic,message:'private GEOS /path source-id'},translate)
      assert.equal(presentation.operator,locale==='zh-cn'?'面界提取':'Extract polygon rings')
      if(field) { assert.ok(presentation.reason.includes(field));assert.match(presentation.action,locale==='zh-cn'?/重命名/:/[Rr]ename/) }
      else if (reason === 'operator_geometry_invalid') {
        assert.match(presentation.reason,locale==='zh-cn'?/未通过有效性检查/:/failed the validity check/)
        assert.match(presentation.action,locale==='zh-cn'?/显式修复/:/explicitly repair/)
        assert.equal(presentation.field,'')
      } else {
        assert.equal(presentation.reason, messages.polygonRings.reasons.operator_input_invalid)
        assert.match(presentation.action, /Polygon.*id_field/)
        assert.equal(presentation.field, '')
      }
      assert.doesNotMatch(JSON.stringify(presentation),/private|GEOS|source-id/)
    }
  }
})

test('line trimming uses closed bilingual input facts and never publishes numerical error text', () => {
  for (const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    for (const [operator_id,reason,parameter,field] of operatorInputRules.filter(([name])=>name==='trim_line_endpoints')) {
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:1,operator_id,...(parameter?{parameter}:{})}}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE GEOS coordinates'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'线端修剪':'Line endpoint trimming')
      assert.equal(got.field,'');assert.ok(got.reason && got.action)
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE|GEOS|coordinates/)
      if(parameter) assert.ok(got.parameter.includes(parameter))
      const bad={...diagnostic,operator:{...diagnostic.operator,field:'distance'}}
      assert.equal(executionFailurePresentation({diagnostic:bad},translate).stage,'')
    }
  }
})


test('intersection overlay presents closed admissions in both languages without arbitrary fields', () => {
  for (const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    const rules=operatorInputRules.filter(([name])=>name==='intersection')
    assert.equal(rules.length,4)
    for (const [operator_id,reason,parameter] of rules) {
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{})}}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE field_id source GEOS'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'相交叠加':'Intersection overlay')
      assert.ok(got.reason&&got.action&&got.node)
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE|field_id|GEOS/)
      if(parameter==='gdf_b')assert.ok(got.parameter.includes('gdf_b'))
      for (const patch of [{field:'source_id_a'},{field:'PRIVATE_FIELD'},{parameter:'reference_gdf'},{operator_id:'unknown_construction'}]) {
        const bad={...diagnostic,operator:{...diagnostic.operator,...patch}}
        assert.equal(executionFailurePresentation({diagnostic:bad},translate).stage,'')
      }
    }
  }
})

test('node extraction presents all closed input facts bilingually without source data', () => {
  for (const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    const rules=operatorInputRules.filter(([name])=>name==='extract_line_nodes')
    assert.equal(rules.length,8)
    for (const [operator_id,reason,parameter,field] of rules) {
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE GEOS /source/path 123'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'节点提取':'Extract line nodes')
      assert.ok(got.reason && got.action)
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE|GEOS|source\/path/)
      if(field)assert.ok(got.reason.includes(field))
      if(reason==='operator_endpoint_network_invalid')assert.match(got.reason,locale==='zh-cn'?/双方端点/:/both endpoints/)
      const bad={...diagnostic,operator:{...diagnostic.operator,field:'PRIVATE_FIELD'}}
      assert.equal(executionFailurePresentation({diagnostic:bad},translate).stage,'')
    }
  }
})


test('difference presents three closed admissions bilingually without B identifier requirements', () => {
  for (const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    const rules=operatorInputRules.filter(([name])=>name==='difference')
    assert.equal(rules.length,3)
    for (const [operator_id,reason,parameter] of rules) {
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{})}}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE field_id GEOS /source/path'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'几何差集':'Geometry difference')
      assert.ok(got.reason&&got.action&&got.node)
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE|field_id|GEOS|source\/path/)
      if(reason==='operator_mask_invalid'){
        assert.match(got.reason,locale==='zh-cn'?/面/:/Polygon/)
        assert.match(got.action,locale==='zh-cn'?/无需指定范围图层/:/no business identifier/)
      }
      for (const patch of [{field:'source_id'},{field:'PRIVATE'},{parameter:'reference_gdf'},{operator_id:'unknown_construction'}]) {
        const bad={...diagnostic,operator:{...diagnostic.operator,...patch}}
        assert.equal(executionFailurePresentation({diagnostic:bad},translate).stage,'')
      }
    }
  }
})


test('clip presents three closed admissions bilingually without B identifier requirements', () => {
  for (const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    const rules=operatorInputRules.filter(([name])=>name==='clip')
    assert.equal(rules.length,3)
    for (const [operator_id,reason,parameter] of rules) {
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{})}}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE field_id GEOS /source/path'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'裁剪几何':'Clip geometry')
      assert.ok(got.reason&&got.action&&got.node)
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE|field_id|GEOS|source\/path/)
      if(parameter==='mask_gdf')assert.match(got.parameter,/mask_gdf/)
      if(reason==='operator_mask_invalid'){
        assert.match(got.reason,locale==='zh-cn'?/面/:/Polygon/)
        assert.match(got.action,locale==='zh-cn'?/无需指定范围图层/:/no business identifier/)
      }
      for (const patch of [{field:'source_id'},{field:'PRIVATE'},{parameter:'reference_gdf'},{operator_id:'unknown_construction'}]) {
        const bad={...diagnostic,operator:{...diagnostic.operator,...patch}}
        assert.equal(executionFailurePresentation({diagnostic:bad},translate).stage,'')
      }
    }
  }
})


test('union presents six closed admissions and side-specific overlap remedies bilingually', () => {
  for (const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    const rules=operatorInputRules.filter(([op])=>op==='union'); assert.equal(rules.length,6)
    for (const [operator_id,reason,parameter] of rules) {
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{})}}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE GEOS source values'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'联合叠加':'Union overlay')
      assert.ok(got.reason&&got.action&&got.node)
      if(reason.endsWith('_overlap')) {
        assert.match(got.reason,locale==='zh-cn'?/正面积重叠/:/positive-area overlaps/)
        assert.match(got.action,locale==='zh-cn'?/面叠检查/:/Check polygon overlaps/)
        assert.ok(got.parameter)
      }
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE|GEOS/)
      for (const patch of [{field:'source_id_a'},{field:'PRIVATE'},{parameter:'mask_gdf'},{operator_id:'unknown_construction'}]) {
        assert.equal(executionFailurePresentation({diagnostic:{...diagnostic,operator:{...diagnostic.operator,...patch}}},translate).stage,'')
      }
    }
  }
})


test('dissolve has four closed bilingual input presentations and no private values',()=>{
  for(const locale of ['zh-cn','en']){
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    const rules=operatorInputRules.filter(([op])=>op==='dissolve');assert.equal(rules.length,4)
    for(const [operator_id,reason,parameter]of rules){
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:1,operator_id,...(parameter?{parameter}:{})}}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'融合几何':'Dissolve geometry');assert.ok(got.reason&&got.action);assert.doesNotMatch(JSON.stringify(got),/PRIVATE/)
      assert.equal(executionFailurePresentation({diagnostic:{...diagnostic,operator:{...diagnostic.operator,field:'source_id'}}},translate).stage,'')
    }
  }
})


test('relation filters use precise geographic-or-projected guidance and keep other operators unchanged', () => {
  for (const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    for (const operator_id of ['contains','intersects']) {
      const rules=operatorInputRules.filter(([name])=>name===operator_id)
      assert.equal(rules.length,3)
      for (const [,reason,parameter] of rules) {
        const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{})}}
        const got=executionFailurePresentation({diagnostic,message:'PRIVATE GEOS fields'},translate)
        assert.equal(got.operator,messages.operatorNames[operator_id])
        assert.equal(got.reason,messages.spatialRelation.reasons[reason])
        assert.equal(got.action,messages.spatialRelation.actions[reason])
        assert.doesNotMatch(JSON.stringify(got),/PRIVATE|GEOS|唯一标识|unique.*identifier/)
        if(reason==='operator_crs_invalid')assert.match(got.reason,locale==='zh-cn'?/地理或投影/:/geographic or projected/)
        for(const patch of [{field:'contains'},{field:'PRIVATE'},{parameter:'reference_gdf'},{operator_id:'unknown_construction'}]) {
          const bad={...diagnostic,operator:{...diagnostic.operator,...patch}}
          assert.equal(executionFailurePresentation({diagnostic:bad},translate).stage,'')
        }
      }
    }
    const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason:'operator_crs_invalid',operator:{node_index:2,operator_id:'intersection'}}
    const got=executionFailurePresentation({diagnostic},translate)
    assert.equal(got.reason,messages.reasons.operator_crs_invalid)
    assert.equal(got.action,messages.actions.operator_crs_invalid)
  }
})


test('voronoi explains explicit coverage, unique station positions and projected units bilingually', () => {
  for(const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    const rules=operatorInputRules.filter(([name])=>name==='voronoi');assert.equal(rules.length,3)
    for(const [operator_id,reason,parameter]of rules) {
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{})}}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE station_id XY coordinates'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'泰森多边形':'Voronoi polygons')
      assert.equal(got.reason,messages.voronoi.reasons[reason]);assert.equal(got.action,messages.voronoi.actions[reason])
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE|station_id|coordinates/)
      if(reason==='operator_input_invalid')assert.match(got.action,locale==='zh-cn'?/重复 XY/:/duplicate XY/)
      if(reason==='operator_mask_invalid')assert.ok(got.parameter.includes('mask_gdf'))
      if(reason==='operator_crs_invalid')assert.match(got.action,locale==='zh-cn'?/投影坐标系/:/projected CRSs/)
      for(const patch of [{field:'station'},{field:'PRIVATE'},{parameter:'gdf_b'},{operator_id:'unknown_construction'}]) {
        assert.equal(executionFailurePresentation({diagnostic:{...diagnostic,operator:{...diagnostic.operator,...patch}}},translate).stage,'')
      }
    }
  }
})

test('buffer diagnostics accept four closed facts and signed finite distances without private data', () => {
  const expected = [
    ['buffer','operator_input_invalid','input_gdf',''],
    ['buffer','operator_crs_invalid','',''],
    ['buffer','operator_number_invalid','distance',''],
    ['buffer','operator_positive_integer_required','resolution','']
  ]
  assert.deepEqual(operatorInputRules.filter(([name]) => name === 'buffer'), expected)
  assert.equal(operatorInputRules.filter(([name,reason]) => name === 'batch_buffer' && reason === 'operator_crs_invalid').length, 1)
  for (const locale of ['zh-cn','en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate = (key,params={}) => key.replace('common.executionFailure.','').split('.').reduce((node,part) => node[part],messages).replace(/\{(\w+)\}/g,(_,key) => params[key])
    for (const [operator_id,reason,parameter] of [...expected,['batch_buffer','operator_crs_invalid','','']]) {
      const diagnostic = { schema_version:'addp.execution-failure/v3', stage:'execution', reason,
        operator:{ node_index:2, operator_id, ...(parameter ? { parameter } : {}) } }
      const got = executionFailurePresentation({ diagnostic, message:'PRIVATE_FIELD private-password /private/input.gpkg' },translate)
      assert.equal(got.operator, operator_id === 'buffer' ? (locale === 'zh-cn' ? '缓冲区分析' : 'Buffer analysis') : (locale === 'zh-cn' ? '多距缓冲' : 'Multi-distance buffer'))
      assert.ok(got.reason && got.action && got.node)
      assert.equal(got.field,'')
      assert.equal(got.parameter, parameter ? messages.operatorParameters[parameter] : '')
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE_FIELD|private-password|\/private\/input\.gpkg/)
      if (reason === 'operator_number_invalid') {
        assert.match(got.reason, locale === 'zh-cn' ? /正数、零或负数/ : /positive, zero or negative/)
        assert.doesNotMatch(got.reason, locale === 'zh-cn' ? /有限正数|正整数|数组/ : /finite positive|positive integer|array/)
      }
      if (reason === 'operator_crs_invalid') {
        assert.equal(got.reason,messages.buffer.reasons[reason])
        assert.equal(got.action,messages.buffer.actions[reason])
        assert.match(got.reason, locale === 'zh-cn' ? /投影坐标系/ : /projected CRS/)
        assert.doesNotMatch(`${got.reason} ${got.action}`,/两个图层|Both layers/)
      }
      if (operator_id === 'buffer' && reason === 'operator_input_invalid') {
        assert.equal(got.reason,messages.buffer.reasons[reason])
        assert.equal(got.action,messages.buffer.actions[reason])
      }
      for (const patch of [{ field:'distance' },{ field:'PRIVATE_FIELD' },{ parameter:'reference_gdf' },{ operator_id:'unknown_construction' }]) {
        const rejected = executionFailurePresentation({ diagnostic:{ ...diagnostic, operator:{ ...diagnostic.operator, ...patch } }, message:'private-password' },translate)
        assert.equal(rejected.stage,'')
        assert.equal(rejected.operator,'')
        assert.doesNotMatch(JSON.stringify(rejected),/PRIVATE_FIELD|private-password/)
      }
    }
    for (const reason of ['operator_positive_number_required','operator_number_array_invalid','operator_geometry_field_conflict']) {
      const rejected = executionFailurePresentation({ diagnostic:{ schema_version:'addp.execution-failure/v3', stage:'execution', reason,
        operator:{ node_index:2, operator_id:'buffer', parameter:'distance' } } },translate)
      assert.equal(rejected.stage,'')
    }
  }
})

test('interior point diagnostics accept only two polygon-source facts without aliases or private values', () => {
  const expected = [
    ['representative_point','operator_input_invalid','input_gdf',''],
    ['representative_point','operator_crs_invalid','','']
  ]
  assert.deepEqual(operatorInputRules.filter(([name])=>name==='representative_point'),expected)
  assert.deepEqual(operatorInputRules.filter(([name])=>name==='point_on_surface'),[])
  for(const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    for(const [operator_id,reason,parameter] of expected) {
      const operator={node_index:2,operator_id,...(parameter?{parameter}:{})}
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'面内点':'Interior point')
      assert.equal(got.reason,messages.interiorPoint.reasons[reason])
      assert.equal(got.action,messages.interiorPoint.actions[reason])
      assert.equal(got.parameter,parameter?messages.operatorParameters[parameter]:'')
      assert.equal(got.field,'')
      assert.ok(got.node)
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE_BIZ_FIELD|private-password|\/private\/input\.gpkg/)
      assert.doesNotMatch(`${got.reason} ${got.action}`,/两个图层|Both layers|标识|identifier|buffer|缓冲|tolerance|容差/i)
      if(reason==='operator_input_invalid') {
        assert.notEqual(got.reason,messages.construction.reasons[reason])
        assert.match(got.reason,/Polygon.*MultiPolygon/)
        assert.match(got.reason,locale==='zh-cn'?/唯一活动几何列.*有效.*非空.*有限二维 XY/:/one active geometry column.*valid non-empty finite XY/)
        assert.match(got.action,/Z\/M/)
      } else {
        assert.match(got.reason,locale==='zh-cn'?/已知投影坐标系.*线性单位.*有限.*正/:/known projected CRS.*matching finite positive horizontal linear units/)
        assert.match(got.action,locale==='zh-cn'?/不会自动投影/:/no automatic projection/)
      }
      for(const patch of [{field:'source_id'},{field:'PRIVATE_BIZ_FIELD'},{parameter:'distance'},{parameter:'reference_gdf'},{parameter_value:'private-password'},{operator_id:'point_on_surface'},{operator_id:'unknown_construction'},{node_index:true}]) {
        const rejected=executionFailurePresentation({diagnostic:{...diagnostic,operator:{...operator,...patch}},message:'private-password'},translate)
        assert.equal(rejected.stage,'')
        assert.equal(rejected.operator,'')
        assert.doesNotMatch(JSON.stringify(rejected),/PRIVATE_BIZ_FIELD|private-password/)
      }
      for(const reason of ['operator_geometry_invalid','operator_field_conflict','operator_positive_number_required']) {
        assert.equal(executionFailurePresentation({diagnostic:{...diagnostic,reason}},translate).stage,'')
      }
    }
  }
})

test('construction diagnostics close eight facts with a shared single-source profile and no private values', () => {
  const expected = ['envelope','centroid','convex_hull','minimum_rotated_rectangle'].flatMap(name => [
    [name,'operator_input_invalid','input_gdf',''],
    [name,'operator_crs_invalid','','']
  ])
  assert.deepEqual(operatorInputRules.filter(([name]) => ['envelope','centroid','convex_hull','minimum_rotated_rectangle'].includes(name)),expected)
  assert.deepEqual(operatorInputRules.filter(([name]) => name==='centroid'),[
    ['centroid','operator_input_invalid','input_gdf',''],
    ['centroid','operator_crs_invalid','','']
  ])
  for (const locale of ['zh-cn','en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate = (key,params={}) => key.replace('common.executionFailure.','').split('.').reduce((node,part) => node[part],messages).replace(/\{(\w+)\}/g,(_,key) => params[key])
    for (const [operator_id,reason,parameter] of expected) {
      const operator = { node_index:2,operator_id,...(parameter?{parameter}:{}) }
      const diagnostic = { schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator }
      const got = executionFailurePresentation({ diagnostic,message:'private-password /private/input.gpkg PRIVATE_BIZ_FIELD' },translate)
      assert.equal(got.operator,messages.operatorNames[operator_id])
      assert.equal(got.reason,messages.construction.reasons[reason])
      assert.equal(got.action,messages.construction.actions[reason])
      assert.equal(got.parameter,parameter ? messages.operatorParameters[parameter] : '')
      assert.equal(got.field,'')
      assert.ok(got.node)
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE_BIZ_FIELD|private-password|\/private\/input\.gpkg/)
      assert.doesNotMatch(`${got.reason} ${got.action}`,/两个图层|Both layers|标识|identifier|buffer|缓冲|tolerance|容差/i)
      if (reason==='operator_input_invalid') {
        assert.match(got.reason,locale==='zh-cn'?/唯一活动几何列.*非空.*有效.*有限.*二维 XY/:/one active geometry column.*valid non-empty finite XY/)
        assert.match(got.action,/Z\/M/)
      } else {
        assert.match(got.reason,locale==='zh-cn'?/已知投影坐标系.*线性单位.*有限.*正/:/known projected CRS.*matching finite positive horizontal linear units/)
        assert.match(got.action,locale==='zh-cn'?/不会自动投影/:/no automatic projection/)
      }
      for (const patch of [{field:'source_id'},{field:'PRIVATE_BIZ_FIELD'},{parameter:'distance'},{parameter:'preserve_topology'},{parameter_value:'private-password'},{operator_id:'unknown_construction'},{node_index:true}]) {
        const rejected = executionFailurePresentation({diagnostic:{...diagnostic,operator:{...operator,...patch}},message:'private-password'},translate)
        assert.equal(rejected.stage,'')
        assert.equal(rejected.operator,'')
        assert.doesNotMatch(JSON.stringify(rejected),/PRIVATE_BIZ_FIELD|private-password/)
      }
      for (const legacyReason of ['operator_geometry_invalid','operator_field_conflict','operator_nonnegative_number_required']) {
        const rejected = executionFailurePresentation({diagnostic:{...diagnostic,reason:legacyReason}},translate)
        assert.equal(rejected.stage,'')
      }
    }
  }
})

test('simplify diagnostics close four facts without borrowing buffer, identifiers or two-layer guidance', () => {
  const expected = [
    ['simplify','operator_input_invalid','input_gdf',''],
    ['simplify','operator_crs_invalid','',''],
    ['simplify','operator_nonnegative_number_required','tolerance',''],
    ['simplify','operator_boolean_required','preserve_topology','']
  ]
  assert.deepEqual(operatorInputRules.filter(([name]) => name === 'simplify'),expected)
  for (const locale of ['zh-cn','en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate = (key,params={}) => key.replace('common.executionFailure.','').split('.').reduce((node,part) => node[part],messages).replace(/\{(\w+)\}/g,(_,key) => params[key])
    for (const [operator_id,reason,parameter] of expected) {
      const diagnostic = { schema_version:'addp.execution-failure/v3', stage:'execution', reason,
        operator:{ node_index:2, operator_id, ...(parameter ? { parameter } : {}) } }
      const got = executionFailurePresentation({ diagnostic, message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg' },translate)
      const profile = ['operator_input_invalid','operator_crs_invalid'].includes(reason) ? messages.simplify : messages
      assert.equal(got.operator,locale === 'zh-cn' ? '简化几何' : 'Simplify geometry')
      assert.equal(got.reason,profile.reasons[reason])
      assert.equal(got.action,profile.actions[reason])
      assert.equal(got.field,'')
      assert.equal(got.parameter,parameter ? messages.operatorParameters[parameter] : '')
      assert.ok(got.node)
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE_BIZ_FIELD|private-password|\/private\/input\.gpkg/)
      if (reason === 'operator_input_invalid' || reason === 'operator_crs_invalid') {
        assert.doesNotMatch(`${got.reason} ${got.action}`,/两个图层|Both layers|标识|identifier|buffer|缓冲/i)
      }
      if (reason === 'operator_crs_invalid') assert.match(got.reason,locale === 'zh-cn' ? /投影坐标系/ : /projected CRS/)
      if (reason === 'operator_nonnegative_number_required') {
        assert.match(got.reason,locale === 'zh-cn' ? /非负.*零/ : /non-negative.*zero/)
        assert.doesNotMatch(got.reason,/有限正数|finite positive|正整数|positive integer|数组|array/)
        assert.match(got.action,locale === 'zh-cn' ? /布尔值.*字符串.*数组/ : /booleans.*strings.*arrays/)
      }
      if (reason === 'operator_boolean_required') {
        assert.match(got.reason,locale === 'zh-cn' ? /布尔值.*true.*false/ : /boolean.*true.*false/)
        assert.equal(got.parameter,locale === 'zh-cn' ? '保留单几何拓扑（preserve_topology）' : 'Preserve single-geometry topology (preserve_topology)')
      }
      for (const patch of [{ field:'preserve_topology' },{ field:'PRIVATE_BIZ_FIELD' },{ parameter:'distance' },{ operator_id:'unknown_construction' },{ node_index:true }]) {
        const rejected = executionFailurePresentation({ diagnostic:{ ...diagnostic, operator:{ ...diagnostic.operator, ...patch } }, message:'private-password' },translate)
        assert.equal(rejected.stage,'')
        assert.equal(rejected.operator,'')
        assert.doesNotMatch(JSON.stringify(rejected),/PRIVATE_BIZ_FIELD|private-password/)
      }
    }
    for (const reason of ['operator_positive_number_required','operator_number_invalid','operator_number_array_invalid']) {
      const rejected = executionFailurePresentation({ diagnostic:{ schema_version:'addp.execution-failure/v3', stage:'execution', reason,
        operator:{ node_index:2, operator_id:'simplify', parameter:'tolerance' } } },translate)
      assert.equal(rejected.stage,'')
    }
    for (const [reason,parameter] of [['operator_nonnegative_number_required','tolerance'],['operator_boolean_required','preserve_topology']]) {
      for (const operator_id of ['buffer','batch_buffer','distance_to','voronoi','contains']) {
        const rejected = executionFailurePresentation({ diagnostic:{ schema_version:'addp.execution-failure/v3', stage:'execution', reason,
          operator:{ node_index:2, operator_id, parameter } } },translate)
        assert.equal(rejected.stage,'')
      }
    }
  }
})


test('measurement diagnostics close ten facts and distinguish EMPTY from NULL without leaking values',()=>{
  const rows=[["get_area", "operator_input_invalid", "input_gdf", ""], ["get_area", "operator_crs_invalid", "", ""], ["get_area", "operator_geometry_field_conflict", "input_gdf", "area"], ["get_length", "operator_input_invalid", "input_gdf", ""], ["get_length", "operator_crs_invalid", "", ""], ["get_length", "operator_geometry_field_conflict", "input_gdf", "length"], ["split_by_area", "operator_input_invalid", "input_gdf", ""], ["split_by_area", "operator_crs_invalid", "", ""], ["split_by_area", "operator_geometry_field_conflict", "input_gdf", "area"], ["split_by_area", "operator_nonnegative_number_required", "threshold", ""]]
  assert.deepEqual(operatorInputRules.filter(([name])=>['get_area','get_length','split_by_area'].includes(name)),rows)
  for(const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    for(const [operator_id,reason,parameter,field]of rows){
      const operator={node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg'},translate)
      assert.equal(got.operator,messages.operatorNames[operator_id]);assert.equal(got.field,field)
      assert.equal(got.reason,translate(`common.executionFailure.measurement.reasons.${reason}`,{field}))
      assert.equal(got.action,translate(`common.executionFailure.measurement.actions.${reason}`,{field}))
      assert.equal(got.parameter,parameter?messages.operatorParameters[parameter]:'')
      if(reason==='operator_input_invalid'){assert.match(got.reason,/EMPTY/);assert.match(got.reason,/NULL/);assert.match(got.action,/Z\/M/)}
      for(const patch of [{field:'PRIVATE_FIELD'},{parameter:'reference_gdf'},{node_index:true},{operator_id:'unknown_measurement'},{parameter_value:99}]){
        const bad=executionFailurePresentation({diagnostic:{...diagnostic,operator:{...operator,...patch}},message:'private-password'},translate)
        assert.equal(bad.stage,'');assert.equal(bad.operator,'')
      }
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    }
  }
})


test('distance admission presents five closed facts with nonempty targets and no leaked values',()=>{
  const rows=[["operator_input_invalid", "input_gdf", ""], ["operator_reference_invalid", "target_geom", ""], ["operator_geometry_field_conflict", "input_gdf", "distance"], ["operator_projected_crs_required", "", ""], ["operator_crs_mismatch", "", ""]]
  assert.deepEqual(operatorInputRules.filter(([name])=>name==='distance_to'),rows.map(([reason,parameter,field])=>['distance_to',reason,parameter,field]))
  for(const locale of ['zh-cn','en']) {
    const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
    for(const [reason,parameter,field]of rows){
      const operator={node_index:2,operator_id:'distance_to',...(parameter?{parameter}:{}),...(field?{field}:{})}
      const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator}
      const got=executionFailurePresentation({diagnostic,message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg'},translate)
      assert.equal(got.operator,locale==='zh-cn'?'距离计算':'Distance calculation')
      assert.equal(got.reason,translate(`common.executionFailure.distance.reasons.${reason}`,{field}))
      assert.equal(got.action,translate(`common.executionFailure.distance.actions.${reason}`,{field}))
      assert.equal(got.parameter,parameter?messages.operatorParameters[parameter]:'');assert.equal(got.field,field)
      if(parameter==='target_geom'){assert.match(got.reason,/NULL/);assert.match(got.reason,/EMPTY/);assert.match(got.action,locale==='zh-cn'?/空源/:/empty source/)}
      for(const patch of [{field:'PRIVATE_FIELD'},{parameter:'reference_gdf'},{node_index:true},{operator_id:'unknown_distance'},{parameter_value:99}]){
        const bad=executionFailurePresentation({diagnostic:{...diagnostic,operator:{...operator,...patch}},message:'private-password'},translate)
        assert.equal(bad.stage,'');assert.equal(bad.operator,'')
      }
      assert.doesNotMatch(JSON.stringify(got),/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    }
  }
})


test("bounds admission closes six facts without confusing coordinate bounds and measurement",()=>{
 const rows=[["get_bounds", "operator_input_invalid", "input_gdf", ""], ["get_bounds", "operator_crs_invalid", "", ""], ["get_bounds", "operator_geometry_field_conflict", "input_gdf", "minx"], ["get_bounds", "operator_geometry_field_conflict", "input_gdf", "miny"], ["get_bounds", "operator_geometry_field_conflict", "input_gdf", "maxx"], ["get_bounds", "operator_geometry_field_conflict", "input_gdf", "maxy"]]
 assert.deepEqual(operatorInputRules.filter(([op])=>op==='get_bounds'),rows)
 for(const locale of ['zh-cn','en']){
  const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
  const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
  for(const [operator_id,reason,parameter,field]of rows){
   const operator={node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}
   const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator}
   const got=executionFailurePresentation({diagnostic,message:'PRIVATE_FIELD private-password /private/input.gpkg'},translate)
   assert.equal(got.operator,messages.operatorNames.get_bounds);assert.equal(got.field,field)
   assert.equal(got.reason,translate(`common.executionFailure.bounds.reasons.${reason}`,{field}))
   assert.equal(got.action,translate(`common.executionFailure.bounds.actions.${reason}`,{field}))
   if(reason==='operator_input_invalid'){assert.match(got.reason,/EMPTY/);assert.match(got.reason,/NULL/);assert.match(got.action,/Z\/M/)}
   if(reason==='operator_crs_invalid')assert.match(got.reason,locale==='zh-cn'?/地理或投影/:/geographic or projected/)
   for(const patch of [{field:'area'},{field:'PRIVATE_FIELD'},{parameter:'reference_gdf'},{node_index:true},{operator_id:'foreign_bounds'},{parameter_value:99}]){
    const bad=executionFailurePresentation({diagnostic:{...diagnostic,operator:{...operator,...patch}}},translate)
    assert.equal(bad.stage,'');assert.equal(bad.operator,'')
   }
   assert.doesNotMatch(JSON.stringify(got),/PRIVATE_FIELD|private-password|\/private\/input.gpkg/)
  }
 }
})


test("multipart split closes four facts without exposing source values",()=>{
 const rows=[["split_multipart", "operator_input_invalid", "input_gdf", ""], ["split_multipart", "operator_crs_invalid", "", ""], ["split_multipart", "operator_field_conflict", "input_gdf", "source_id"], ["split_multipart", "operator_field_conflict", "input_gdf", "part_no"]]
 assert.deepEqual(operatorInputRules.filter(([op])=>op==='split_multipart'),rows)
 for(const locale of ['zh-cn','en']){
  const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
  const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
  for(const [operator_id,reason,parameter,field]of rows){
   const operator={node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}
   const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator}
   const got=executionFailurePresentation({diagnostic,message:'PRIVATE_FIELD private-password /private/input.gpkg'},translate)
   assert.equal(got.operator,messages.operatorNames.split_multipart);assert.equal(got.field,field)
   assert.equal(got.reason,translate(`common.executionFailure.multipart.reasons.${reason}`,{field}))
   assert.equal(got.action,translate(`common.executionFailure.multipart.actions.${reason}`,{field}))
   if(reason==='operator_input_invalid'){assert.match(got.reason,/EMPTY/);assert.match(got.reason,/NULL/);assert.match(got.action,/Z\/M/)}
   if(reason==='operator_crs_invalid')assert.match(got.reason,locale==='zh-cn'?/地理或投影/:/geographic or projected/)
   for(const patch of [{field:'area'},{field:'PRIVATE_FIELD'},{parameter:'reference_gdf'},{node_index:true},{operator_id:'foreign_parts'},{parameter_value:99}]){
    const bad=executionFailurePresentation({diagnostic:{...diagnostic,operator:{...operator,...patch}}},translate)
    assert.equal(bad.stage,'');assert.equal(bad.operator,'')
   }
   assert.doesNotMatch(JSON.stringify(got),/PRIVATE_FIELD|private-password|\/private\/input.gpkg/)
  }
 }
})


test("vertices extraction closes seven facts and retains coordinate occurrences",()=>{
 const rows=[["extract_vertices", "operator_input_invalid", "input_gdf", ""], ["extract_vertices", "operator_crs_invalid", "", ""], ["extract_vertices", "operator_field_conflict", "input_gdf", "source_id"], ["extract_vertices", "operator_field_conflict", "input_gdf", "part_no"], ["extract_vertices", "operator_field_conflict", "input_gdf", "ring_no"], ["extract_vertices", "operator_field_conflict", "input_gdf", "vertex_no"], ["extract_vertices", "operator_field_conflict", "input_gdf", "vertex_id"]]
 assert.deepEqual(operatorInputRules.filter(([op])=>op==='extract_vertices'),rows)
 for(const locale of ['zh-cn','en']){
  const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
  const translate=(key,params={})=>key.replace('common.executionFailure.','').split('.').reduce((node,part)=>node[part],messages).replace(/\{(\w+)\}/g,(_,key)=>params[key])
  for(const [operator_id,reason,parameter,field]of rows){
   const operator={node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}
   const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator}
   const got=executionFailurePresentation({diagnostic,message:'PRIVATE_FIELD private-password /private/input.gpkg'},translate)
   assert.equal(got.operator,messages.operatorNames.extract_vertices);assert.equal(got.field,field)
   assert.equal(got.reason,translate(`common.executionFailure.vertices.reasons.${reason}`,{field}))
   assert.equal(got.action,translate(`common.executionFailure.vertices.actions.${reason}`,{field}))
   if(reason==='operator_input_invalid'){assert.match(got.reason,/EMPTY/);assert.match(got.reason,/NULL/);assert.match(got.action,/Z\/M/)}
   if(reason==='operator_crs_invalid')assert.match(got.reason,locale==='zh-cn'?/地理或投影/:/geographic or projected/)
   for(const patch of [{field:'area'},{field:'PRIVATE_FIELD'},{parameter:'reference_gdf'},{node_index:true},{operator_id:'foreign_vertices'},{parameter_value:99}]){
    const bad=executionFailurePresentation({diagnostic:{...diagnostic,operator:{...operator,...patch}}},translate)
    assert.equal(bad.stage,'');assert.equal(bad.operator,'')
   }
   assert.doesNotMatch(JSON.stringify(got),/PRIVATE_FIELD|private-password|\/private\/input.gpkg/)
  }
 }
})


test('geometry validity diagnostics retain old geometry rules and hide configured names',()=>{
 const rows=[["check_geometry", "operator_input_invalid", "input_gdf", ""], ["check_geometry", "operator_output_fields_invalid", "", ""], ["repair_geometry", "operator_input_invalid", "input_gdf", ""]]
 assert.deepEqual(operatorInputRules.filter(([op])=>['check_geometry','repair_geometry'].includes(op)),rows)
 for(const locale of ['zh-cn','en']){
  const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
  const translate=(key,args={})=>key.split('.').slice(2).reduce((v,k)=>v[k],messages).replace(/\{(\w+)\}/g,(_,k)=>args[k])
  for(const [operator_id,reason,parameter]of rows){
   const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:1,operator_id,...(parameter?{parameter}:{})}}
   const got=executionFailurePresentation({code:'develop.workflow.invalid_params',message:'PRIVATE_FIELD /private/geometry password=value',diagnostic},translate)
   assert.equal(got.operator,messages.operatorNames[operator_id]);assert.equal(got.reason,messages.geometryValidity.reasons[reason]);assert.equal(got.action,messages.geometryValidity.actions[reason]);assert.equal(got.field,'')
   assert.doesNotMatch(JSON.stringify(got),/PRIVATE_FIELD|password|\/private\//)
   if(reason==='operator_input_invalid'){assert.match(got.action,/NULL\/EMPTY/);assert.match(got.reason,/XY/);assert.match(got.reason,/Z\/M/)}
   else{assert.match(got.action,/valid_field/);assert.match(got.action,/reason_field/)}
   for(const change of [{field:'PRIVATE_FIELD'},{parameter:'valid_field'},{parameter_value:'PRIVATE'},{node_index:true}]){
    const invalid=executionFailurePresentation({diagnostic:{...diagnostic,operator:{...diagnostic.operator,...change}},message:'PRIVATE_FIELD'},translate);assert.equal(invalid.operator,'');assert.doesNotMatch(JSON.stringify(invalid),/PRIVATE_FIELD/)
   }
  }
 }
})


test('reference snap diagnostics preserve tolerance semantics and privacy',()=>{
 const rows=[["snap_geometry", "operator_input_invalid", "input_gdf", ""], ["snap_geometry", "operator_reference_invalid", "reference_gdf", ""], ["snap_geometry", "operator_crs_invalid", "", ""], ["snap_geometry", "operator_nonnegative_number_required", "tolerance", ""]]
 assert.deepEqual(operatorInputRules.filter(([op])=>op==='snap_geometry'),rows)
 for(const locale of ['zh-cn','en']){
  const messages=JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
  const translate=(key,args={})=>key.split('.').slice(2).reduce((v,k)=>v[k],messages).replace(/\{(\w+)\}/g,(_,k)=>args[k])
  for(const [operator_id,reason,parameter]of rows){
   const diagnostic={schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:1,operator_id,...(parameter?{parameter}:{})}}
   const got=executionFailurePresentation({code:'develop.workflow.invalid_params',message:'PRIVATE_FIELD /private/snap 123.456 password=value',diagnostic},translate)
   assert.equal(got.operator,messages.operatorNames.snap_geometry);assert.equal(got.reason,messages.referenceSnap.reasons[reason]);assert.equal(got.action,messages.referenceSnap.actions[reason]);assert.equal(got.field,'')
   assert.doesNotMatch(JSON.stringify(got),/PRIVATE_FIELD|password|123\.456|\/private\//)
   for(const change of [{field:'distance'},{parameter:'private'},{parameter_value:0.75},{node_index:true}]){
    const invalid=executionFailurePresentation({diagnostic:{...diagnostic,operator:{...diagnostic.operator,...change}},message:'PRIVATE_FIELD'},translate);assert.equal(invalid.operator,'');assert.doesNotMatch(JSON.stringify(invalid),/PRIVATE_FIELD/)
   }
  }
  assert.match(messages.referenceSnap.actions.operator_nonnegative_number_required,/零|Zero/)
  assert.match(messages.referenceSnap.actions.operator_reference_invalid,/空参考|empty reference/)
 }
})


test('topology check diagnostics identify the source and coverage in both languages', () => {
  const cases = [
    ['merge_line_chains', 'lineCheck', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['merge_line_chains', 'lineCheck', 'operator_crs_invalid', '', /投影|projected/],
    ['merge_line_chains', 'lineCheck', 'operator_endpoint_network_invalid', 'input_gdf', /打断|split/],
    ['merge_line_chains', '', 'operator_match_fields_invalid', 'match_fields', /空数组|empty list/],
    ['normalize_line_network', 'lineCheck', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['normalize_line_network', 'lineCheck', 'operator_crs_invalid', '', /投影|projected/],
    ['group_line_components', 'lineCheck', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['group_line_components', 'lineCheck', 'operator_crs_invalid', '', /投影|projected/],
    ['split_line_intersections', 'lineSplit', 'operator_enum_invalid', 'scope', /between.*all/],
    ['split_line_intersections', 'lineSplit', 'operator_input_overlap', 'input_gdf', /线叠检查|line overlap/],
    ['split_line_intersections', 'lineSplit', 'operator_source_topology_invalid', 'input_gdf', /\ball\b/],
    ['check_line_overlap', 'lineCheck', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['check_line_overlap', 'lineCheck', 'operator_crs_invalid', '', /投影|projected/],
    ['check_line_self_overlap', 'lineCheck', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['check_line_self_overlap', 'lineCheck', 'operator_crs_invalid', '', /投影|projected/],
    ['check_dangling_points', 'lineCheck', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['check_dangling_points', 'lineCheck', 'operator_crs_invalid', '', /投影|projected/],
    ['check_line_intersection', 'lineCheck', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['check_line_intersection', 'lineCheck', 'operator_crs_invalid', '', /投影|projected/],
    ['check_line_self_intersection', 'lineCheck', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['check_line_self_intersection', 'lineCheck', 'operator_crs_invalid', '', /投影|projected/],
    ['check_polygon_adjacency', 'polygonAdjacency', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['check_polygon_adjacency', 'polygonAdjacency', 'operator_crs_invalid', '', /投影|projected/],
    ['check_polygon_adjacency', 'polygonAdjacency', 'operator_input_overlap', 'input_gdf', /面叠检查|polygon overlap/],
    ['check_polygon_overlap', 'polygonOverlap', 'operator_input_invalid', 'input_gdf', /id_field/],
    ['check_polygon_overlap', 'polygonOverlap', 'operator_crs_invalid', '', /投影|projected/],
    ['check_polygon_gap', 'polygonGap', 'operator_input_invalid', 'input_gdf', /空表|no rows/],
    ['check_polygon_gap', 'polygonGap', 'operator_mask_invalid', 'coverage_gdf', /coverage_gdf/],
    ['check_polygon_gap', 'polygonGap', 'operator_crs_invalid', '', /两个图层|both layers/],
  ]
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const lineNames = locale === 'zh-cn' ? ['线交检查', '线内自交'] : ['Check line intersections', 'Check line self-intersections']
    assert.equal(messages.operatorNames.check_line_intersection, lineNames[0])
    assert.equal(messages.operatorNames.check_line_self_intersection, lineNames[1])
    const overlapNames = locale === 'zh-cn' ? ['线叠检查', '线内自叠'] : ['Check line overlaps', 'Check line self-overlaps']
    assert.equal(messages.operatorNames.check_line_overlap, overlapNames[0])
    assert.equal(messages.operatorNames.check_line_self_overlap, overlapNames[1])
    const translate = (key, args = {}) => key.replace('common.executionFailure.', '').split('.')
      .reduce((value, part) => value[part], messages).replace(/\{(\w+)\}/g, (_, name) => args[name])
    for (const [operator_id, profile, reason, parameter, guidance] of cases) {
      const diagnostic = {
        schema_version: 'addp.execution-failure/v3', stage: 'execution', reason,
        operator: { node_index: 2, operator_id, ...(parameter ? { parameter } : {}) },
      }
      const display = executionFailurePresentation({ diagnostic, message: 'PRIVATE_FIELD 123.456' }, translate)
      assert.equal(display.operator, messages.operatorNames[operator_id])
      assert.equal(display.reason, (profile ? messages[profile] : messages).reasons[reason])
      assert.equal(display.parameter, parameter ? messages.operatorParameters[parameter] : '')
      assert.equal(display.field, '')
      assert.match(display.action, guidance)
      if (operator_id === 'split_line_intersections' && reason === 'operator_input_overlap') {
        assert.doesNotMatch(display.action, /\ball\b/)
      }
      assert.doesNotMatch(JSON.stringify(display), /PRIVATE_FIELD|123\.456/)
    }
  }
})

test('endpoint diagnostics show the correct bilingual input and distance guidance', () => {
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate = (key, args = {}) => key.replace('common.executionFailure.', '').split('.')
      .reduce((value, part) => value[part], messages).replace(/\{(\w+)\}/g, (_, name) => args[name])
    const names = locale === 'zh-cn' ? ['端点吸附', '线端延伸'] : ['Snap line endpoints', 'Extend line endpoints']
    for (const [index, op] of ['snap_line_endpoints', 'extend_line_endpoints'].entries()) {
      const bound = index === 0 ? 'tolerance' : 'max_distance'
      const reason = index === 0 ? 'operator_positive_number_required' : 'operator_distance_limit_invalid'
      const cases = [
        ['operator_input_invalid', 'input_gdf', /\bid_field\b/],
        ['operator_reference_invalid', 'reference_gdf', /reference_id_field/],
        ['operator_crs_invalid', '', locale === 'zh-cn' ? /相同/ : /same/],
        [reason, bound, new RegExp(bound)]
      ]
      for (const [reason, parameter, action] of cases) {
        const diagnostic = {
          schema_version: 'addp.execution-failure/v3', stage: 'execution', reason,
          operator: { node_index: 2, operator_id: op, ...(parameter ? { parameter } : {}) }
        }
        const display = executionFailurePresentation({ diagnostic, message: 'PRIVATE_FIELD 123.456' }, translate)
        assert.equal(display.operator, names[index])
        assert.match(display.node, /2/)
        if (parameter) assert.ok(display.parameter.includes(parameter))
        else assert.equal(display.parameter, '')
        assert.match(display.action, action)
        assert.doesNotMatch(JSON.stringify(display), /PRIVATE_FIELD|123\.456/)
      }
    }
  }
})

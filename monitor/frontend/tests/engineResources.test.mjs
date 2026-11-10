import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { engineMetrics, engineSummaryMetrics, redisMetrics, minioMetrics, engineDetailMetrics, engineMetricMissingReason, engineSummaryFor, resolveEngineRoute, validateEnginePage, validateEngineResources, engineTrendParameters } from '../src/utils/engineResources.js'

const id = '10000000-0000-4000-8000-000000000001', node = '10000000-0000-4000-8000-000000000002'
const owner = { scope: 'infra', deployment_id: id, display_name: 'Infra PostgreSQL', version: 1, enabled: true, node_id: node, host_binding_state: 'bound', engine_type: 'postgresql' }
const at = '2026-10-20T14:00:00Z'
test('service and engine host navigation share one domain component and bounded reference resolver', () => {
  for (const name of ['ProcessResources', 'EngineResources']) {
    const source = readFileSync(new URL(`../src/views/${name}.vue`, import.meta.url), 'utf8')
    assert.match(source, /import MonitoredHostLink from '\.\.\/components\/MonitoredHostLink\.vue'/)
    assert.match(source, /<MonitoredHostLink\b/)
    assert.doesNotMatch(source, /function openHost|path: `\/node-resources\//)
    assert.match(source, /loadHostReferences/)
  }
  const source = readFileSync(new URL('../src/components/MonitoredHostLink.vue', import.meta.url), 'utf8')
  assert.match(source, /navigateMonitorRoute/)
  assert.match(source, /history: 'push'/)
  assert.match(source, /props\.host\?\.node_id === props\.nodeId/)
})
function response(metrics = engineSummaryMetrics) {
  return { queried_at: at, start: at, end: at, step_seconds: 15, policy_version: 1, lookback_seconds: 300, data: [{ subject: { kind: 'engine_deployment', deployment_id: id, display_name: owner.display_name, engine_type: 'postgresql' }, node_id: node, collection: { state: 'collecting', sampled_at: at }, series: metrics.map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: metric.windowSeconds, dimensions: {}, points: [{ evaluated_at: at, sampled_at: at, value: 0, data_state: 'valid' }] })) }] }
}

function undefinedRedisRatio(key) {
 const value=response(engineDetailMetrics('redis',key)),row=value.data[0]
 row.subject.engine_type='redis'
 Object.assign(row.series.find(s=>s.metric_key===key).points[0],{value:null,sampled_at:null,data_state:'no_data'})
 return row
}
test('Redis undefined ratios explain only current exact zero dependencies without altering data',()=>{
 for(const [key,reason] of [['redis.memory.used_percent','redisNoLimit'],['redis.keyspace.hit_percent','redisNoLookups']]){
  const row=undefinedRedisRatio(key),before=structuredClone(row)
  assert.equal(engineMetricMissingReason(row,key),reason)
  assert.deepEqual(row,before)
  row.series.find(s=>s.metric_key===key).points[0]={evaluated_at:at,sampled_at:at,value:0,data_state:'valid'}
  assert.equal(engineMetricMissingReason(row,key),'')
 }
})
test('Redis explanations reject partial, stale, rounded or differently timed dependency evidence',()=>{
 for(const [key,dependencies] of [
  ['redis.memory.used_percent',['redis.memory.limit_bytes']],
  ['redis.keyspace.hit_percent',['redis.keyspace.hits_per_second','redis.keyspace.misses_per_second']]
 ])for(const dependency of dependencies)for(const change of [
  point=>{point.value=0.0000001},point=>{point.data_state='stale'},
  point=>{point.data_state='no_data';point.value=null;point.sampled_at=null},
  point=>{point.evaluated_at='2026-10-20T13:59:45Z'},point=>{point.sampled_at='2026-10-20T13:59:45Z'}
 ]){
  const row=undefinedRedisRatio(key);change(row.series.find(s=>s.metric_key===dependency).points[0])
  assert.equal(engineMetricMissingReason(row,key),'')
 }
 const row=undefinedRedisRatio('redis.keyspace.hit_percent');row.series=row.series.filter(s=>s.metric_key!=='redis.keyspace.misses_per_second')
 assert.equal(engineMetricMissingReason(row,'redis.keyspace.hit_percent'),'')
})
test('Redis explanations clear with unavailable source, other engine and incomplete or historical rows',()=>{
 const key='redis.memory.used_percent'
 for(const state of ['no_sample','failed','stale','snapshot_unavailable','source_unavailable','not_active','not_connected']){
  const row=undefinedRedisRatio(key);row.collection.state=state
  assert.equal(engineMetricMissingReason(row,key),'')
 }
 for(const mutate of [
  row=>{row.subject.engine_type='postgresql'},row=>{row.collection.sampled_at=null},
  row=>{row.series.find(s=>s.metric_key===key).points.push({...row.series[0].points[0]})},
  row=>{row.series.find(s=>s.metric_key==='redis.memory.limit_bytes').points.push({...row.series[0].points[0]})},
  row=>{row.series=row.series.filter(s=>s.metric_key!==key)}
 ]){const row=undefinedRedisRatio(key);mutate(row);assert.equal(engineMetricMissingReason(row,key),'')}
 assert.equal(engineMetricMissingReason(null,key),'')
})
test('Redis dependency timestamps compare instants rather than textual fractions',()=>{
 const key='redis.memory.used_percent',row=undefinedRedisRatio(key)
 const point=row.series.find(s=>s.metric_key==='redis.memory.limit_bytes').points[0]
 point.evaluated_at=point.sampled_at='2026-10-20T14:00:00.000Z'
 assert.equal(engineMetricMissingReason(row,key),'redisNoLimit')
})

test('PostgreSQL catalog has fifteen metrics and three explicit one-minute rates', () => {
  assert.equal(engineMetrics.length, 15)
  assert.equal(new Set(engineMetrics.map(item => item.key)).size, 15)
  assert.equal(engineMetrics.filter(item => item.windowSeconds === 60).length, 3)
  assert.deepEqual(engineSummaryMetrics.map(item => item.name), ['total', 'active', 'committed', 'capacity'])
})
test('engine URL retains exact deployment, metric, range and bounded refresh', () => {
  const input = { deployment_id: id, metric: engineMetrics[11].key, range: '7d', refresh: 'off', search: 'Infra', page: '2' }
  assert.equal(resolveEngineRoute(input).changed, false)
  assert.deepEqual(resolveEngineRoute(input).query, input)
  assert.deepEqual(resolveEngineRoute({ deployment_id: 'foreign', metric: 'up', range: '30d', refresh: '1', page: '-1', extra: 'x' }).query, {})
})
test('current page rejects duplicate, malformed and wrong-page owner references', () => {
  const page = { data: [owner], page: 1, page_size: 20, total: 1 }
  assert.equal(validateEnginePage(page, 1), page)
  for (const value of [{ ...page, data: [owner, owner], total: 2 }, { ...page, page: 2 }, { ...page, data: [{ ...owner, node_id: '' }] }, { ...page, data: [{ ...owner, engine_type: 'mysql' }] }]) assert.throws(() => validateEnginePage(value, 1), /invalid_resource_response/)
})
test('valid zero is preserved; foreign, stale, fractional connection or incomplete evidence is rejected', () => {
  const good = response()
  assert.equal(validateEngineResources(good, [owner], () => engineSummaryMetrics), good)
  for (const mutate of [
    value => { value.data[0].subject.deployment_id = node },
    value => { value.data[0].collection.state = 'snapshot_unavailable' },
    value => { value.data[0].series[0].points[0].value = 0.5 },
    value => { value.data[0].series[0].points[0].sampled_at = '2026-10-20T13:58:00Z' },
    value => { value.data[0].series[0].unit = 'percent' },
    value => { value.data[0].series.pop() },
    value => { value.data[0].series[0].metric_key = 'node.cpu.busy_percent' }
  ]) { const value = response(); mutate(value); assert.throws(() => validateEngineResources(value, [owner], () => engineSummaryMetrics), /invalid_resource_response/) }
})
test('current-container trend accepts clipping and null gaps without inventing zero', () => {
  const metric = engineMetrics[8], params = engineTrendParameters(id, metric.key, '7d', at), value = response([metric])
  value.start = '2026-10-20T13:59:30Z'
  value.data[0].collection = { state: 'no_sample', sampled_at: null }
  value.data[0].series[0].points = ['2026-10-20T13:59:30Z', '2026-10-20T13:59:45Z', at].map((at, index) => ({ evaluated_at: at, sampled_at: index === 1 ? null : at, value: index === 1 ? null : 0, data_state: index === 1 ? 'no_data' : 'valid' }))
  assert.equal(validateEngineResources(value, [owner], () => [metric], params), value)
  value.data[0].series[0].points[1].value = 0
  assert.throws(() => validateEngineResources(value, [owner], () => [metric], params), /invalid_resource_response/)
})

test('Redis has a fixed 22-metric catalog and explicit groups within the existing 12-metric budget',()=>{
 assert.equal(redisMetrics.length,22)
 assert.equal(new Set(redisMetrics.map(m=>m.key)).size,22)
 for(const [key,count] of [['redis.memory.used_percent',10],['redis.commands.per_second',7],['redis.persistence.aof_enabled',5]])assert.equal(engineDetailMetrics('redis',key).length,count)
 assert.equal(engineSummaryFor('redis').length,4)
 const route=resolveEngineRoute({engine_type:'redis',deployment_id:id,metric:'redis.commands.per_second',range:'1h'})
 assert.equal(route.engineType,'redis');assert.equal(route.metric,'redis.commands.per_second');assert.equal(route.changed,false)
 assert.equal(resolveEngineRoute({engine_type:'redis',metric:'postgresql.connections.total'}).metric,'redis.memory.used_percent')
 assert.equal(validateEnginePage({data:[{...owner,engine_type:'redis'}],total:1,page:1,page_size:20},1).data.length,1)
})

test('MinIO uses a bounded two-group catalog, exact type and current-unit metadata',()=>{
 assert.equal(minioMetrics.length,17)
 assert.equal(new Set(minioMetrics.map(metric=>metric.key)).size,17)
 assert.equal(engineDetailMetrics('minio','minio.capacity.used_percent').length,10)
 assert.equal(engineDetailMetrics('minio','minio.requests.per_second').length,7)
 assert.equal(engineSummaryFor('minio').length,4)
 const query={engine_type:'minio',deployment_id:id,metric:'minio.traffic.sent_bytes_per_second',range:'1h'}
 const state=resolveEngineRoute(query);assert.equal(state.changed,false);assert.deepEqual(state.query,query)
 assert.equal(resolveEngineRoute({engine_type:'minio',metric:'redis.commands.per_second'}).metric,'minio.capacity.used_percent')
 assert.equal(validateEnginePage({data:[{...owner,engine_type:'minio'}],total:1,page:1,page_size:20},1).data.length,1)
 assert.equal(engineTrendParameters(id,query.metric,'1h',at).metrics,query.metric)
 const metrics=engineSummaryFor('minio'), value=response(metrics);value.data[0].subject.engine_type='minio'
 assert.equal(validateEngineResources(value,[{...owner,engine_type:'minio'}],()=>metrics),value)
})


test('deployment provenance scopes survive URL round trips and reject invented owner scopes', () => {
 const input={scope:'business',search:'Warehouse',page:'2',refresh:'off'}
 assert.deepEqual(resolveEngineRoute(input).query,input)
 assert.equal(resolveEngineRoute(input).scope,'business')
 assert.equal(resolveEngineRoute({scope:'tenant',engine_type:'minio'}).scope,'infra')
 assert.deepEqual(resolveEngineRoute({engine_type:'minio'}).query,{})
 for(const scope of [undefined,'tenant','Infra'])assert.throws(()=>validateEnginePage({data:[{...owner,scope}],page:1,page_size:20,total:1},1))
})

test('mixed engine summaries validate each current owner catalog without accepting foreign metrics', () => {
 const owners=[owner,{...owner,deployment_id:node,engine_type:'minio'}]
 const value=response()
 const minio=response(engineSummaryFor('minio')).data[0]
 Object.assign(minio.subject,{deployment_id:node,engine_type:'minio'})
 value.data.push(minio)
 assert.equal(validateEngineResources(value,owners,row=>engineSummaryFor(row.engine_type)),value)
 minio.series[0]=structuredClone(value.data[0].series[0])
 assert.throws(()=>validateEngineResources(value,owners,row=>engineSummaryFor(row.engine_type)),/invalid_resource_response/)
})

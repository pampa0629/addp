import { autoRefreshOptions } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { isTargetUUID } from './monitoringTargets.js'
import { currentResourceValue, resourceRanges } from './nodeResources.js'

export const engineMetrics = [
  ['connections.total', 'total', 'connections'], ['connections.active', 'active', 'connections'],
  ['connections.idle', 'idle', 'connections'], ['connections.idle_in_transaction', 'idleTransaction', 'connections'],
  ['connections.waiting', 'waiting', 'connections'], ['connections.lock_waiting', 'lockWaiting', 'connections'],
  ['connections.io_waiting', 'ioWaiting', 'connections'], ['connections.maximum', 'maximum', 'connections'],
  ['transactions.committed_per_second', 'committed', 'transactions_per_second', 60],
  ['transactions.rolled_back_per_second', 'rolledBack', 'transactions_per_second', 60],
  ['deadlocks.per_second', 'deadlocks', 'events_per_second', 60], ['database.total_bytes', 'capacity', 'bytes']
].map(([key, name, unit, windowSeconds = 0]) => ({ key: `postgresql.${key}`, name, unit, windowSeconds }))
export const engineSummaryMetrics = engineMetrics.filter(item => ['total', 'active', 'committed', 'capacity'].includes(item.name))
export const redisMetrics = [
  ['uptime_seconds', 'redisUptime', 'seconds', 'resources'],
  ['memory.allocated_bytes', 'redisAllocated', 'bytes', 'resources'],
  ['memory.rss_bytes', 'redisRSS', 'bytes', 'resources'],
  ['memory.limit_bytes', 'redisLimit', 'bytes', 'resources'],
  ['memory.excluded_bytes', 'redisExcluded', 'bytes', 'resources'],
  ['memory.counted_bytes', 'redisCounted', 'bytes', 'resources'],
  ['memory.used_percent', 'redisPercent', 'percent', 'resources'],
  ['connections.total', 'redisConnections', 'connections', 'resources'],
  ['connections.maximum', 'redisMaximum', 'connections', 'resources'],
  ['connections.blocked', 'redisBlocked', 'connections', 'resources'],
  ['commands.per_second', 'redisCommands', 'commands_per_second', 'throughput', 60],
  ['keyspace.hits_per_second', 'redisHits', 'events_per_second', 'throughput', 60],
  ['keyspace.misses_per_second', 'redisMisses', 'events_per_second', 'throughput', 60],
  ['keyspace.hit_percent', 'redisHitPercent', 'percent', 'throughput', 60],
  ['evictions.per_second', 'redisEvictions', 'events_per_second', 'throughput', 60],
  ['connections.rejected_per_second', 'redisRejected', 'events_per_second', 'throughput', 60],
  ['oom.per_second', 'redisOOM', 'events_per_second', 'throughput', 60],
  ['loading', 'redisLoading', 'status', 'persistence'],
  ['persistence.aof_enabled', 'redisAOF', 'status', 'persistence'],
  ['persistence.rdb_save_ok', 'redisRDB', 'status', 'persistence'],
  ['persistence.aof_write_ok', 'redisAOFWrite', 'status', 'persistence'],
  ['persistence.aof_rewrite_ok', 'redisAOFRewrite', 'status', 'persistence']
].map(([key, name, unit, group, windowSeconds = 0]) => ({ key: `redis.${key}`, name, unit, group, windowSeconds }))
export const minioMetrics = [
  ['uptime_seconds','minioUptime','seconds','minioResources'],
  ['drives.online','minioOnline','drives','minioResources'], ['drives.offline','minioOffline','drives','minioResources'],
  ['capacity.total_bytes','minioTotal','bytes','minioResources'], ['capacity.free_bytes','minioFree','bytes','minioResources'], ['capacity.used_percent','minioPercent','percent','minioResources'],
  ['scan.object_bytes','minioObjectBytes','bytes','minioResources'], ['scan.objects','minioObjects','objects','minioResources'], ['scan.buckets','minioBuckets','buckets','minioResources'], ['scan.age_seconds','minioScanAge','seconds','minioResources'],
  ['requests.per_second','minioRequests','requests_per_second','minioTraffic',60], ['requests.inflight','minioInflight','requests','minioTraffic'],
  ['requests.errors_4xx_per_second','minio4xx','requests_per_second','minioTraffic',60], ['requests.errors_5xx_per_second','minio5xx','requests_per_second','minioTraffic',60], ['requests.error_5xx_percent','minioErrorPercent','percent','minioTraffic',60],
  ['traffic.sent_bytes_per_second','minioSent','bytes_per_second','minioTraffic',60], ['traffic.received_bytes_per_second','minioReceived','bytes_per_second','minioTraffic',60]
].map(([key,name,unit,group,windowSeconds=0])=>({key:`minio.${key}`,name,unit,group,windowSeconds}))
export const engineMetricsFor = kind => kind === 'minio' ? minioMetrics : kind === 'redis' ? redisMetrics : kind === 'postgresql' ? engineMetrics : []
export const engineSummaryFor = kind => kind === 'minio' ? minioMetrics.filter(metric=>['minioTotal','minioFree','minioPercent','minioRequests'].includes(metric.name)) : kind === 'redis' ? redisMetrics.filter(metric => ['redisAllocated', 'redisPercent', 'redisConnections', 'redisCommands'].includes(metric.name)) : kind === 'postgresql' ? engineSummaryMetrics : []
export const engineGroups = kind => [...new Set(engineMetricsFor(kind).map(metric=>metric.group).filter(Boolean))]
export const defaultEngineMetric = kind => kind === 'minio' ? 'minio.capacity.used_percent' : kind === 'redis' ? 'redis.memory.used_percent' : 'postgresql.transactions.committed_per_second'
export function engineDetailMetrics(kind, metric) {
  const catalog = engineMetricsFor(kind)
  const selected = catalog.find(item => item.key === metric)
  return engineGroups(kind).length ? catalog.filter(item => item.group === selected?.group) : catalog
}
export function engineMetricMissingReason(row, key) {
  if (row?.subject?.engine_type !== 'redis' || row.collection?.state !== 'collecting') return ''
  const series = name => row.series?.find(item => item.metric_key === name)
  const target = series(key)
  if (target?.points?.length !== 1 || target.points[0].data_state !== 'no_data') return ''
  const evaluated = Date.parse(target.points[0].evaluated_at), sampled = Date.parse(row.collection.sampled_at)
  if (!Number.isFinite(evaluated) || !Number.isFinite(sampled)) return ''
  const dependency = name => {
    const points = series(name)?.points, point = points?.[0]
    return points?.length === 1 && Date.parse(point.evaluated_at) === evaluated && Date.parse(point.sampled_at) === sampled ? currentResourceValue(point) : null
  }
  if (key === 'redis.memory.used_percent' && dependency('redis.memory.limit_bytes') === 0) return 'redisNoLimit'
  if (key === 'redis.keyspace.hit_percent' && dependency('redis.keyspace.hits_per_second') === 0 && dependency('redis.keyspace.misses_per_second') === 0) return 'redisNoLookups'
  return ''
}
const invalid = () => { throw new Error('invalid_resource_response') }
export function resolveEngineRoute(input = {}) {
  const page = typeof input.page === 'string' && /^[1-9]\d{0,5}$/.test(input.page) ? Number(input.page) : 1
  const deploymentID = isTargetUUID(input.deployment_id) ? input.deployment_id : ''
  const scope = input.scope === 'business' ? 'business' : 'infra'
  const engineType = ['redis','minio'].includes(input.engine_type) ? input.engine_type : 'postgresql'
  const metric = engineMetricsFor(engineType).some(item => item.key === input.metric) ? input.metric : defaultEngineMetric(engineType)
  const range = typeof input.range === 'string' && Object.hasOwn(resourceRanges, input.range) ? input.range : '5m'
  const refresh = autoRefreshOptions.includes(input.refresh) ? input.refresh : '15'
  const search = typeof input.search === 'string' && input.search.length <= 255 && !/[\u0000-\u001f]/u.test(input.search) ? input.search.trim() : ''
  const query = { ...(scope!=='infra'?{scope}:{}), ...(deploymentID && engineType!=='postgresql'?{engine_type:engineType}:{}), ...(page !== 1 ? { page: String(page) } : {}), ...(search ? { search } : {}), ...(refresh !== '15' ? { refresh } : {}), ...(deploymentID ? { deployment_id: deploymentID, ...(metric !== defaultEngineMetric(engineType) ? { metric } : {}), ...(range !== '5m' ? { range } : {}) } : {}) }
  return { scope, engineType, page, deploymentID, metric, range, refresh, search, query, changed: Object.keys(input).length !== Object.keys(query).length || Object.keys(query).some(key => query[key] !== input[key]) }
}
export function validateEngineDeployment(row) {
  if (!['infra','business'].includes(row?.scope) || !isTargetUUID(row?.deployment_id) || typeof row.display_name !== 'string' || !row.display_name || row.display_name.length > 1024 || !['postgresql','redis','minio'].includes(row.engine_type) || typeof row.enabled !== 'boolean' || !Number.isSafeInteger(row.version) || row.version < 1 || !['bound', 'rejected'].includes(row.host_binding_state) || (row.host_binding_state === 'bound' ? !isTargetUUID(row.node_id) : row.node_id !== '')) invalid()
  return row
}
export function validateEnginePage(value, page, size = 20) {
  if (!Array.isArray(value?.data) || value.data.length > size || !Number.isSafeInteger(value.total) || value.total < 0 || value.page !== page || value.page_size !== size || value.total < (page - 1) * size + value.data.length && value.data.length) invalid()
  const seen = new Set()
  for (const row of value.data) { validateEngineDeployment(row); if (seen.has(row.deployment_id)) invalid(); seen.add(row.deployment_id) }
  return value
}
export function engineTrendParameters(id, metric, range, queriedAt) {
  const end = Math.floor(Date.parse(queriedAt) / 1000) * 1000
  if (!isTargetUUID(id) || ![...engineMetrics,...redisMetrics,...minioMetrics].some(item => item.key === metric) || !Object.hasOwn(resourceRanges, range) || !Number.isFinite(end)) invalid()
  return { deployment_id: id, metrics: metric, start: new Date(end - resourceRanges[range] * 1000).toISOString().replace('.000Z', 'Z'), end: new Date(end).toISOString().replace('.000Z', 'Z') }
}
export function validateEngineResources(value, owners, metricsForOwner, params = null) {
  const start = Date.parse(value?.start), end = Date.parse(value?.end), queried = Date.parse(value?.queried_at), step = value?.step_seconds
  if (!Array.isArray(value?.data) || value.data.length !== owners.length || !Number.isSafeInteger(value.policy_version) || value.policy_version < 0 || value.lookback_seconds !== 300 || !Number.isFinite(queried) || !Number.isFinite(start) || start % 1000 || !Number.isFinite(end) || end % 1000 || end > queried || start > end || !Number.isSafeInteger(step) || step < 15 || step % 15) invalid()
  if (params ? start < Date.parse(params.start) || end !== Date.parse(params.end) : start !== end) invalid()
  const count = Math.floor((end - start) / (step * 1000)) + 1
  if (count < 1 || count > 1000 || owners.reduce((sum,owner)=>sum+count*metricsForOwner(owner).length,0) > 20000) invalid()
  const seen = new Set()
  for (const row of value.data) {
    const owner = owners.find(item => item.deployment_id === row?.subject?.deployment_id)
    const metrics = owner ? metricsForOwner(owner) : []
    if (!owner || seen.has(owner.deployment_id) || row.subject.kind !== 'engine_deployment' || row.subject.engine_type !== owner.engine_type || row.subject.display_name !== owner.display_name || row.node_id !== owner.node_id || !Array.isArray(row.series) || row.series.length !== metrics.length) invalid()
    seen.add(owner.deployment_id)
    const state = row.collection?.state, stamp = Date.parse(row.collection?.sampled_at)
    if (!['collecting', 'failed', 'stale', 'no_sample', 'snapshot_unavailable', 'not_active', 'not_connected', 'source_unavailable'].includes(state)) invalid()
    if (['collecting', 'failed', 'stale', 'snapshot_unavailable'].includes(state)) {
      if (!Number.isFinite(stamp) || stamp > queried || (state === 'stale' ? queried - stamp <= 60000 : queried - stamp > 60000)) invalid()
    } else if (row.collection.sampled_at !== null) invalid()
    const keys = new Set()
    for (const series of row.series) {
      const metric = metrics.find(item => item.key === series.metric_key)
      if (!metric || keys.has(metric.key) || series.unit !== metric.unit || series.window_seconds !== metric.windowSeconds || !series.dimensions || Array.isArray(series.dimensions) || Object.keys(series.dimensions).length || !Array.isArray(series.points) || series.points.length !== count) invalid()
      keys.add(metric.key)
      for (const [index, point] of series.points.entries()) {
        const evaluated = Date.parse(point?.evaluated_at), sampled = Date.parse(point?.sampled_at)
        if (evaluated !== start + index * step * 1000 || !['valid', 'stale', 'no_data', 'not_active', 'not_connected', 'source_unavailable', 'no_sample'].includes(point.data_state)) invalid()
        if (['valid', 'stale'].includes(point.data_state)) {
          if (typeof point.value !== 'number' || !Number.isFinite(point.value) || point.value < 0 || (metric.unit==='percent' && metric.key!=='redis.memory.used_percent' && point.value>100) || (['connections','drives','objects','buckets','requests'].includes(metric.unit) && !Number.isSafeInteger(point.value)) || (metric.unit==='status' && ![0,1].includes(point.value)) || !Number.isFinite(sampled) || sampled > evaluated || (point.data_state === 'valid' ? evaluated - sampled > 60000 : evaluated - sampled <= 60000) || (!params && state !== 'collecting')) invalid()
        } else if (point.value !== null || point.sampled_at !== null) invalid()
      }
    }
  }
  return value
}

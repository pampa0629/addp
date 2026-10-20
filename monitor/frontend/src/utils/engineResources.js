import { autoRefreshOptions } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { isTargetUUID } from './monitoringTargets.js'
import { resourceRanges } from './nodeResources.js'

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
const invalid = () => { throw new Error('invalid_resource_response') }
export function resolveEngineRoute(input = {}) {
  const page = typeof input.page === 'string' && /^[1-9]\d{0,5}$/.test(input.page) ? Number(input.page) : 1
  const deploymentID = isTargetUUID(input.deployment_id) ? input.deployment_id : ''
  const metric = engineMetrics.some(item => item.key === input.metric) ? input.metric : engineMetrics[8].key
  const range = typeof input.range === 'string' && Object.hasOwn(resourceRanges, input.range) ? input.range : '5m'
  const refresh = autoRefreshOptions.includes(input.refresh) ? input.refresh : '15'
  const search = typeof input.search === 'string' && input.search.length <= 255 && !/[\u0000-\u001f]/u.test(input.search) ? input.search.trim() : ''
  const query = { ...(page !== 1 ? { page: String(page) } : {}), ...(search ? { search } : {}), ...(refresh !== '15' ? { refresh } : {}), ...(deploymentID ? { deployment_id: deploymentID, ...(metric !== engineMetrics[8].key ? { metric } : {}), ...(range !== '5m' ? { range } : {}) } : {}) }
  return { page, deploymentID, metric, range, refresh, search, query, changed: Object.keys(input).length !== Object.keys(query).length || Object.keys(query).some(key => query[key] !== input[key]) }
}
export function validateEngineDeployment(row) {
  if (!isTargetUUID(row?.deployment_id) || typeof row.display_name !== 'string' || !row.display_name || row.display_name.length > 1024 || row.engine_type !== 'postgresql' || typeof row.enabled !== 'boolean' || !Number.isSafeInteger(row.version) || row.version < 1 || !['bound', 'rejected'].includes(row.host_binding_state) || (row.host_binding_state === 'bound' ? !isTargetUUID(row.node_id) : row.node_id !== '')) invalid()
  return row
}
export function validateEnginePage(value, page) {
  if (!Array.isArray(value?.data) || value.data.length > 20 || !Number.isSafeInteger(value.total) || value.total < 0 || value.page !== page || value.page_size !== 20 || value.total < (page - 1) * 20 + value.data.length && value.data.length) invalid()
  const seen = new Set()
  for (const row of value.data) { validateEngineDeployment(row); if (seen.has(row.deployment_id)) invalid(); seen.add(row.deployment_id) }
  return value
}
export function engineTrendParameters(id, metric, range, queriedAt) {
  const end = Math.floor(Date.parse(queriedAt) / 1000) * 1000
  if (!isTargetUUID(id) || !engineMetrics.some(item => item.key === metric) || !Object.hasOwn(resourceRanges, range) || !Number.isFinite(end)) invalid()
  return { deployment_id: id, metrics: metric, start: new Date(end - resourceRanges[range] * 1000).toISOString().replace('.000Z', 'Z'), end: new Date(end).toISOString().replace('.000Z', 'Z') }
}
export function validateEngineResources(value, owners, metrics, params = null) {
  const start = Date.parse(value?.start), end = Date.parse(value?.end), queried = Date.parse(value?.queried_at), step = value?.step_seconds
  if (!Array.isArray(value?.data) || value.data.length !== owners.length || !Number.isSafeInteger(value.policy_version) || value.policy_version < 0 || value.lookback_seconds !== 300 || !Number.isFinite(queried) || !Number.isFinite(start) || start % 1000 || !Number.isFinite(end) || end % 1000 || end > queried || start > end || !Number.isSafeInteger(step) || step < 15 || step % 15) invalid()
  if (params ? start < Date.parse(params.start) || end !== Date.parse(params.end) : start !== end) invalid()
  const count = Math.floor((end - start) / (step * 1000)) + 1
  if (count < 1 || count > 1000 || count * owners.length * metrics.length > 20000) invalid()
  const seen = new Set()
  for (const row of value.data) {
    const owner = owners.find(item => item.deployment_id === row?.subject?.deployment_id)
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
          if (typeof point.value !== 'number' || !Number.isFinite(point.value) || point.value < 0 || (metric.unit === 'connections' && !Number.isSafeInteger(point.value)) || !Number.isFinite(sampled) || sampled > evaluated || (point.data_state === 'valid' ? evaluated - sampled > 60000 : evaluated - sampled <= 60000) || (!params && state !== 'collecting')) invalid()
        } else if (point.value !== null || point.sampled_at !== null) invalid()
      }
    }
  }
  return value
}

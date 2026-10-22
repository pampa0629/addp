import { normalizeRuntimeInstanceStopReason } from '../../../../common-frontend/basic/src/utils/runtimeInstanceStopReason.mjs'
import { resolveRuntimeInstanceTime, runtimeInstanceTimeParams } from '../../../../common-frontend/basic/src/utils/runtimeInstanceTime.mjs'
import { autoRefreshOptions } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { resourceRanges } from './nodeResources.js'

export const processMetrics = [
  { key: 'process.cpu.core_equivalents', name: 'cpu', unit: 'cores', windowSeconds: 60 },
  { key: 'process.memory.resident_bytes', name: 'memory', unit: 'bytes', windowSeconds: 0 },
  { key: 'process.uptime_seconds', name: 'uptime', unit: 'seconds', windowSeconds: 0 }
]
export const processRoles = ['backend', 'worker', 'scheduler', 'ingress']
export function processRoleLabelKey(instance) {
  const runtime = instance.metadata?.runtime_name
  return instance.role === 'worker' && ['scan', 'sync', 'continuous_sync'].includes(runtime)
    ? `monitor.process.workerTypes.${runtime}`
    : `monitor.process.roles.${instance.role}`
}
export function resolveProcessRoute(input = {}) {
  const time = resolveRuntimeInstanceTime(input)
  const page = typeof input.page === 'string' && /^[1-9]\d{0,6}$/.test(input.page) ? Number(input.page) : 1
  const module = typeof input.module_name === 'string' && /^[a-z][a-z0-9_-]{0,49}$/.test(input.module_name) ? input.module_name : ''
  const registeredHost = typeof input.registered_host === 'string' && input.registered_host.length <= 255 && !/[\x00-\x1f\x7f]/.test(input.registered_host) ? input.registered_host.trim().toLowerCase() : ''
  const nodeName = typeof input.node_name === 'string' ? input.node_name.trim() : ''
  const nodeIP = typeof input.node_ip === 'string' ? input.node_ip.trim() : ''
  const role = processRoles.includes(input.role) ? input.role : ''
  const stopReason = normalizeRuntimeInstanceStopReason(input.stop_reason)
  const status = ['up', 'down', 'all'].includes(input.status) ? input.status : 'up'
  const declared = ['true', 'false', 'all'].includes(input.process_metrics_declared) ? input.process_metrics_declared : 'true'
  const instanceID = typeof input.instance_id === 'string' && /^[1-9]\d{0,15}$/.test(input.instance_id) && Number.isSafeInteger(Number(input.instance_id)) ? Number(input.instance_id) : 0
  const range = typeof input.range === 'string' && Object.hasOwn(resourceRanges, input.range) ? input.range : '5m'
  const refresh = typeof input.refresh === 'string' && autoRefreshOptions.includes(input.refresh) ? input.refresh : '15'
  const query = { ...time.query, ...(refresh !== '15' ? { refresh } : {}), ...(page !== 1 ? { page: String(page) } : {}), ...(module ? { module_name: module } : {}), ...(registeredHost ? { registered_host: registeredHost } : {}), ...(nodeName ? { node_name: nodeName } : {}), ...(nodeIP ? { node_ip: nodeIP } : {}), ...(role ? { role } : {}), ...(stopReason ? { stop_reason: stopReason } : {}), ...(status !== 'up' ? { status } : {}), ...(declared !== 'true' ? { process_metrics_declared: declared } : {}), ...(instanceID ? { instance_id: String(instanceID), ...(range !== '5m' ? { range } : {}) } : {}) }
  const params = { page, page_size: 20, ...(module ? { module_name: module } : {}), ...(registeredHost ? { registered_host: registeredHost } : {}), ...(nodeName ? { node_name: nodeName } : {}), ...(nodeIP ? { node_ip: nodeIP } : {}), ...(role ? { role } : {}), ...(stopReason ? { stop_reason: stopReason } : {}), ...(status !== 'all' ? { status } : {}), ...(declared !== 'all' ? { process_metrics_declared: declared } : {}) }
  return { timeBasis: time.timeBasis, period: time.period, from: time.from, to: time.to, page, module, registeredHost, nodeName, nodeIP, role, stopReason, status, declared, instanceID, range, refresh, query, params, changed: Object.keys(input).length !== Object.keys(query).length || Object.keys(query).some(key => input[key] !== query[key]) }
}
export function processInstanceParameters(state, now = Date.now()) {
  return { ...state.params, ...runtimeInstanceTimeParams(state, now) }
}
const invalid = () => { throw new Error('invalid_resource_response') }
export function processTrendParameters(instanceID, range, queriedAt) {
  const end = Math.floor(Date.parse(queriedAt) / 1000) * 1000
  if (!Number.isSafeInteger(instanceID) || instanceID < 1 || typeof range !== 'string' || !Object.hasOwn(resourceRanges, range) || !Number.isFinite(end)) invalid()
  return { instance_id: String(instanceID), start: new Date(end - resourceRanges[range] * 1000).toISOString().replace('.000Z', 'Z'), end: new Date(end).toISOString().replace('.000Z', 'Z') }
}
export function validateProcessTrend(value, owner, params) {
  const subject = value?.subject, start = Date.parse(value?.start), end = Date.parse(value?.end), queried = Date.parse(value?.queried_at), step = value?.step_seconds
  const started = Date.parse(owner.process_started_at), fraction = typeof owner.process_started_at === 'string' ? owner.process_started_at.match(/\.(\d+)(?:Z|[+-]\d{2}:\d{2})$/)?.[1] : ''
  const first = Math.floor(started / 1000) * 1000 + (fraction && /[1-9]/.test(fraction) ? 1000 : 0)
  if (!Number.isFinite(started) || start !== Math.min(end, Math.max(Date.parse(params.start), first))) invalid()
  if (!subject || subject.kind !== 'module_instance' || subject.id !== owner.id || subject.module_name !== owner.module_name || subject.instance_id !== owner.instance_id || subject.role !== owner.role || value.node_id !== owner.node_id || value.lookback_seconds !== 300 || !Number.isSafeInteger(value.policy_version) || value.policy_version < 0 || !Number.isFinite(queried) || !Number.isFinite(start) || start % 1000 || start < Date.parse(params.start) || start > end || end !== Date.parse(params.end) || end > queried || !Number.isSafeInteger(step) || step < 15 || step % 15 || !Array.isArray(value.series) || value.series.length !== 2) invalid()
  const count = Math.floor((end - start) / (step * 1000)) + 1, seen = new Set()
  if (count < 1 || count > 1000) invalid()
  for (const series of value.series) {
    const metric = processMetrics.slice(0, 2).find(item => item.key === series.metric_key)
    if (!metric || seen.has(metric.key) || series.unit !== metric.unit || series.window_seconds !== metric.windowSeconds || !series.dimensions || Object.keys(series.dimensions).length || !Array.isArray(series.points) || series.points.length !== count) invalid()
    seen.add(metric.key)
    for (const [index, point] of series.points.entries()) {
      const evaluated = Date.parse(point?.evaluated_at), sampled = Date.parse(point?.sampled_at)
      if (evaluated !== start + index * step * 1000 || !['valid', 'stale', 'no_data', 'not_active', 'not_connected'].includes(point.data_state)) invalid()
      if (['valid', 'stale'].includes(point.data_state)) {
        if (typeof point.value !== 'number' || !Number.isFinite(point.value) || point.value < 0 || !Number.isFinite(sampled) || sampled > evaluated || (point.data_state === 'valid' ? evaluated - sampled > 60000 : evaluated - sampled <= 60000)) invalid()
      } else if (point.value !== null || point.sampled_at !== null) invalid()
    }
  }
  return value
}
export function validateProcessInstances(value, page, filters = {}, pageSize = 20) {
  if (!Array.isArray(value?.data) || value.data.length > pageSize || !Number.isSafeInteger(value.total) || value.total < value.data.length || value.page !== page || value.page_size !== pageSize) invalid()
  const ids = new Set(), names = new Set()
  for (const row of value.data) {
    const identity = JSON.stringify([row.module_name, row.instance_id])
    if (!Number.isSafeInteger(row.id) || row.id < 1 || ids.has(row.id) || names.has(identity) || typeof row.module_name !== 'string' || !row.module_name || typeof row.instance_id !== 'string' || !row.instance_id || !processRoles.includes(row.role) || !['up', 'down'].includes(row.status) || typeof row.node_id !== 'string' || typeof row.process_metrics_declared !== 'boolean') invalid()
    if ((filters.status && row.status !== filters.status) || (filters.process_metrics_declared !== undefined && String(row.process_metrics_declared) !== filters.process_metrics_declared)) invalid()
    if (filters.stop_reason && (row.status !== 'down' || row.stop_reason !== filters.stop_reason)) invalid()
    ids.add(row.id); names.add(identity)
  }
  return value
}
export function validateProcessSummaries(value, instances) {
  if (!Array.isArray(value?.data) || value.data.length !== instances.length) invalid()
  const expected = new Map(instances.map(row => [row.id, row])), result = new Map()
  const anchor = value.data[0]
  for (const row of value.data) {
    const subject = row?.subject, owner = expected.get(subject?.id)
    const queried = Date.parse(row.queried_at), sample = Date.parse(row.collection?.sampled_at)
    if (!owner || result.has(subject.id) || subject.kind !== 'module_instance' || subject.module_name !== owner.module_name || subject.instance_id !== owner.instance_id || subject.role !== owner.role || row.node_id !== owner.node_id || !Number.isSafeInteger(row.policy_version) || row.policy_version < 0 || row.policy_version !== anchor.policy_version || row.queried_at !== anchor.queried_at || !Number.isFinite(queried) || row.lookback_seconds !== 300) invalid()
    const state = row.collection?.state
    if (!['collecting', 'failed', 'stale', 'identity_mismatch', 'not_active', 'not_connected', 'no_sample'].includes(state)) invalid()
    const sampled = ['collecting', 'failed', 'stale', 'identity_mismatch'].includes(state)
    if (sampled ? !Number.isFinite(sample) || sample > queried || (state === 'stale' ? queried - sample <= 60000 : queried - sample > 60000) : row.collection.sampled_at !== null) invalid()
    if (!Array.isArray(row.series) || row.series.length !== processMetrics.length) invalid()
    const metrics = new Set()
    for (const series of row.series) {
      const metric = processMetrics.find(item => item.key === series.metric_key)
      if (!metric || metrics.has(metric.key) || series.unit !== metric.unit || series.window_seconds !== metric.windowSeconds || !series.dimensions || Object.keys(series.dimensions).length || !Array.isArray(series.points) || series.points.length !== 1) invalid()
      metrics.add(metric.key)
      const point = series.points[0], evaluated = Date.parse(point.evaluated_at), at = Date.parse(point.sampled_at)
      if (!['valid', 'stale', 'no_data', 'not_active', 'not_connected'].includes(point.data_state) || !Number.isFinite(evaluated) || evaluated > queried || queried - evaluated > 6000) invalid()
      if (['valid', 'stale'].includes(point.data_state)) {
        if (typeof point.value !== 'number' || !Number.isFinite(point.value) || point.value < 0 || !Number.isFinite(at) || at > evaluated || (point.data_state === 'valid' ? queried - at > 60000 : queried - at <= 60000)) invalid()
      } else if (point.value !== null || point.sampled_at !== null) invalid()
      if (state !== 'collecting' && point.data_state === 'valid') invalid()
    }
    result.set(subject.id, row)
  }
  return result
}

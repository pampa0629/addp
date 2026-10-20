import { autoRefreshOptions } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { currentResourceValue } from './nodeResources.js'

export const httpMetrics = [
  { key: 'http.requests.per_second', name: 'qps', unit: 'requests_per_second' },
  { key: 'http.failures.percent', name: 'failure', unit: 'percent' },
  { key: 'http.client_errors.percent', name: 'clientError', unit: 'percent' },
  { key: 'http.duration.mean_seconds', name: 'mean', unit: 'seconds' },
  { key: 'http.duration.p95_seconds', name: 'p95', unit: 'seconds' },
  { key: 'http.duration.p99_seconds', name: 'p99', unit: 'seconds' },
  { key: 'http.active_requests', name: 'active', unit: 'requests' }
]
const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS', 'CONNECT', 'TRACE', 'QUERY', '_OTHER']
const invalid = () => { throw new Error('invalid_http_quality_response') }
export function resolveHTTPRoute(query = {}) {
  const page = typeof query.page === 'string' && /^[1-9]\d{0,5}$/.test(query.page) ? Number(query.page) : 1
  const module = typeof query.module_name === 'string' ? query.module_name.trim().slice(0, 50) : ''
  const role = query.role === 'ingress' ? 'ingress' : 'backend'
  const window = ['60', '300', '900'].includes(query.window) ? Number(query.window) : 60
  const refresh = autoRefreshOptions.includes(query.refresh) ? query.refresh : '15'
  const instanceID = typeof query.instance_id === 'string' && /^[1-9]\d{0,14}$/.test(query.instance_id) ? Number(query.instance_id) : null
  const route = instanceID && typeof query.route === 'string' && query.route.length <= 1024 && !/[\x00-\x1f\x7f]/.test(query.route) && (query.route === '' || query.route.startsWith('/')) && methods.includes(query.method) ? query.route : null
  const method = route !== null ? query.method : ''
  const sort = query.sort === 'qps' ? 'qps' : 'mean'
  const range = ['5m', '1h', '24h'].includes(query.range) ? query.range : '1h'
  const canonical = { ...(page === 1 ? {} : { page: String(page) }), ...(module ? { module_name: module } : {}), ...(role === 'backend' ? {} : { role }), ...(window === 60 ? {} : { window: String(window) }), ...(refresh === '15' ? {} : { refresh }), ...(sort === 'mean' ? {} : { sort }), ...(instanceID ? { instance_id: String(instanceID), ...(range === '1h' ? {} : { range }), ...(route !== null ? { route, method } : {}) } : {}) }
  return { page, module, role, window, refresh, instanceID, route, method, sort, range, query: canonical, changed: JSON.stringify(Object.entries(query).sort()) !== JSON.stringify(Object.entries(canonical).sort()), params: { page, page_size: 10, role, status: 'up', ...(module ? { module_name: module } : {}) } }
}

export function validateHTTPResponse(value, instances, window, grouped = false, filtered = null) {
  const start = Date.parse(value?.start), end = Date.parse(value?.end), step = value?.step_seconds
  if (!Array.isArray(value?.data) || value.data.length !== instances.length || value.window_seconds !== window || !Number.isFinite(start) || !Number.isFinite(end) || start % 1000 || end % 1000 || start > end || !Number.isInteger(step) || step < 15 || step % 15 || (grouped && (!Number.isInteger(value.ranking_limit) || value.ranking_limit < 1 || value.ranking_limit > 10))) invalid()
  const expected = new Map(instances.map(row => [row.id, row])), seen = new Set()
  for (const process of value.data) {
    const subject = process?.subject, owner = expected.get(subject?.id), sampled = Date.parse(process.collection?.sampled_at), queried = Date.parse(process.queried_at)
    if (!owner || seen.has(subject.id) || subject.kind !== 'module_instance' || subject.module_name !== owner.module_name || subject.instance_id !== owner.instance_id || subject.role !== owner.role || !Number.isSafeInteger(process.policy_version) || process.policy_version < 0 || !Number.isFinite(queried) || end > queried || process.policy_version !== value.data[0].policy_version || process.queried_at !== value.data[0].queried_at) invalid()
    seen.add(subject.id)
    const state = process.collection?.state
    if (!['collecting', 'failed', 'stale', 'identity_mismatch', 'not_active', 'not_connected', 'not_instrumented', 'no_sample'].includes(state) || !Array.isArray(process.observations) || process.observations.length > 16) invalid()
    if (['collecting', 'failed', 'stale', 'identity_mismatch', 'not_instrumented'].includes(state) ? !Number.isFinite(sampled) || sampled > queried || (state === 'stale' ? queried - sampled <= 60000 : queried - sampled > 60000) : process.collection.sampled_at !== null) invalid()
    if (state !== 'collecting' && process.observations.length) invalid()
    if ((!grouped && state === 'collecting' && process.observations.length !== 1) || (grouped && process.observations.length > value.ranking_limit)) invalid()
    const routes = new Set()
    for (const row of process.observations) {
      const identity = JSON.stringify([row.route, row.method]), catalog = grouped || filtered ? httpMetrics.slice(0, -1) : httpMetrics
      if (typeof row.route !== 'string' || row.route.length > 1024 || (row.route && !row.route.startsWith('/')) || routes.has(identity) || ((grouped || filtered) && !methods.includes(row.method)) || !Array.isArray(row.series) || row.series.length !== catalog.length) invalid()
      if (filtered ? row.route !== filtered.route || row.method !== filtered.method : !grouped && (row.route !== '' || row.method !== '')) invalid()
      routes.add(identity)
      const keys = new Set()
      for (const series of row.series) {
        const metric = catalog.find(item => item.key === series.metric_key)
        if (!metric || keys.has(metric.key) || metric.unit !== series.unit || series.window_seconds !== (metric.name === 'active' ? 0 : window) || !series.dimensions || Object.keys(series.dimensions).length || !Array.isArray(series.points) || series.points.length !== Math.floor((end - start) / (step * 1000)) + 1 || series.points.length > 1000) invalid()
        keys.add(metric.key)
        for (const [index, point] of series.points.entries()) {
          const at = Date.parse(point?.evaluated_at), sample = Date.parse(point?.sampled_at)
          if (at !== start + index * step * 1000 || !['valid', 'stale', 'no_data'].includes(point.data_state)) invalid()
          if (point.data_state === 'no_data' ? point.value !== null || point.sampled_at !== null : typeof point.value !== 'number' || !Number.isFinite(point.value) || point.value < 0 || (metric.unit === 'percent' && point.value > 100) || !Number.isFinite(sample) || sample > at || (point.data_state === 'valid' ? at - sample > 60000 : at - sample <= 60000)) invalid()
        }
      }
    }
  }
  return value
}

export function httpValue(observation, name) {
  const key = httpMetrics.find(metric => metric.name === name)?.key
  return currentResourceValue(observation?.series?.find(series => series.metric_key === key)?.points[0])
}
export function sortHTTPInterfaces(rows, name) {
  return [...rows].sort((a, b) => (httpValue(b, name) ?? -Infinity) - (httpValue(a, name) ?? -Infinity) || a.route.localeCompare(b.route) || a.method.localeCompare(b.method))
}

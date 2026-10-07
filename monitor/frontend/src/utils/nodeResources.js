import { resolveTargetRoute } from './monitoringTargets.js'

// Domain adaptation of Monitor's fixed scalar catalog, never arbitrary PromQL.
export const resourceMetrics = [
  { key: 'node.cpu.logical_cores', name: 'cores', unit: 'cores', precision: 0 },
  { key: 'node.memory.total_bytes', name: 'memoryTotal', unit: 'bytes', precision: 0 },
  { key: 'node.memory.available_bytes', name: 'memoryAvailable', unit: 'bytes', precision: 0 },
  { key: 'node.memory.used_percent', name: 'memoryUsed', unit: 'percent', precision: 2 },
  { key: 'node.load.average_1m', name: 'load1m', unit: 'load', precision: 2 },
  { key: 'node.load.average_5m', name: 'load5m', unit: 'load', precision: 2 },
  { key: 'node.load.average_15m', name: 'load15m', unit: 'load', precision: 2 },
  { key: 'node.uptime_seconds', name: 'uptime', unit: 'seconds', precision: 0 }
]
export const resourceRefreshOptions = ['15', '10', '30', '60', 'off']
export const resourceRanges = { '5m': 300, '1h': 3600, '24h': 86400, '7d': 604800 }
const defaultMetric = 'node.memory.used_percent'
export function resolveResourceRoute(query = {}, detail = false) {
  const pagination = resolveTargetRoute({ ...(query.page === undefined ? {} : { page: query.page }), ...(query.page_size === undefined ? {} : { page_size: query.page_size }) })
  const search = typeof query.search === 'string' ? query.search.trim().slice(0, 200) : ''
  const range = detail && typeof query.range === 'string' && Object.hasOwn(resourceRanges, query.range) ? query.range : '1h'
  const metric = detail && resourceMetrics.some(item => item.key === query.metric) ? query.metric : defaultMetric
  const refresh = detail && typeof query.refresh === 'string' && resourceRefreshOptions.includes(query.refresh) ? query.refresh : '15'
  const canonical = { ...pagination.query, ...(search ? { search } : {}), ...(detail && range !== '1h' ? { range } : {}), ...(detail && metric !== defaultMetric ? { metric } : {}), ...(detail && refresh !== '15' ? { refresh } : {}) }
  return { page: pagination.page, pageSize: pagination.pageSize, search, range, metric, refresh, query: canonical,
    changed: Object.keys(query).length !== Object.keys(canonical).length || Object.keys(canonical).some(key => query[key] !== canonical[key]) }
}
export function trendParameters(nodeID, metric, range, serverEnd) {
  const end = Date.parse(serverEnd)
  if (!Number.isFinite(end) || end % 1000 !== 0) throw new Error('invalid_resource_response')
  return { node_id: nodeID, metrics: metric, start: new Date(end - resourceRanges[range] * 1000).toISOString(), end: new Date(end).toISOString() }
}
export function currentResourceValue(point) {
  return point?.data_state === 'valid' && typeof point.value === 'number' && Number.isFinite(point.value) ? point.value : null
}
export function resourceChartRows(series) {
  return series.points.map(point => ({ evaluated_at: point.evaluated_at, value: currentResourceValue(point) }))
}
export function validateResourceResponse(value, nodeID, keys, trend = false) {
  if (value?.subject?.kind !== 'node' || value.subject.node_id !== nodeID || !Array.isArray(value.series) || value.series.length !== keys.length || !Number.isFinite(Date.parse(value.end)) || !Number.isFinite(Date.parse(value.queried_at))) throw new Error('invalid_resource_response')
  const seen = new Set()
  for (const series of value.series) {
    const definition = resourceMetrics.find(item => item.key === series.metric_key)
    if (!definition || !keys.includes(series.metric_key) || seen.has(series.metric_key) || series.unit !== definition.unit || !Array.isArray(series.points) || !series.points.length || series.points.length > (trend ? 1000 : 1)) throw new Error('invalid_resource_response')
    seen.add(series.metric_key)
    for (const point of series.points) {
      if (!['valid', 'stale', 'no_data', 'not_connected'].includes(point.data_state) || !Number.isFinite(Date.parse(point.evaluated_at)) || (point.sampled_at !== null && !Number.isFinite(Date.parse(point.sampled_at))) || (point.value !== null && (typeof point.value !== 'number' || !Number.isFinite(point.value))) || (point.data_state === 'valid' && (point.value === null || point.sampled_at === null))) throw new Error('invalid_resource_response')
    }
  }
  return value
}
export function resourceErrorKey(error) {
  const code = error.response?.data?.error_code
  const names = {
    observability_capability_disabled: 'disabled', observability_capability_unconfigured: 'unconfigured',
    observability_backend_unavailable: 'unavailable', observability_query_timeout: 'timeout',
    observability_query_budget_exceeded: 'budget', observability_query_concurrency_exceeded: 'busy'
  }
  const status = error.response?.status
  return `monitor.resources.errors.${names[code] || (status === 404 ? 'notFound' : status === 401 || status === 403 ? 'denied' : error.message === 'invalid_resource_response' ? 'invalidResponse' : 'unavailable')}`
}

import { resolveTargetRoute } from './monitoringTargets.js'

// Domain adaptation of Monitor's fixed scalar catalog, never arbitrary PromQL.
export const resourceMetrics = [
  { key: 'node.cpu.logical_cores', name: 'cores', unit: 'cores', precision: 0 },
  { key: 'node.cpu.busy_percent', name: 'cpuBusy', unit: 'percent', precision: 2, windowSeconds: 60 },
  { key: 'node.memory.total_bytes', name: 'memoryTotal', unit: 'bytes', precision: 0 },
  { key: 'node.memory.available_bytes', name: 'memoryAvailable', unit: 'bytes', precision: 0 },
  { key: 'node.memory.used_percent', name: 'memoryUsed', unit: 'percent', precision: 2 },
  { key: 'node.load.average_1m', name: 'load1m', unit: 'load', precision: 2 },
  { key: 'node.load.average_5m', name: 'load5m', unit: 'load', precision: 2 },
  { key: 'node.load.average_15m', name: 'load15m', unit: 'load', precision: 2 },
  { key: 'node.uptime_seconds', name: 'uptime', unit: 'seconds', precision: 0 }
]
export const filesystemMetrics = [
  { key: 'node.filesystem.used_percent', name: 'filesystemUsedPercent', unit: 'percent', precision: 2 },
  { key: 'node.filesystem.total_bytes', name: 'filesystemTotal', unit: 'bytes', precision: 0 },
  { key: 'node.filesystem.used_bytes', name: 'filesystemUsed', unit: 'bytes', precision: 0 },
  { key: 'node.filesystem.available_bytes', name: 'filesystemAvailable', unit: 'bytes', precision: 0 },
  { key: 'node.filesystem.free_bytes', name: 'filesystemFree', unit: 'bytes', precision: 0 }
]
export const inodeMetrics = [
  { key: 'node.filesystem.inodes_used_percent', name: 'inodeUsedPercent', unit: 'percent', precision: 2 },
  { key: 'node.filesystem.inodes_total', name: 'inodeTotal', unit: 'inodes', precision: 0 },
  { key: 'node.filesystem.inodes_free', name: 'inodeFree', unit: 'inodes', precision: 0 },
  { key: 'node.filesystem.inodes_used', name: 'inodeUsed', unit: 'inodes', precision: 0 }
]
export const mountMetrics = [...filesystemMetrics, ...inodeMetrics]
export const allResourceMetrics = [...resourceMetrics, ...mountMetrics]
const dimensionKeys = ['device', 'mountpoint', 'fstype']
export function validResourceDimensions(value, allowEmpty = true) {
  return value && typeof value === 'object' && !Array.isArray(value) && ((allowEmpty && Object.keys(value).length === 0) ||
    (Object.keys(value).length === 3 && dimensionKeys.every(key => typeof value[key] === 'string' && value[key].length > 0 && value[key].length <= 4096 && !value[key].includes('\0')) && value.mountpoint.startsWith('/')))
}
export function resourceDimensionID(dimensions) { return JSON.stringify(dimensionKeys.map(key => dimensions[key] ?? null)) }
export function filesystemRows(...values) {
  const rows = new Map()
  for (const series of values.filter(Boolean).flatMap(value => value.series)) {
    if (!validResourceDimensions(series.dimensions, false)) continue
    const key = resourceDimensionID(series.dimensions)
    if (!rows.has(key)) rows.set(key, { key, dimensions: series.dimensions, metrics: {} })
    rows.get(key).metrics[series.metric_key] = series.points[0]
  }
  return [...rows.values()]
}
export const resourceRefreshOptions = ['15', '10', '30', '60', 'off']
export const resourceRanges = { '5m': 300, '1h': 3600, '24h': 86400, '7d': 604800 }
const defaultMetric = 'node.memory.used_percent'
export function resolveResourceRoute(query = {}, detail = false) {
  const pagination = resolveTargetRoute({ ...(query.page === undefined ? {} : { page: query.page }), ...(query.page_size === undefined ? {} : { page_size: query.page_size }) })
  const search = typeof query.search === 'string' ? query.search.trim().slice(0, 200) : ''
  const range = detail && typeof query.range === 'string' && Object.hasOwn(resourceRanges, query.range) ? query.range : '1h'
  let metric = detail && allResourceMetrics.some(item => item.key === query.metric) ? query.metric : defaultMetric
  let dimensions = {}, invalidDimensions = false
  const hasDimensions = detail && dimensionKeys.some(key => Object.hasOwn(query, key))
  if (metric.startsWith('node.filesystem.')) {
    const selected = Object.fromEntries(dimensionKeys.map(key => [key, query[key]]))
    if (validResourceDimensions(selected, false)) dimensions = selected
    else invalidDimensions = true
  }
  if (hasDimensions && !metric.startsWith('node.filesystem.')) invalidDimensions = true
  const canonicalDimensions = invalidDimensions ? Object.fromEntries(dimensionKeys.filter(key => Object.hasOwn(query, key)).map(key => [key, query[key]])) : dimensions
  const refresh = detail && typeof query.refresh === 'string' && resourceRefreshOptions.includes(query.refresh) ? query.refresh : '15'
  const canonical = { ...pagination.query, ...canonicalDimensions, ...(search ? { search } : {}), ...(detail && range !== '1h' ? { range } : {}), ...(detail && metric !== defaultMetric ? { metric } : {}), ...(detail && refresh !== '15' ? { refresh } : {}) }
  return { page: pagination.page, pageSize: pagination.pageSize, search, range, metric, refresh, dimensions, invalidDimensions, query: canonical,
    changed: Object.keys(query).length !== Object.keys(canonical).length || Object.keys(canonical).some(key => query[key] !== canonical[key]) }
}
export function trendParameters(nodeID, metric, range, serverEnd, dimensions = {}) {
  const end = Date.parse(serverEnd)
  if (!Number.isFinite(end) || end % 1000 !== 0) throw new Error('invalid_resource_response')
  return { node_id: nodeID, metrics: metric, ...dimensions, start: new Date(end - resourceRanges[range] * 1000).toISOString(), end: new Date(end).toISOString() }
}
export function currentResourceValue(point) {
  return point?.data_state === 'valid' && typeof point.value === 'number' && Number.isFinite(point.value) ? point.value : null
}
export function resourceChartRows(series) {
  return series.points.map(point => ({ evaluated_at: point.evaluated_at, value: currentResourceValue(point) }))
}
export function validateResourceResponse(value, nodeID, keys, trend = false, dimensions = {}) {
  if (value?.subject?.kind !== 'node' || value.subject.node_id !== nodeID || !Array.isArray(value.series) || value.series.length < keys.length || value.series.length > 100 || !Number.isFinite(Date.parse(value.end)) || !Number.isFinite(Date.parse(value.queried_at))) throw new Error('invalid_resource_response')
  const seen = new Set()
  for (const series of value.series) {
    if (!validResourceDimensions(series.dimensions)) throw new Error('invalid_resource_response')
    const definition = allResourceMetrics.find(item => item.key === series.metric_key)
    if (!definition || !keys.includes(series.metric_key) || seen.has(series.metric_key + resourceDimensionID(series.dimensions)) || series.unit !== definition.unit || series.window_seconds !== (definition.windowSeconds || 0) || !Array.isArray(series.points) || !series.points.length || series.points.length > (trend ? 1000 : 1)) throw new Error('invalid_resource_response')
    const filesystem = series.metric_key.startsWith('node.filesystem.')
    if (!validResourceDimensions(series.dimensions) || (!filesystem && Object.keys(series.dimensions).length) || (Object.keys(dimensions).length && filesystem && resourceDimensionID(series.dimensions) !== resourceDimensionID(dimensions))) throw new Error('invalid_resource_response')
    seen.add(series.metric_key + resourceDimensionID(series.dimensions))
    for (const point of series.points) {
      if (!['valid', 'stale', 'no_data', 'not_connected'].includes(point.data_state) || !Number.isFinite(Date.parse(point.evaluated_at)) || (point.sampled_at !== null && !Number.isFinite(Date.parse(point.sampled_at))) || (point.value !== null && (typeof point.value !== 'number' || !Number.isFinite(point.value))) || (point.data_state === 'valid' && (point.value === null || point.sampled_at === null))) throw new Error('invalid_resource_response')
      if ((point.value !== null && (point.value < 0 || (series.unit === 'percent' && point.value > 100) || (series.unit === 'inodes' && !Number.isSafeInteger(point.value)))) || (['no_data', 'not_connected'].includes(point.data_state) && (point.value !== null || point.sampled_at !== null)) || (point.data_state === 'stale' && (point.value === null || point.sampled_at === null)) || (filesystem && Object.keys(series.dimensions).length === 0 && !['no_data', 'not_connected'].includes(point.data_state))) throw new Error('invalid_resource_response')
    }
  }
  const metricGroups = keys.map(key => value.series.filter(series => series.metric_key === key).map(series => resourceDimensionID(series.dimensions)).sort())
  if (metricGroups.some((groups, index) => groups.length === 0 || (!keys[index].startsWith('node.filesystem.') && groups.length !== 1))) throw new Error('invalid_resource_response')
  const filesystemGroups = metricGroups.filter((_, index) => keys[index].startsWith('node.filesystem.')).map(groups => JSON.stringify(groups))
  if (new Set(filesystemGroups).size > 1 || value.series.reduce((total, series) => total + series.points.length, 0) > 20000) throw new Error('invalid_resource_response')
  if (inodeMetrics.every(metric => keys.includes(metric.key))) {
    for (const group of new Set(value.series.filter(series => series.metric_key === inodeMetrics[0].key).map(series => resourceDimensionID(series.dimensions)))) {
      const mounted = Object.fromEntries(value.series.filter(series => resourceDimensionID(series.dimensions) === group).map(series => [series.metric_key, series]))
      for (let i = 0; i < mounted[inodeMetrics[0].key].points.length; i++) {
        const points = inodeMetrics.map(metric => mounted[metric.key].points[i])
        if (points.some(point => !point) || new Set(points.map(point => point.data_state)).size !== 1 || new Set(points.map(point => point.sampled_at)).size !== 1) throw new Error('invalid_resource_response')
        if (['valid', 'stale'].includes(points[0].data_state)) {
          const [percentage, total, free, used] = points.map(point => point.value)
          if (total <= 0 || free > total || used !== total - free || Math.abs(percentage - 100 * used / total) > 1e-8) throw new Error('invalid_resource_response')
        }
      }
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

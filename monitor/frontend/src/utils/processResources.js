export const processMetrics = [
  { key: 'process.cpu.core_equivalents', name: 'cpu', unit: 'cores', windowSeconds: 60 },
  { key: 'process.memory.resident_bytes', name: 'memory', unit: 'bytes', windowSeconds: 0 },
  { key: 'process.uptime_seconds', name: 'uptime', unit: 'seconds', windowSeconds: 0 }
]
export const processRoles = ['backend', 'worker', 'scheduler', 'ingress']
export function resolveProcessRoute(input = {}) {
  const page = typeof input.page === 'string' && /^[1-9]\d{0,6}$/.test(input.page) ? Number(input.page) : 1
  const module = typeof input.module_name === 'string' && /^[a-z][a-z0-9_-]{0,49}$/.test(input.module_name) ? input.module_name : ''
  const role = processRoles.includes(input.role) ? input.role : ''
  const status = ['up', 'down'].includes(input.status) ? input.status : ''
  const query = { ...(page !== 1 ? { page: String(page) } : {}), ...(module ? { module_name: module } : {}), ...(role ? { role } : {}), ...(status ? { status } : {}) }
  return { page, module, role, status, query, changed: Object.keys(input).length !== Object.keys(query).length || Object.keys(query).some(key => input[key] !== query[key]) }
}
const invalid = () => { throw new Error('invalid_resource_response') }
export function validateProcessInstances(value, page) {
  if (!Array.isArray(value?.data) || value.data.length > 20 || !Number.isSafeInteger(value.total) || value.total < value.data.length || value.page !== page || value.page_size !== 20) invalid()
  const ids = new Set(), names = new Set()
  for (const row of value.data) {
    const identity = JSON.stringify([row.module_name, row.instance_id])
    if (!Number.isSafeInteger(row.id) || row.id < 1 || ids.has(row.id) || names.has(identity) || typeof row.module_name !== 'string' || !row.module_name || typeof row.instance_id !== 'string' || !row.instance_id || !processRoles.includes(row.role) || !['up', 'down'].includes(row.status) || typeof row.node_id !== 'string' || typeof row.process_metrics_declared !== 'boolean') invalid()
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

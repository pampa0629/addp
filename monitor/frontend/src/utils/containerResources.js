export const containerMetrics = [
  { key: 'container.cpu.core_equivalents', name: 'cpu', unit: 'cores', window: 60 },
  { key: 'container.memory.current_bytes', name: 'memory', unit: 'bytes', window: 0 },
  { key: 'container.cpu.limit_percent', name: 'cpuRatio', unit: 'percent', window: 60 },
  { key: 'container.memory.limit_percent', name: 'memoryRatio', unit: 'percent', window: 0 }
]
export function validateContainerObservation(value, owner) {
  const invalid = () => { throw new Error('invalid_resource_response') }
  const row = value?.observation, subject = row?.subject, queried = Date.parse(row?.queried_at)

  if (!['collecting', 'failed', 'missing', 'identity_mismatch', 'stale', 'not_active', 'not_connected'].includes(row?.collection?.state)) invalid()
  if (!row || subject?.kind !== 'module_instance' || subject.id !== owner.id || subject.module_name !== owner.module_name || subject.instance_id !== owner.instance_id || subject.role !== owner.role || row.node_id !== owner.node_id || !Number.isFinite(queried) || !Number.isSafeInteger(row.policy_version) || row.policy_version < 0 || row.lookback_seconds !== 300 || !['verified', 'unverified'].includes(value.proof_state) || typeof value.cpu_window_verified !== 'boolean' || !Array.isArray(row.series) || row.series.length !== 4) invalid()
  if (value.cpu_upper_cores !== null && (typeof value.cpu_upper_cores !== 'number' || !Number.isFinite(value.cpu_upper_cores) || value.cpu_upper_cores <= 0 || value.cpu_upper_cores > 2 ** 20)) invalid()
  if (value.memory_upper_bytes !== null && (!Number.isSafeInteger(value.memory_upper_bytes) || value.memory_upper_bytes <= 0)) invalid()
  const proofAt = Date.parse(value.proof_observed_at)
  if (value.cpu_window_verified && (value.proof_state !== 'verified' || value.cpu_upper_cores === null)) invalid()
  if (value.proof_state === 'verified' ? !Number.isFinite(proofAt) || proofAt > queried || queried - proofAt > 90000 : value.proof_observed_at !== null || value.cpu_upper_cores !== null || value.memory_upper_bytes !== null || value.cpu_window_verified) invalid()
  const seen = new Set()
  for (const series of row.series) {
    const metric = containerMetrics.find(item => item.key === series.metric_key)
    if (!metric || seen.has(metric.key) || series.unit !== metric.unit || series.window_seconds !== metric.window || !series.dimensions || Object.keys(series.dimensions).length || series.points?.length !== 1) invalid()
    seen.add(metric.key)
    const point = series.points[0], evaluated = Date.parse(point.evaluated_at), sampled = Date.parse(point.sampled_at)
    if (!Number.isFinite(evaluated) || evaluated > queried || !['valid', 'stale', 'no_data', 'not_active', 'not_connected'].includes(point.data_state)) invalid()
    if (['valid', 'stale'].includes(point.data_state)) {
      if (typeof point.value !== 'number' || !Number.isFinite(point.value) || point.value < 0 || !Number.isFinite(sampled) || sampled > evaluated || point.data_state === 'valid' && queried - sampled > 60000) invalid()
      if (metric.name.endsWith('Ratio') && (value.proof_state !== 'verified' || metric.name === 'cpuRatio' && (!value.cpu_upper_cores || !value.cpu_window_verified) || metric.name === 'memoryRatio' && !value.memory_upper_bytes)) invalid()
    } else if (point.value !== null || point.sampled_at !== null) invalid()
  }
  return value
}

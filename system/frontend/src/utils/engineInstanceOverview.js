import { parseEngineCapabilities } from '@common-ui'

export const supportsEngineInstanceOverview = engine => parseEngineCapabilities(engine)?.instance_overview === true

// The HTTP boundary requires exact integer strings, including zero, and explicit missingness.
export function instanceOverviewSnapshot(value, engine) {
  if (value?.engine_id !== String(engine.id) || value.version !== engine.version ||
      typeof value.queried_at !== 'string' || !Number.isFinite(Date.parse(value.queried_at)) || !Array.isArray(value.metrics)) {
    throw new Error('invalid_instance_overview')
  }
  const keys = new Set()
  for (const metric of value.metrics) {
    if (typeof metric.key !== 'string' || !/^[a-z][a-z0-9_]*$/.test(metric.key) || keys.has(metric.key) ||
        !['database', 'server', 'account'].includes(metric.scope) ||
        !['bytes', 'count', 'operations_per_second'].includes(metric.unit) ||
        !['gauge', 'counter', 'rate'].includes(metric.semantics) ||
        !['available', 'permission_denied', 'unavailable'].includes(metric.status) ||
        (metric.scope_name !== undefined && typeof metric.scope_name !== 'string') ||
        (metric.status === 'available' ? typeof metric.value !== 'string' || !/^(0|[1-9][0-9]*)$/.test(metric.value) : metric.value !== undefined)) {
      throw new Error('invalid_instance_overview')
    }
    keys.add(metric.key)
  }
  return value
}

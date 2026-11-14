import { validateCDCBuffer, validateCDCPostgreSQL, validateCDCDelay, cdcTaskHistoryRanges } from './cdcTasks.js'

const invalid = () => { throw new Error('invalid_resource_response') }

export function cdcTaskHistoryParams(generation, range, now = Date.now()) {
  const end = Math.floor(now / 60000) * 60000
  return { generation, start: new Date(end - cdcTaskHistoryRanges[range] * 1000).toISOString(), end: new Date(end).toISOString() }
}

export function validateCDCTaskHistory(value, taskID, params) {
  const start = Date.parse(params.start), end = Date.parse(params.end), step = value?.step_seconds
  if (String(value?.task_id) !== String(taskID) || value.generation !== params.generation || Date.parse(value.start) !== start || Date.parse(value.end) !== end || !Number.isFinite(Date.parse(value.queried_at)) || Date.parse(value.queried_at) < end || !Number.isSafeInteger(step) || step !== Math.ceil((end-start)/60000/1000)*60 || !Array.isArray(value.points) || value.points.length !== Math.ceil((end-start)/(step*1000)) || value.points.length > 1000) invalid()
  value.points.forEach((point, index) => {
    const at = start + index * step * 1000, recorded = Date.parse(point.recorded_at)
    if (Date.parse(point.timestamp) !== at) invalid()
    if (point.recorded_at === null) {
      if (point.consumption_state !== '' || [point.buffer_observation, point.postgresql_observation, point.delay_observation].some(v => v !== null)) invalid()
    } else {
      if (!Number.isFinite(recorded) || recorded < at || recorded >= Math.min(at + step*1000,end) || !['running','paused','stopped'].includes(point.consumption_state)) invalid()
      validateCDCBuffer(point.buffer_observation)
      validateCDCPostgreSQL(point.postgresql_observation)
      validateCDCDelay(point.delay_observation)
      for (const sample of [point.buffer_observation,point.postgresql_observation,point.delay_observation]) if (sample && Date.parse(sample.sampled_at) > recorded) invalid()
    }
  })
  return value
}

export function cdcTaskHistoryValue(point, metric) {
  const observation = metric === 'lag_offsets' ? point.buffer_observation : metric === 'delay_seconds' ? point.delay_observation : point.postgresql_observation
  if (!point.recorded_at || observation?.status !== 'available' || Date.parse(observation.expires_at) <= Date.parse(point.recorded_at)) return null
  const value = observation[metric]
  if (value === null || value === undefined) return null
  if (metric === 'delay_seconds') return value
  return BigInt(value) <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(value) : null
}

export function cdcTaskHistoryPrecisionLimited(point, metric) {
  const observation = metric === 'lag_offsets' ? point.buffer_observation : point.postgresql_observation
  return metric !== 'delay_seconds' && observation?.status === 'available' && point.recorded_at && Date.parse(observation.expires_at) > Date.parse(point.recorded_at) && observation[metric] != null && BigInt(observation[metric]) > BigInt(Number.MAX_SAFE_INTEGER)
}

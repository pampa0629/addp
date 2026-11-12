import { autoRefreshOptions } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { isTargetUUID } from './monitoringTargets.js'
import { resourceRanges } from './nodeResources.js'

export const cdcMetrics = {
  redpanda: [
    ['disk_used_percent', 'diskUsed', 'percent'], ['unavailable_partitions', 'unavailablePartitions', 'partitions'],
    ['disk_free_bytes', 'diskFree', 'bytes'], ['disk_total_bytes', 'diskTotal', 'bytes'],
    ['consumer_group_pending_records', 'pendingRecords', 'records'], ['consumer_group_partitions', 'groupPartitions', 'partitions']
  ].map(([field, name, unit]) => ({ key: `cdc.redpanda.${field}`, name, unit })),
  'kafka-connect': [
    ['connectors', 'connectors', 'connectors'], ['failed_tasks', 'failedTasks', 'tasks'], ['source_delay_max_seconds', 'sourceDelay', 'seconds'],
    ['tasks', 'tasks', 'tasks'], ['failed_connectors', 'failedConnectors', 'connectors'], ['running_tasks', 'runningTasks', 'tasks'],
    ['paused_tasks', 'pausedTasks', 'tasks'], ['offset_commit_tasks', 'offsetTasks', 'tasks'], ['offset_commit_failure_percent', 'offsetFailure', 'percent'],
    ['streaming_connectors', 'streamingConnectors', 'connectors'], ['streaming_connected', 'connected', 'connectors'], ['source_delay_connectors', 'delaySamples', 'connectors']
  ].map(([field, name, unit]) => ({ key: `cdc.connect.${field}`, name, unit }))
}
export function resolveCDCRoute(input = {}) {
  const query = { view: 'resources' }
  const stage = ['kafka-connect', 'redpanda'].includes(input.stage) ? input.stage : 'kafka-connect'
  const metric = cdcMetrics[stage].find(item => item.key === input.metric) || cdcMetrics[stage][0]
  const range = Object.hasOwn(resourceRanges, input.range) ? input.range : '5m'
  const refresh = autoRefreshOptions.includes(input.refresh) ? input.refresh : '15'
  if (stage !== 'kafka-connect') query.stage = stage
  if (metric !== cdcMetrics[stage][0]) query.metric = metric.key
  if (range !== '5m') query.range = range
  if (refresh !== '15') query.refresh = refresh
  return { stage, metric, range, refresh, query, changed: JSON.stringify(Object.entries(input).sort()) !== JSON.stringify(Object.entries(query).sort()) }
}
const invalid = () => { throw new Error('invalid_resource_response') }
export function validateCDCResources(value, params = null, expected = null) {
  const start = Date.parse(value?.start), end = Date.parse(value?.end), queried = Date.parse(value?.queried_at), step = value?.step_seconds
  if (!['available', 'unavailable'].includes(value?.discovery_state) || !Array.isArray(value.data) || value.data.length > 2 || value.discovery_state === 'unavailable' && value.data.length || !Number.isFinite(start) || !Number.isFinite(end) || !Number.isFinite(queried) || start % 1000 || end % 1000 || start > end || end > queried || !Number.isSafeInteger(step) || step < 15 || step % 15 || !Number.isSafeInteger(value.policy_version) || value.policy_version < 0 || value.lookback_seconds !== 300) invalid()
  if (params ? value.data.length !== 1 || start < Date.parse(params.start) || end !== Date.parse(params.end) : start !== end) invalid()
  const count = Math.floor((end - start) / (step * 1000)) + 1, seen = new Set(), ids = new Set()
  if (count > 1000 || value.data.reduce((sum, row) => sum + (row.series?.length || 0) * count, 0) > 20000) invalid()
  for (const row of value.data) {
    const subject = row?.subject, kind = subject?.component_type
    if (!['redpanda', 'kafka-connect'].includes(kind) || !Number.isFinite(Date.parse(subject.started_at)) || Date.parse(subject.started_at) > queried || subject.kind !== 'infrastructure_component' || !isTargetUUID(subject.component_id) || seen.has(kind) || ids.has(subject.component_id) || expected && (subject.component_id !== expected.component_id || kind !== expected.component_type || subject.started_at !== expected.started_at)) invalid()
    seen.add(kind); ids.add(subject.component_id)
    const state = row.collection?.state, sampled = Date.parse(row.collection?.sampled_at)
    if (!['collecting', 'failed', 'stale', 'no_sample', 'snapshot_unavailable', 'not_connected', 'source_unavailable'].includes(state)) invalid()
    if (['collecting', 'failed', 'stale', 'snapshot_unavailable'].includes(state) ? !Number.isFinite(sampled) || sampled > queried || (state === 'stale' ? queried - sampled <= 60000 : queried - sampled > 60000) : row.collection.sampled_at !== null) invalid()
    const metrics = params ? cdcMetrics[kind].filter(item => params.metrics.split(',').includes(item.key)) : cdcMetrics[kind]
    if (!Array.isArray(row.series) || row.series.length !== metrics.length) invalid()
    const keys = new Set()
    for (const series of row.series) {
      const metric = metrics.find(item => item.key === series.metric_key)
      if (!metric || keys.has(metric.key) || series.unit !== metric.unit || series.window_seconds !== 0 || !series.dimensions || Array.isArray(series.dimensions) || Object.keys(series.dimensions).length || !Array.isArray(series.points) || series.points.length !== count) invalid()
      keys.add(metric.key)
      for (const [index, point] of series.points.entries()) {
        const evaluated = Date.parse(point?.evaluated_at), stamp = Date.parse(point?.sampled_at)
        if (evaluated !== start + index * step * 1000 || !['valid', 'stale', 'no_data', 'no_sample', 'not_connected', 'source_unavailable'].includes(point.data_state)) invalid()
        if (['valid', 'stale'].includes(point.data_state)) {
          if (typeof point.value !== 'number' || !Number.isFinite(point.value) || (point.value < 0 && metric.name !== 'sourceDelay') || metric.unit === 'percent' && point.value > 100 || ['tasks', 'connectors', 'records', 'partitions'].includes(metric.unit) && !Number.isSafeInteger(point.value) || !Number.isFinite(stamp) || stamp > evaluated || (point.data_state === 'valid' ? evaluated - stamp > 60000 : evaluated - stamp <= 60000) || !params && state !== 'collecting') invalid()
        } else if (point.value !== null || point.sampled_at !== null) invalid()
      }
    }
  }
  return value
}

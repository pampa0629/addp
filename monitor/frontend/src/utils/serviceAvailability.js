import { isTargetUUID } from './monitoringTargets.js'
const hash = value => typeof value === 'string' && /^[0-9a-f]{64}$/u.test(value)
export const validAvailabilityPolicy = value => [value?.trigger_seconds, value?.recovery_seconds].every(v => Number.isInteger(v) && v >= 15 && v <= 300 && v % 15 === 0)
export const validAvailabilityTarget = value => hash(value?.deployment_id) && typeof value?.module_name === 'string' && /^[a-z][a-z0-9_-]{0,49}$/u.test(value.module_name) && ((value.role === 'backend' && value.runtime_name === '') || (value.role === 'worker' && /^[a-z][a-z0-9_-]{0,49}$/u.test(value.runtime_name)))
export function validateAvailabilityGroups(value) {
  if (!value || !Array.isArray(value.data) || value.data.length > 1000 || value.total !== value.data.length) throw new Error('invalid_service_groups')
  const ids = new Set()
  for (const row of value.data) {
    if (!hash(row.id) || ids.has(row.id) || !validAvailabilityTarget(row) || !Number.isInteger(row.expected_replicas) || row.expected_replicas < 1 || row.expected_replicas > 1000 || !['running','planned_stopped'].includes(row.desired_state) || !['observed','paused','unknown'].includes(row.state) || !Number.isInteger(row.online_replicas) || row.online_replicas < 0 || row.online_replicas > 10000) throw new Error('invalid_service_groups')
    ids.add(row.id)
  }
  return value.data
}
export function validateAvailabilityRules(value, page) {
  if (!value || !Array.isArray(value.data) || value.data.length > 20 || value.page !== page || value.page_size !== 20 || !Number.isSafeInteger(value.total) || value.total < 0 || value.total > 1000 || value.total_pages !== Math.ceil(value.total / 20) || value.data.length !== Math.max(0, Math.min(20, value.total - (page - 1) * 20))) throw new Error('invalid_availability_rules')
  const result = value
  const ids = new Set()
  for (const row of result.data) {
    if (!isTargetUUID(row.id) || ids.has(row.id) || !hash(row.group_id) || !validAvailabilityTarget(row) || !validAvailabilityPolicy(row) || typeof row.name !== 'string' || typeof row.enabled !== 'boolean' || !Number.isSafeInteger(row.version) || row.version < 1 || !['','normal','risk','unknown','paused'].includes(row.last_verdict) || (row.online_replicas !== null && (!Number.isInteger(row.online_replicas) || row.online_replicas < 0))) throw new Error('invalid_availability_rules')
    ids.add(row.id)
  }
  return result
}
export function availabilityEvidence(row) {
  const e = row?.availability_evidence
  if (row?.category !== 'service_availability' || row.subject_kind !== 'service_group' || !hash(row.subject_id) || !e || !validAvailabilityTarget(e) || !isTargetUUID(e.rule_id) || row.signal !== e.rule_id || !validAvailabilityPolicy(e.policy) || !Number.isSafeInteger(e.rule_version) || e.rule_version < 1 || !Number.isInteger(e.expected_replicas) || e.expected_replicas < 1 || !Number.isInteger(e.online_replicas) || e.online_replicas < 0 || !Number.isInteger(e.samples) || e.samples < 2) return null
  const times = ['intent_changed_at','confirmed_at','observed_from','observed_to'].map(key => Date.parse(e[key]))
  return times.every(Number.isFinite) && times[0] <= times[1] && times[1] <= times[2] && times[2] <= times[3] ? e : null
}

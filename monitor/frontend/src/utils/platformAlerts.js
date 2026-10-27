import { normalizeQueryTimestamp } from '../../../../common-frontend/basic/src/utils/runtimeInstanceTime.mjs'
import { validResourceDimensions } from './nodeResources.js'
import { isTargetUUID } from './monitoringTargets.js'

const alertTimeBound = value => {
  if (value === undefined || value === null || value === '') return ''
  const normalized = normalizeQueryTimestamp(value instanceof Date && Number.isFinite(value.getTime()) ? value.toISOString() : value)
  return normalized && new Date(normalized).getUTCFullYear() >= 1 ? normalized : 'invalid'
}

export function platformAlertTimeDraft(from, to) {
  const time_from = alertTimeBound(from), time_to = alertTimeBound(to)
  return { time_from, time_to, valid: time_from !== 'invalid' && time_to !== 'invalid' && (!time_from || !time_to || Date.parse(time_from) < Date.parse(time_to)) }
}

export function platformAlertSearch(value) {
  if (value === undefined || value === null) return ''
  if (typeof value !== 'string' || /[\p{Cc}\p{Cs}]/u.test(value) || [...value.trim()].length > 100) return '\u0000'
  return value.trim()
}

// Stable platform-management state; opaque IDs are never inferred from names.
export function platformAlertState(query = {}) {
  const positive = value => typeof value === 'string' && /^[1-9][0-9]*$/u.test(value) && Number(value) <= 10000 ? Number(value) : 1
  return { tab: ['notifications', 'rules', 'http-rules', 'process-rules', 'availability-rules'].includes(query.tab) ? query.tab : 'incidents',
    search: platformAlertSearch(query.search), host_name: platformAlertSearch(query.host_name),
    time_from: alertTimeBound(query.time_from), time_to: alertTimeBound(query.time_to),
    severity: query.severity === undefined ? '' : typeof query.severity === 'string' && query.severity.length <= 32 ? query.severity : 'invalid',
    category: ['log_pipeline', 'resource', 'http_quality', 'process_resource', 'service_availability'].includes(query.category) ? query.category : '',
    status: query.status === 'all' ? '' : ['open', 'acknowledged', 'resolved'].includes(query.status) ? query.status : 'active',
    subject_id: typeof query.subject_id === 'string' && query.subject_id.length <= 100 && !/[\r\n\u0000]/u.test(query.subject_id) ? query.subject_id : '',
    availability_rule_page: positive(query.availability_rule_page), process_rule_page: positive(query.process_rule_page), http_rule_page: positive(query.http_rule_page), rule_page: positive(query.rule_page), page: positive(query.page), delivery_page: positive(query.delivery_page) }
}
export function platformAlertQuery(state) {
  return { ...(['notifications', 'rules', 'http-rules', 'process-rules', 'availability-rules'].includes(state.tab) ? { tab: state.tab } : {}), ...(state.category ? { category: state.category } : {}), ...(state.severity ? { severity: state.severity } : {}),
    ...(state.status === 'active' ? {} : { status: state.status || 'all' }), ...(state.subject_id ? { subject_id: state.subject_id } : {}), ...(state.search ? { search: state.search } : {}), ...(state.host_name ? { host_name: state.host_name } : {}), ...(state.time_from ? { time_from: state.time_from } : {}), ...(state.time_to ? { time_to: state.time_to } : {}),
    ...(state.availability_rule_page > 1 ? { availability_rule_page: String(state.availability_rule_page) } : {}), ...(state.process_rule_page > 1 ? { process_rule_page: String(state.process_rule_page) } : {}), ...(state.http_rule_page > 1 ? { http_rule_page: String(state.http_rule_page) } : {}), ...(state.rule_page > 1 ? { rule_page: String(state.rule_page) } : {}), ...(state.page > 1 ? { page: String(state.page) } : {}), ...(state.delivery_page > 1 ? { delivery_page: String(state.delivery_page) } : {}) }
}

export function resourceAlertHostID(row) {
  return row?.category === 'resource' && row.subject_kind === 'host_node' && isTargetUUID(row.subject_id) ? row.subject_id : ''
}

export const filesystemAlertMetric = 'node.filesystem.used_percent'
export function resourceAlertMetric(row) {
  if (['node.cpu.busy_percent','node.memory.used_percent'].includes(row?.signal)) return row.signal
  return typeof row?.signal === 'string' && /^node\.filesystem\.used_percent:[a-f0-9]{64}$/u.test(row.signal) ? filesystemAlertMetric : ''
}

// Interpret the saved opening evidence; never reconstruct it from a live rule.
export function resourceAlertEvidence(row) {
  if (!resourceAlertHostID(row) || !resourceAlertMetric(row)) return null
  const e = row.resource_evidence, p = e?.policy
  if (!e || !p || !isTargetUUID(e.rule_id) || !isTargetUUID(e.target_id) ||
    ![e.rule_version, e.node_version, e.target_version, e.samples].every(value => Number.isSafeInteger(value) && value > 0) ||
    !(Number.isSafeInteger(e.query_policy_version) && e.query_policy_version >= 0) ||
    ![p.trigger_percent, p.recovery_percent, e.minimum, e.maximum, e.latest].every(value => typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 100) ||
    !(p.recovery_percent > 0 && p.recovery_percent < p.trigger_percent) ||
    ![p.trigger_seconds, p.recovery_seconds].every(value => Number.isInteger(value) && value >= 15 && value <= 300 && value % 15 === 0) ||
    e.minimum > e.latest || e.latest > e.maximum) return null
  if (resourceAlertMetric(row) === filesystemAlertMetric) {
    const f = e.filesystem
    if (!f || !validResourceDimensions(f.dimensions, false) || ![f.total_bytes,f.used_bytes,f.available_bytes].every(value => typeof value === 'number' && Number.isFinite(value) && value >= 0) || f.total_bytes <= 0 || f.used_bytes > f.total_bytes || f.available_bytes > f.total_bytes-f.used_bytes+4*Number.EPSILON*f.total_bytes || f.used_bytes+f.available_bytes <= 0 || Math.abs(e.latest-100*f.used_bytes/(f.used_bytes+f.available_bytes)) > 0.000001) return null
  } else if (e.filesystem) return null
  const times = [e.sampled_from, e.sampled_to, e.evaluated_at].map(value => typeof value === 'string' ? Date.parse(value) : NaN)
  return times.every(Number.isFinite) && times[0] <= times[1] && times[1] <= times[2] ? e : null
}

export function resourceAlertTrendRoute(row) {
  const evidence = resourceAlertEvidence(row)
  if (!evidence) return null
  return { path: `/node-resources/${row.subject_id}`, query: { metric: resourceAlertMetric(row), ...(evidence.filesystem?.dimensions || {}),
    end: new Date(Math.floor(Date.parse(evidence.evaluated_at) / 1000) * 1000).toISOString(), refresh: 'off' } }
}

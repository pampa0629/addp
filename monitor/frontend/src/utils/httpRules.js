import { isTargetUUID } from './monitoringTargets.js'
import { httpMetrics } from './httpQuality.js'

export const httpRuleMetrics = httpMetrics.filter(metric => ['qps', 'failure', 'mean'].includes(metric.name))
const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS', 'CONNECT', 'TRACE', 'QUERY', '_OTHER']
export function validHTTPPolicy(policy, metric) {
  return httpRuleMetrics.some(item => item.key === metric) &&
    [policy?.trigger_value, policy?.recovery_value].every(value => typeof value === 'number' && Number.isFinite(value)) &&
    policy.recovery_value > 0 && policy.recovery_value < policy.trigger_value && policy.trigger_value <= (metric === 'http.failures.percent' ? 100 : 1e9) &&
    [policy.trigger_seconds, policy.recovery_seconds].every(value => Number.isInteger(value) && value >= 15 && value <= 300 && value % 15 === 0)
}
export function validHTTPSubject(row) {
  return typeof row?.module_name === 'string' && /^[a-z][a-z0-9_-]{0,49}$/u.test(row.module_name) && ['backend', 'ingress'].includes(row.role) &&
    (row.route === null ? row.method === '' : typeof row.route === 'string' && row.route.length <= 1024 && !/[\x00-\x1f\x7f]/u.test(row.route) && (!row.route || row.route.startsWith('/')) && methods.includes(row.method))
}
export function validateHTTPRules(page, number) {
  if (!Array.isArray(page?.data) || page.data.length > 20 || page.page !== number || page.page_size !== 20 || !Number.isSafeInteger(page.total) || page.total < page.data.length) throw new Error('invalid_http_rules')
  const ids = new Set()
  for (const row of page.data) {
    if (!isTargetUUID(row.id) || ids.has(row.id) || !Number.isSafeInteger(row.version) || row.version < 1 || !validHTTPSubject(row) || !validHTTPPolicy(row, row.metric_key) || typeof row.name !== 'string' || typeof row.enabled !== 'boolean' || !Number.isSafeInteger(row.anchor_instance_id) || row.anchor_instance_id < 1 || !['', 'normal', 'risk', 'unknown', 'paused'].includes(row.last_verdict) || (row.last_value !== null && (typeof row.last_value !== 'number' || !Number.isFinite(row.last_value)))) throw new Error('invalid_http_rules')
    ids.add(row.id)
  }
  return page
}
export function httpAlertEvidence(row) {
  const e = row?.http_evidence
  if (row?.category !== 'http_quality' || row.subject_kind !== 'module' || !e || row.signal !== e.rule_id || row.subject_id !== e.module_name || !isTargetUUID(e.rule_id) || !Number.isSafeInteger(e.rule_version) || e.rule_version < 1 || !validHTTPSubject(e) || !validHTTPPolicy(e.policy, e.metric_key) || !Array.isArray(e.instance_ids) || !e.instance_ids.length || new Set(e.instance_ids).size !== e.instance_ids.length || !e.instance_ids.every(value => typeof value === 'string' && value.length > 0 && value.length <= 100) || !Number.isSafeInteger(e.query_policy_version) || e.query_policy_version < 0 || !Number.isInteger(e.samples) || e.samples < 2 || e.samples > 22 || ![e.minimum, e.maximum, e.latest].every(value => typeof value === 'number' && Number.isFinite(value) && value >= 0) || e.minimum > e.latest || e.latest > e.maximum || (e.metric_key === 'http.failures.percent' && e.maximum > 100)) return null
  const times = [e.sampled_from, e.sampled_to, e.evaluated_at].map(value => typeof value === 'string' ? Date.parse(value) : NaN)
  return times.every(Number.isFinite) && times[0] <= times[1] && times[1] <= times[2] ? e : null
}

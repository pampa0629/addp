import { isTargetUUID } from './monitoringTargets.js'
import { processMetrics, processRoles } from './processResources.js'
export const processRuleMetrics = processMetrics.slice(0, 2)
export const processRuleScale = key => key === 'process.memory.resident_bytes' ? 1024 ** 3 : 1
export function validProcessPolicy(p, key) {
 return processRuleMetrics.some(m => m.key === key) && [p?.trigger_value, p?.recovery_value].every(v => typeof v === 'number' && Number.isFinite(v)) && p.recovery_value > 0 && p.recovery_value < p.trigger_value && p.trigger_value <= Number.MAX_SAFE_INTEGER && [p.trigger_seconds,p.recovery_seconds].every(v => Number.isInteger(v) && v >= 15 && v <= 300 && v % 15 === 0)
}
export function validProcessSubject(row) {
 return Number.isSafeInteger(row?.anchor_instance_id) && row.anchor_instance_id > 0 && typeof row.module_name === 'string' && /^[a-z][a-z0-9_-]{0,49}$/u.test(row.module_name) && processRoles.includes(row.role) && typeof row.runtime_instance_id === 'string' && row.runtime_instance_id.length > 0 && row.runtime_instance_id.length <= 100 && !/[\x00-\x1f\x7f]/u.test(row.runtime_instance_id) && typeof row.process_started_at === 'string' && Number.isFinite(Date.parse(row.process_started_at))
}
export function validateProcessRules(page, number) {
 if (!Array.isArray(page?.data) || page.data.length > 20 || page.page !== number || page.page_size !== 20 || !Number.isSafeInteger(page.total) || page.total < page.data.length) throw new Error('invalid_process_rules')
 const ids = new Set()
 for(const row of page.data) {
  if(!isTargetUUID(row.id) || ids.has(row.id) || !Number.isSafeInteger(row.version) || row.version < 1 || !validProcessSubject(row) || !validProcessPolicy(row,row.metric_key) || typeof row.name !== 'string' || typeof row.enabled !== 'boolean' || !['','normal','risk','unknown','paused'].includes(row.last_verdict) || (row.last_value !== null && (typeof row.last_value !== 'number' || !Number.isFinite(row.last_value) || row.last_value < 0))) throw new Error('invalid_process_rules')
  ids.add(row.id)
 }
 return page
}
export function processAlertEvidence(row) {
 const e=row?.process_evidence
 if(row?.category !== 'process_resource' || row.subject_kind !== 'module_instance' || !e || row.signal !== e.rule_id || row.subject_id !== String(e.anchor_instance_id) || row.instance_id !== e.runtime_instance_id || !isTargetUUID(e.rule_id) || !Number.isSafeInteger(e.rule_version) || e.rule_version < 1 || !validProcessSubject(e) || !validProcessPolicy(e.policy,e.metric_key) || !Number.isSafeInteger(e.query_policy_version) || e.query_policy_version < 0 || !Number.isInteger(e.samples) || e.samples < 2 || e.samples > 22 || ![e.minimum,e.maximum,e.latest].every(v=>typeof v==='number' && Number.isFinite(v) && v>=0) || e.minimum>e.latest || e.latest>e.maximum) return null
 const times=[e.sampled_from,e.sampled_to,e.evaluated_at].map(v=>typeof v==='string'?Date.parse(v):NaN)
 return times.every(Number.isFinite) && Date.parse(e.process_started_at)<=times[0] && times[0]<=times[1] && times[1]<=times[2] ? e : null
}

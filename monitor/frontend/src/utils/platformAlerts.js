import { isTargetUUID } from './monitoringTargets.js'

// Stable platform-management state; opaque IDs are never inferred from names.
export function platformAlertState(query = {}) {
  const positive = value => typeof value === 'string' && /^[1-9][0-9]*$/u.test(value) && Number(value) <= 10000 ? Number(value) : 1
  return { tab: ['notifications', 'rules'].includes(query.tab) ? query.tab : 'incidents',
    category: ['log_pipeline', 'resource'].includes(query.category) ? query.category : '',
    status: query.status === 'all' ? '' : ['open', 'acknowledged', 'resolved'].includes(query.status) ? query.status : 'active',
    subject_id: typeof query.subject_id === 'string' && query.subject_id.length <= 100 && !/[\r\n\u0000]/u.test(query.subject_id) ? query.subject_id : '',
    rule_page: positive(query.rule_page), page: positive(query.page), delivery_page: positive(query.delivery_page) }
}
export function platformAlertQuery(state) {
  return { ...(['notifications', 'rules'].includes(state.tab) ? { tab: state.tab } : {}), ...(state.category ? { category: state.category } : {}),
    ...(state.status === 'active' ? {} : { status: state.status || 'all' }), ...(state.subject_id ? { subject_id: state.subject_id } : {}),
    ...(state.rule_page > 1 ? { rule_page: String(state.rule_page) } : {}), ...(state.page > 1 ? { page: String(state.page) } : {}), ...(state.delivery_page > 1 ? { delivery_page: String(state.delivery_page) } : {}) }
}

export function resourceAlertHostID(row) {
  return row?.category === 'resource' && row.subject_kind === 'host_node' && isTargetUUID(row.subject_id) ? row.subject_id : ''
}

// Interpret the saved opening evidence; never reconstruct it from a live rule.
export function resourceAlertEvidence(row) {
  if (!resourceAlertHostID(row) || !['node.cpu.busy_percent', 'node.memory.used_percent'].includes(row.signal)) return null
  const e = row.resource_evidence, p = e?.policy
  if (!e || !p || !isTargetUUID(e.rule_id) || !isTargetUUID(e.target_id) ||
    ![e.rule_version, e.node_version, e.target_version, e.samples].every(value => Number.isSafeInteger(value) && value > 0) ||
    !(Number.isSafeInteger(e.query_policy_version) && e.query_policy_version >= 0) ||
    ![p.trigger_percent, p.recovery_percent, e.minimum, e.maximum, e.latest].every(value => typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 100) ||
    !(p.recovery_percent > 0 && p.recovery_percent < p.trigger_percent) ||
    ![p.trigger_seconds, p.recovery_seconds].every(value => Number.isInteger(value) && value >= 15 && value <= 300 && value % 15 === 0) ||
    e.minimum > e.latest || e.latest > e.maximum) return null
  const times = [e.sampled_from, e.sampled_to, e.evaluated_at].map(value => typeof value === 'string' ? Date.parse(value) : NaN)
  return times.every(Number.isFinite) && times[0] <= times[1] && times[1] <= times[2] ? e : null
}

export function resourceAlertTrendRoute(row) {
  const evidence = resourceAlertEvidence(row)
  if (!evidence) return null
  return { path: `/node-resources/${row.subject_id}`, query: { metric: row.signal,
    end: new Date(Math.floor(Date.parse(evidence.evaluated_at) / 1000) * 1000).toISOString(), refresh: 'off' } }
}

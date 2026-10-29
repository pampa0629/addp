const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
export const cleanupOwners = Object.freeze(['asset', 'develop', 'graph', 'manager', 'meta', 'model', 'quality', 'service', 'standard', 'transfer'])
const object = value => value !== null && typeof value === 'object' && !Array.isArray(value)
const invalid = () => { throw new Error('cleanup_response_invalid') }
const count = value => Number.isSafeInteger(value) && value >= 0 && value <= 1e12
const instant = value => typeof value === 'string' && /^\d{4}-\d\d-\d\dT.*Z$/.test(value) && Number.isFinite(Date.parse(value))
const closed = (value, keys) => object(value) && Object.keys(value).every(key => keys.includes(key))
const summaryKeys = ['scanned_items', 'affected_records', 'deleted_physical_artifacts', 'freed_bytes', 'marked_missing_source', 'marked_outdated', 'disabled_task_definitions', 'deleted_task_definitions', 'skipped_items', 'error_count', 'risk_level']
const impactKeys = ['rebindable', 'will_disable', 'will_delete', 'running', 'external_artifact']
const digest = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value)
function validateImpact(value) {
  if (!closed(value, impactKeys) || Object.values(value).some(v => !count(v))) invalid()
}
export function validateCleanupSummary(value) {
  if (!closed(value, summaryKeys) || Object.entries(value).some(([key, v]) => key === 'risk_level' ? !['low', 'medium', 'high'].includes(v) : !count(v))) invalid()
  return value
}
export function parseCleanupAdmission(value, execution) {
  if (value === undefined) return undefined
  if (!closed(value, ['schema', 'state', 'basis', 'observed_at']) || value.schema !== 'addp.cleanup-admission-view/v1' || !instant(value.observed_at)) invalid()
  if (!execution || execution.task_type !== 'cleanup_executor' || execution.source !== 'system' || execution.attempt !== 0 || execution.progress !== 0 || execution.started_at != null || execution.execution_time_ms != null) invalid()
  switch (value.state) {
    case 'closed_unclaimed':
      if (value.basis !== 'parent_terminal' || execution.status !== 'pending' || execution.completed_at != null) invalid()
      break
    case 'closed_rejected':
      if (!['parent_terminal', 'deadline_reached'].includes(value.basis) || execution.status !== 'failed' || !instant(execution.completed_at)) invalid()
      break
    case 'rejected_unstarted':
      if (Object.hasOwn(value, 'basis') || execution.status !== 'failed' || !instant(execution.completed_at)) invalid()
      break
    default: invalid()
  }
  return value
}
export function parseCleanupExecutions(response) {
  if (!object(response) || !uuid.test(response.task_id) || !['scan', 'execute'].includes(response.action) || !['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(response.status)) invalid()
  if (response.summary !== undefined) {
    const { impact, impact_digest, ...summary } = response.summary || {}
    validateCleanupSummary(summary)
    validateImpact(impact)
    if (impact_digest !== undefined && !digest(impact_digest)) invalid()
    if (!instant(response.cutoff_at)) invalid()
  }
  if (!Object.hasOwn(response, 'module_executions')) return undefined
  const rows = response.module_executions
  if (!Array.isArray(rows) || rows.length > 10) invalid()
  const owners = new Set()
  for (const row of rows) {
    if (!object(row) || !uuid.test(row.execution_id) || !cleanupOwners.includes(row.module) || owners.has(row.module) || row.task_type !== 'cleanup_executor' || row.source !== 'system' || !Number.isSafeInteger(row.attempt) || row.attempt < 0 || !['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(row.status)) invalid()
    owners.add(row.module)
    parseCleanupAdmission(row.cleanup_admission, row)
    if (row.receipt !== undefined) {
      const r = row.receipt
      if (!object(r) || r.schema !== 'addp.cleanup-receipt/v1' || r.execution_id !== row.execution_id || r.module !== row.module || r.request_id !== response.task_id || r.attempt !== row.attempt || !['success', 'failed', 'partial_success', 'timeout', 'skipped'].includes(r.status)) invalid()
      validateCleanupSummary(r.summary)
      validateImpact(r.impact)
      if (!digest(r.impact_digest) || !digest(r.request_digest) || r.parent_execution_id !== row.parent_execution_id || r.action !== response.action || r.attempt < 1 || !['success', 'failed', 'timeout'].includes(row.status)) invalid()
      const terminal = r.status === 'success' || r.status === 'skipped' ? 'success' : r.status === 'timeout' ? 'timeout' : 'failed'
      if (terminal !== row.status) invalid()
    }
  }
  return rows
}
export function parseCleanupProfessionalResult(value, expected) {
  if (!closed(value, ['schema', 'execution_id', 'request_id', 'module', 'attempt', 'action', 'cleanup_mode', 'status', 'summary', 'statistics', 'reason']) || value.schema !== 'addp.cleanup-professional-result/v1' || value.execution_id !== expected.execution_id || value.request_id !== expected.request_id || value.module !== expected.module || value.attempt !== expected.attempt || !Number.isSafeInteger(value.attempt) || value.attempt < 1 || !['scan', 'execute'].includes(value.action) || !['success', 'failed', 'partial_success', 'timeout', 'skipped'].includes(value.status)) invalid()
  if (value.action === 'scan' ? value.cleanup_mode !== undefined : !['logical_cleanup', 'physical_cleanup'].includes(value.cleanup_mode)) invalid()
  if (value.reason !== undefined && !['owner_failed', 'owner_unavailable', 'authorization_denied', 'active_execution', 'external_effect_uncertain', 'scope_changed', 'unsupported_scope'].includes(value.reason)) invalid()
  if (expected.receipt && ['action', 'cleanup_mode', 'status', 'reason'].some(key => value[key] !== expected.receipt[key])) invalid()
  validateCleanupSummary(value.summary)
  if (!object(value.statistics) || Object.keys(value.statistics).length > 128 || Object.entries(value.statistics).some(([key, v]) => !/^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){0,3}$/.test(key) || key.length > 128 || !count(v))) invalid()
  return value
}

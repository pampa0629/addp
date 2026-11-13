import { traceLinkQuery, traceRoute } from '../../../../monitor/frontend/src/utils/traces.js'

const traceID = value => typeof value === 'string' && /^[0-9a-f]{32}$/.test(value) && !/^0+$/.test(value)
const spanID = value => typeof value === 'string' && /^[0-9a-f]{16}$/.test(value) && !/^0+$/.test(value)
const instanceID = value => typeof value === 'string' && /^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$/.test(value)
const utc = value => typeof value === 'string' && value.endsWith('Z') && Number.isFinite(Date.parse(value))
const invalid = () => { throw new Error('execution_trace_local_browser_input_invalid') }

// This input contains only references independently found through public APIs.
// Authentication material is never accepted as part of a saved browser input.
export function executionTraceBrowserInput(value, now = Date.now()) {
  const allowed = ['schema', 'run_id', 'admin_id', 'module_name', 'instance_id', 'runtime_instance_id', 'execution_id', 'trace_id', 'span_id', 'started_at', 'start', 'end', 'links']
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).some(key => !allowed.includes(key)) || value.schema !== 'addp.execution-trace-browser/v1' || typeof value.run_id !== 'string' || !/^[A-Za-z0-9_-]{1,100}$/.test(value.run_id) || !['orchestrator', 'develop'].includes(value.module_name) || !instanceID(value.instance_id) || !Number.isSafeInteger(value.runtime_instance_id) || value.runtime_instance_id < 1 || !traceID(value.trace_id) || !spanID(value.span_id) || typeof value.execution_id !== 'string' || !/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value.execution_id) || !utc(value.started_at) || !utc(value.start) || !utc(value.end) || Date.parse(value.start) > Date.parse(value.started_at) || Date.parse(value.started_at) >= Date.parse(value.end) || Date.parse(value.end) > now || Date.parse(value.start) < now - 72 * 3600000 || !Array.isArray(value.links) || value.links.length < 1 || value.links.length > 2 || (value.admin_id !== undefined && (!Number.isSafeInteger(value.admin_id) || value.admin_id < 1))) invalid()
  const query = { instance_id: String(value.runtime_instance_id), module_name: value.module_name, role: 'backend', trace_id: value.trace_id, start: value.start, end: value.end }
  try { traceRoute(query) } catch { invalid() }
  const seen = new Set()
  for (const link of value.links) {
    if (!link || Object.keys(link).sort().join(',') !== 'instance_id,module_name,role,runtime_instance_id,span_id,started_at,trace_id' || !traceID(link.trace_id) || !spanID(link.span_id) || link.trace_id === value.trace_id || !instanceID(link.instance_id) || !utc(link.started_at) || seen.has(link.trace_id + '/' + link.span_id)) invalid()
    try { traceLinkQuery(link, now) } catch { invalid() }
    seen.add(link.trace_id + '/' + link.span_id)
  }
  return { ...value, trace_path: '/monitor/traces?' + new URLSearchParams(query) }
}

export function admitExecutionTraceBrowserEnvironment(env) {
  if (env.ADDP_EXECUTION_TRACE_LOCAL !== '1' || ['GITHUB_ACTIONS', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_OWNER_MANAGED', 'ADDP_ONLINE_LOCAL_SGM'].some(key => env[key] !== undefined)) throw new Error('execution_trace_local_browser_environment_denied')
  for (const field of ['ADDP_EXECUTION_TRACE_ADMIN_USERNAME', 'ADDP_EXECUTION_TRACE_ADMIN_PASSWORD', 'ADDP_EXECUTION_TRACE_ADMIN_TOTP', 'ADDP_EXECUTION_TRACE_USER_USERNAME', 'ADDP_EXECUTION_TRACE_USER_PASSWORD', 'CONSOLE_URL']) if (typeof env[field] !== 'string' || !env[field]) throw new Error('execution_trace_local_browser_environment_denied')
  let url
  try { url = new URL(env.CONSOLE_URL) } catch { throw new Error('execution_trace_local_browser_environment_denied') }
  if (url.protocol !== 'http:' || url.hostname !== 'nginx' || url.port || url.username || url.password || url.search || url.hash || url.pathname !== '/') throw new Error('execution_trace_local_browser_environment_denied')
}

export { localIdentityTOTP as executionTraceTOTP } from './local-identity-support.mjs'

export const tenantStagePermissions = [
  'orchestrator.workflow.create', 'orchestrator.workflow.read', 'orchestrator.workflow.delete', 'orchestrator.workflow.execute',
  'develop.task.create', 'develop.task.read', 'develop.task.delete', 'develop.task.execute', 'develop.data_read.execute',
  'monitor.execution.read', 'system.execution_authorization.create'
]

// Separate input for a separate real Tenant session. Never pass platform references.
export function tenantStageBrowserInput(value) {
  if (!value || Object.keys(value).sort().join(',') !== 'cases,run_id,schema,tenant_id,user_id' ||
      value.schema !== 'addp.tenant-stage-browser/v1' || !/^[A-Za-z0-9_-]{1,100}$/.test(value.run_id) ||
      !/^[1-9][0-9]*$/.test(value.tenant_id) || !/^[1-9][0-9]*$/.test(value.user_id) ||
      !Array.isArray(value.cases) || value.cases.length !== 4) invalid()
  const seen = new Set()
  for (const row of value.cases) {
    if (Object.keys(row).sort().join(',') !== 'execution_id,kind,module,stages' ||
        !['orchestrator', 'develop'].includes(row.module) || !['readable', 'missing'].includes(row.kind) ||
        !/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(row.execution_id) ||
        seen.has(row.module + row.kind) || !Array.isArray(row.stages) || row.stages.length !== 2) invalid()
    seen.add(row.module + row.kind)
    for (const [index, stage] of row.stages.entries()) {
      const fields = stage.state === 'available' ? 'attempt,duration_ms,end,outcome,phase,slot,start,state' : 'slot,state'
      if (Object.keys(stage).sort().join(',') !== fields || stage.slot !== ['enqueue', 'latest'][index] || !['available', 'not_found'].includes(stage.state)) invalid()
      if (stage.state === 'available' && (!Number.isSafeInteger(stage.attempt) || (index ? stage.attempt < 1 : stage.attempt !== 0) ||
          !Number.isFinite(stage.duration_ms) || stage.duration_ms < 0 || !utc(stage.start) || !utc(stage.end) || Date.parse(stage.end) < Date.parse(stage.start) ||
          !['success', 'failed', 'timeout', 'cancelled', 'rejected'].includes(stage.outcome) ||
          !(index ? ['claim', 'dispatch', 'terminal'] : ['enqueue']).includes(stage.phase))) invalid()
    }
    if (row.kind === 'readable' && (row.stages[1].state !== 'available' || row.stages[1].phase !== 'terminal' || row.stages[1].outcome !== 'failed') ||
        row.kind === 'missing' && row.stages[0].state !== 'not_found') invalid()
  }
  return value
}

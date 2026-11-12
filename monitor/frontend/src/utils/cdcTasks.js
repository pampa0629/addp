import { autoRefreshOptions } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { parseLocator } from '../../../../common-frontend/basic/src/types/resourceLocator.js'
import { isTargetUUID } from './monitoringTargets.js'

const invalid = () => { throw new Error('invalid_resource_response') }
const positive = value => Number.isSafeInteger(value) && value > 0
export function cdcView(query, context) {
  return ['tasks', 'resources'].includes(query.view) ? query.view : context === 'tenant' ? 'tasks' : 'resources'
}
export function resolveCDCTaskRoute(input = {}) {
  const page = typeof input.page === 'string' && /^[1-9]\d{0,5}$/.test(input.page) ? Number(input.page) : 1
  const taskID = typeof input.task_id === 'string' && /^[1-9]\d*$/.test(input.task_id) && positive(Number(input.task_id)) ? input.task_id : ''
  const refresh = autoRefreshOptions.includes(input.refresh) ? input.refresh : '15'
  const query = { view: 'tasks' }
  if (page !== 1) query.page = String(page)
  if (taskID) query.task_id = taskID
  if (refresh !== '15') query.refresh = refresh
  return { page, taskID, refresh, query, changed: JSON.stringify(Object.entries(input).sort()) !== JSON.stringify(Object.entries(query).sort()) }
}
export function cdcEndpoints(task) {
  const source = parseLocator(task.config.source.locator)
  const parent = parseLocator(task.config.target.parent_locator)
  return { source: { engineID: source.engineId, path: source.path.join('.') }, target: { engineID: parent.engineId, path: [...parent.path, task.config.target.name].join('.') } }
}
export function validateCDCTask(task, expectedID = null) {
  if (!positive(task?.id) || expectedID && String(task.id) !== expectedID || typeof task.name !== 'string' || !task.name || task.task_type !== 'sync' || task.config?.runtime?.boundary !== 'continuous' || task.config?.load?.change_detection?.type !== 'cdc' || typeof task.status !== 'string' || !['running', 'paused', 'stopped'].includes(task.desired_state) || typeof task.config?.target?.name !== 'string' || !task.config.target.name) invalid()
  if (task.last_execution_id != null && !isTargetUUID(task.last_execution_id)) invalid()
  if (task.capture && (!positive(task.capture.generation) || typeof task.capture.status !== 'string')) invalid()
  if (task.capture) {
    if (!['postgresql','mysql','oracle'].includes(task.capture.source_type)) invalid()
    validateCDCBuffer(task.capture.buffer_observation)
    if (task.capture.source_type !== 'postgresql' && (task.capture.postgresql_observation !== null || task.capture.delay_observation !== null)) invalid()
    validateCDCPostgreSQL(task.capture.postgresql_observation)
    validateCDCDelay(task.capture.delay_observation)
  }
  try { cdcEndpoints(task) } catch { invalid() }
  return task
}
export function validateCDCTaskPage(value, page) {
  if (!Array.isArray(value?.items) || value.items.length > 20 || value.page !== page || value.page_size !== 20 || !Number.isSafeInteger(value.total) || value.total < value.items.length) invalid()
  const seen = new Set()
  for (const task of value.items) { validateCDCTask(task); if (seen.has(task.id)) invalid(); seen.add(task.id) }
  return value
}
export function validateCDCTaskExecution(execution, task) {
  if (execution?.execution_id !== task.last_execution_id || execution.module !== 'transfer' || execution.task_type !== 'sync' || String(execution.source_task_id) !== String(task.id) || execution.execution_boundary !== 'continuous') invalid()
  const generation = execution.metadata?.continuous?.capture?.generation
  if (generation != null && task.capture && generation !== task.capture.generation) invalid()
  return execution
}

export function validateCDCBuffer(value) {
  if (value === null) return value
  const sample = Date.parse(value?.sampled_at), expires = Date.parse(value?.expires_at)
  if (!['available', 'unavailable'].includes(value?.status) || !Number.isFinite(sample) || !Number.isFinite(expires) || expires <= sample || !['available', 'lost', 'not_started', 'unknown'].includes(value.recovery_state)) invalid()
  const fields = ['earliest_offset', 'latest_offset', 'committed_offset', 'lag_offsets']
  for (const key of fields) if (value[key] !== null && (typeof value[key] !== 'string' || !/^(0|[1-9]\d*)$/.test(value[key]) || BigInt(value[key]) > 9223372036854775807n)) invalid()
  if (value.status === 'unavailable') {
    if (fields.some(key => value[key] !== null) || value.recovery_state !== 'unknown') invalid()
  } else {
    if (value.earliest_offset === null || value.latest_offset === null || BigInt(value.earliest_offset) > BigInt(value.latest_offset)) invalid()
    if (value.committed_offset === null) {
      if (value.lag_offsets !== null || value.recovery_state !== (BigInt(value.earliest_offset) > 0n ? 'lost' : 'not_started')) invalid()
    } else if (value.lag_offsets === null || BigInt(value.latest_offset) - BigInt(value.committed_offset) !== BigInt(value.lag_offsets) || value.recovery_state !== (BigInt(value.committed_offset) < BigInt(value.earliest_offset) ? 'lost' : 'available')) invalid()
  }
  return value
}

function validObservationTime(value) {
 const sample=Date.parse(value?.sampled_at),expires=Date.parse(value?.expires_at)
 if (!Number.isFinite(sample)||!Number.isFinite(expires)||expires<=sample) invalid()
}
export function validateCDCPostgreSQL(value) {
 if(value===null)return value
 validObservationTime(value)
 if(!['available','missing','identity_mismatch','unavailable'].includes(value.status)||!['reserved','extended','unreserved','lost','unknown'].includes(value.risk_state))invalid()
 const bytes=['retained_wal_bytes','unacknowledged_wal_bytes','safe_wal_size_bytes']
 for(const key of bytes)if(value[key]!==null&&(typeof value[key]!=='string'||!/^(0|[1-9]\d*)$/.test(value[key])||BigInt(value[key])>18446744073709551615n))invalid()
 if(value.status==='available'){
  if(typeof value.slot_active!=='boolean'||typeof value.wal_keep_unlimited!=='boolean'||value.wal_status!==null&&!['reserved','extended','unreserved','lost'].includes(value.wal_status)||value.risk_state!==(value.wal_status||'unknown'))invalid()
 }else if(value.risk_state!=='unknown'||[...bytes,'slot_active','wal_status','wal_keep_unlimited'].some(key=>value[key]!==null))invalid()
 return value
}
export function validateCDCDelay(value){
 if(value===null)return value
 validObservationTime(value)
 if(!['available','no_sample','unavailable'].includes(value.status))invalid()
 if(value.status==='available'){
  if(typeof value.connected!=='boolean'||value.delay_seconds!==null&&(typeof value.delay_seconds!=='number'||!Number.isFinite(value.delay_seconds)||!value.connected))invalid()
 }else if(value.connected!==null||value.delay_seconds!==null)invalid()
 return value
}
export function cdcObservationState(value,now=Date.now()){
 return !value?'no_sample':Date.parse(value.expires_at)<=now?'stale':value.status
}

import test from 'node:test'
import assert from 'node:assert/strict'
import { parseCleanupAdmission, parseCleanupExecutions, parseCleanupProfessionalResult } from '../src/utils/cleanupObservation.mjs'
const request = 'a8eb1e50-fbe5-4c40-b096-bf04a64eb141'
const child = 'bf0044f5-3bc7-48a0-b14a-c4f6a44d8ce2'
const at = '2026-10-29T01:00:00Z'
const digest = 'a'.repeat(64)
const pending = () => ({ execution_id: child, parent_execution_id: request, module: 'graph', task_type: 'cleanup_executor', source: 'system', status: 'pending', attempt: 0, progress: 0 })
const view = () => ({ schema: 'addp.cleanup-admission-view/v1', state: 'closed_unclaimed', basis: 'parent_terminal', observed_at: at })
const response = () => ({ task_id: request, action: 'scan', status: 'timeout', module_executions: [{ ...pending(), cleanup_admission: view() }] })
test('closed unclaimed is explanation only: pending, attempt zero and no start remain unchanged', () => {
  const value = response(), before = structuredClone(value)
  assert.equal(parseCleanupExecutions(value)[0].cleanup_admission.state, 'closed_unclaimed')
  assert.deepEqual(value, before)
  assert.equal(parseCleanupAdmission(undefined, pending()), undefined)
})
test('admission rejects malformed schema, leaked fields and evidence contradicting unstarted', () => {
  for (const change of [{ status: 'running' }, { attempt: 1 }, { progress: 1 }, { started_at: at }, { execution_time_ms: 0 }, { completed_at: at }]) {
    assert.throws(() => parseCleanupAdmission(view(), { ...pending(), ...change }))
  }
  for (const change of [{ schema: 'old' }, { state: 'closed' }, { basis: 'deadline_reached' }, { parent_id: request }, { observed_at: 'not a date' }]) {
    assert.throws(() => parseCleanupAdmission({ ...view(), ...change }, pending()))
  }
  const rejected = { ...pending(), status: 'failed', completed_at: at }
  assert.equal(parseCleanupAdmission({ ...view(), state: 'closed_rejected', basis: 'deadline_reached' }, rejected).state, 'closed_rejected')
  assert.throws(() => parseCleanupAdmission({ ...view(), state: 'rejected_unstarted' }, rejected))
  assert.equal(parseCleanupAdmission({ schema: view().schema, state: 'rejected_unstarted', observed_at: at }, rejected).state, 'rejected_unstarted')
})
test('omitted scope differs from readable empty scope; unknown and duplicate Owners fail closed', () => {
  const value = response()
  delete value.module_executions
  assert.equal(parseCleanupExecutions(value), undefined)
  value.module_executions = []
  assert.deepEqual(parseCleanupExecutions(value), [])
  value.module_executions = [pending(), pending()]
  assert.throws(() => parseCleanupExecutions(value))
  value.module_executions = [{ ...pending(), module: 'unknown' }]
  assert.throws(() => parseCleanupExecutions(value))
})
test('frozen summary and current receipt must remain separately valid and exactly bound', () => {
  const row = { ...pending(), status: 'success', attempt: 1, started_at: at }
  row.receipt = { schema: 'addp.cleanup-receipt/v1', execution_id: child, parent_execution_id: request, request_id: request, request_digest: digest, module: 'graph', attempt: 1, action: 'scan', status: 'success', summary: { scanned_items: 3 }, impact: {}, impact_digest: digest }
  const value = { ...response(), module_executions: [row], cutoff_at: at, summary: { scanned_items: 1, impact: {} } }
  assert.equal(parseCleanupExecutions(value)[0].receipt.summary.scanned_items, 3)
  assert.equal(value.summary.scanned_items, 1)
  for (const change of [{ request_id: child }, { attempt: 2 }, { action: 'execute' }, { status: 'failed' }, { impact_digest: 'bad' }, { summary: { scanned_items: -1 } }]) {
    assert.throws(() => parseCleanupExecutions({ ...value, module_executions: [{ ...row, receipt: { ...row.receipt, ...change } }] }))
  }
  assert.throws(() => parseCleanupExecutions({ ...value, summary: { scanned_items: 1, impact: {}, private_path: '/secret' } }))
})
test('professional result rejects cross task, unsafe body, bad counters and stale attempt', () => {
  const expected = { ...pending(), request_id: request, attempt: 1 }
  const value = { schema: 'addp.cleanup-professional-result/v1', execution_id: child, request_id: request, module: 'graph', attempt: 1, action: 'scan', status: 'success', summary: {}, statistics: { graphs: 3 } }
  assert.equal(parseCleanupProfessionalResult(value, expected), value)
  for (const change of [{ request_id: child }, { attempt: 2 }, { errors: ['secret'] }, { reason: 'sql leaked' }, { statistics: { '/bucket/file': 1 } }, { statistics: { graphs: Number.MAX_SAFE_INTEGER } }, { cleanup_mode: 'physical_cleanup' }]) {
    assert.throws(() => parseCleanupProfessionalResult({ ...value, ...change }, expected))
  }
})
